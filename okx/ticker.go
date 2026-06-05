package okx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type okxFuturesTicker struct {
	Symbol          string
	BidPrice        float64
	AskPrice        float64
	BidSize         float64
	AskSize         float64
	Timestamp       int64
	MarkPrice       float64
	IndexPrice      float64
	FundingRate     float64
	NextFundingTime int64
	FundingInterval int
}

func fetchOkxFuturesTickers(client *http.Client) ([]okxFuturesTicker, error) {
	// 1. БАЗОВЫЕ ТИКЕРЫ И СТАКАНЫ (SWAP)
	resp1, err := client.Get("https://www.okx.com/api/v5/market/tickers?instType=SWAP")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()
	var rawTickers struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&rawTickers); err != nil {
		return nil, err
	}

	// 2. ИСТОРИЯ ФАНДИНГОВ
	fundingMap := make(map[string]struct {
		Rate     float64
		Next     int64
		Interval int
	})
	resp3, err := client.Get("https://www.okx.com/api/v5/public/funding-rate?instId=ANY")
	if err == nil {
		defer resp3.Body.Close()
		var rawFund struct {
			Data []struct {
				InstId          string      `json:"instId"`
				FundingRate     interface{} `json:"fundingRate"`
				FundingTime     interface{} `json:"fundingTime"`
				NextFundingTime interface{} `json:"nextFundingTime"`
			} `json:"data"`
		}
		if json.NewDecoder(resp3.Body).Decode(&rawFund) == nil {
			for _, f := range rawFund.Data {
				ft := int64(utils.ParseFloat(f.FundingTime))
				nt := int64(utils.ParseFloat(f.NextFundingTime))
				intervalHours := int((nt - ft) / 3600000)
				if intervalHours <= 0 {
					intervalHours = 8
				}
				fundingMap[f.InstId] = struct {
					Rate     float64
					Next     int64
					Interval int
				}{Rate: utils.ParseFloat(f.FundingRate), Next: nt, Interval: intervalHours}
			}
		}
	}

	// 3. ИНДЕКСНЫЕ ЦЕНЫ С БИРЖИ (СУЩЕСТВУЮТ ТОЛЬКО БЕЗ '-SWAP')
	indexMap := make(map[string]float64)
	resp4, err := client.Get("https://www.okx.com/api/v5/market/index-tickers?quoteCcy=USDT")
	if err == nil {
		defer resp4.Body.Close()
		var rawIndex struct {
			Data []struct {
				InstId string `json:"instId"`
				IdxPx  string `json:"idxPx"`
			} `json:"data"`
		}
		if json.NewDecoder(resp4.Body).Decode(&rawIndex) == nil {
			for _, idx := range rawIndex.Data {
				indexMap[idx.InstId] = utils.ParseFloat(idx.IdxPx)
			}
		}
	}

	// 4. МАРК-ПРАЙС ИНСТРУМЕНТА (SWAP-ВЕРСИЯ)
	markMap := make(map[string]float64)
	respMark, errMark := client.Get("https://www.okx.com/api/v5/public/mark-price?instType=SWAP")
	if errMark == nil {
		defer respMark.Body.Close()
		var rawMark struct {
			Data []struct {
				InstId string `json:"instId"`
				MarkPx string `json:"markPx"`
			} `json:"data"`
		}
		if json.NewDecoder(respMark.Body).Decode(&rawMark) == nil {
			for _, mk := range rawMark.Data {
				markMap[mk.InstId] = utils.ParseFloat(mk.MarkPx)
			}
		}
	}

	// 5. ПОКАЗАТЕЛИ ЕМКОСТИ СТАКАНА
	ctValMap := make(map[string]float64)
	respInst, errInst := client.Get("https://www.okx.com/api/v5/public/instruments?instType=SWAP")
	if errInst == nil {
		defer respInst.Body.Close()
		var rawInst struct {
			Data []struct {
				InstId string `json:"instId"`
				CtVal  string `json:"ctVal"`
			} `json:"data"`
		}
		if json.NewDecoder(respInst.Body).Decode(&rawInst) == nil {
			for _, idx := range rawInst.Data {
				ctValMap[idx.InstId] = utils.ParseFloat(idx.CtVal)
			}
		}
	}

	var result []okxFuturesTicker
	nowTime := time.Now().UnixMilli()

	for _, t := range rawTickers.Data {
		instId := fmt.Sprintf("%v", t["instId"]) // "BTC-USDT-SWAP"
		if !strings.HasSuffix(instId, "-USDT-SWAP") {
			continue
		}

		bid := utils.ParseFloat(t["bidPx"])
		ask := utils.ParseFloat(t["askPx"])
		if bid <= 0 {
			continue
		}

		symbol := strings.ReplaceAll(strings.TrimSuffix(instId, "-SWAP"), "-", "")
		fundData := fundingMap[instId]

		// 🔥 ЛОГИЧЕСКИЙ ФИКС ПРОИЗОШЁЛ ТУТ 🔥
		// Удаляем -SWAP чтобы маска точно наложилась на ключ словаря: "BTC-USDT"
		baseIndexId := strings.TrimSuffix(instId, "-SWAP")
		indexPrice := indexMap[baseIndexId]
		markPrice := markMap[instId]

		if markPrice <= 0 {
			markPrice = utils.ParseFloat(t["last"])
		}
		if indexPrice <= 0 {
			indexPrice = markPrice // Выстраиваем фейковый паритет только для тех, кто реально пуст (как PRE-IPO OPENAI)
		}

		volScale := ctValMap[instId]
		if volScale <= 0 {
			volScale = 1.0
		}

		result = append(result, okxFuturesTicker{
			Symbol:   symbol,
			BidPrice: bid,
			AskPrice: ask,

			BidSize: utils.ParseFloat(t["bidSz"]) * volScale,
			AskSize: utils.ParseFloat(t["askSz"]) * volScale,

			Timestamp:       nowTime,
			MarkPrice:       markPrice,
			IndexPrice:      indexPrice,
			FundingRate:     fundData.Rate,
			NextFundingTime: fundData.Next,
			FundingInterval: fundData.Interval,
		})
	}
	return result, nil
}
