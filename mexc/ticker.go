package mexc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type mexcFuturesTicker struct {
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

func fetchMexcFuturesTickers(client *http.Client) ([]mexcFuturesTicker, error) {
	resp1, err := client.Get("https://contract.mexc.com/api/v1/contract/ticker")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()
	var rawT struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&rawT); err != nil {
		return nil, err
	}

	fundingMap := make(map[string]struct {
		Rate     float64
		Next     int64
		Interval int
	})
	resp2, err := client.Get("https://contract.mexc.com/api/v1/contract/funding_rate")
	if err == nil {
		defer resp2.Body.Close()
		var rawF struct {
			Success bool                     `json:"success"`
			Data    []map[string]interface{} `json:"data"`
		}
		if json.NewDecoder(resp2.Body).Decode(&rawF) == nil && rawF.Success {
			for _, f := range rawF.Data {
				sym := fmt.Sprintf("%v", f["symbol"])
				rate := utils.ParseFloat(f["fundingRate"])
				next := int64(utils.ParseFloat(f["nextSettleTime"]))
				interval := int(utils.ParseFloat(f["collectCycle"]))
				if interval <= 0 {
					interval = 8
				}
				fundingMap[sym] = struct {
					Rate     float64
					Next     int64
					Interval int
				}{rate, next, interval}
			}
		}
	}

	// ВЫТАКИВАЕМ ТАБЛИЦУ УЧЁТА НОМИНАЛОВ MEXC, ЕСЛИ ОНИ ВДРУГ ВЕРНУТ ОТДАЧУ СТАКАНОВ (CONTRACTSIZE)
	ctValMap := make(map[string]float64)
	resp3, err3 := client.Get("https://contract.mexc.com/api/v1/contract/detail")
	if err3 == nil {
		defer resp3.Body.Close()
		var rawD struct {
			Data []map[string]interface{} `json:"data"`
		}
		if json.NewDecoder(resp3.Body).Decode(&rawD) == nil {
			for _, r := range rawD.Data {
				ctValMap[fmt.Sprintf("%v", r["symbol"])] = utils.ParseFloat(r["contractSize"])
			}
		}
	}

	var result []mexcFuturesTicker
	nowTime := time.Now().UnixMilli()

	for _, t := range rawT.Data {
		sym := fmt.Sprintf("%v", t["symbol"])
		if !strings.HasSuffix(sym, "_USDT") {
			continue
		}
		bid := utils.ParseFloat(t["bid1"])
		ask := utils.ParseFloat(t["ask1"])
		if bid <= 0 || ask <= 0 {
			continue
		}

		markPrice := utils.ParseFloat(t["fairPrice"])
		if markPrice <= 0 {
			markPrice = (bid + ask) / 2.0
		}
		indexPrice := utils.ParseFloat(t["indexPrice"])
		if indexPrice <= 0 {
			indexPrice = markPrice
		}

		fd, ok := fundingMap[sym]
		fundingRate := 0.0
		nextFunding := int64(0)
		interval := 8
		if ok {
			fundingRate = fd.Rate
			nextFunding = fd.Next
			interval = fd.Interval
		}

		cScale := ctValMap[sym]
		if cScale <= 0 {
			cScale = 1.0
		}

		// MEXC скрывает Volume для быстрого Ticker, поэтому если его нет = Искусственный Флаг '999999' - Заглушка Бесконечности "Можно торгануть"
		bSz := utils.ParseFloat(t["bidVol1"]) * cScale
		if bSz == 0.0 && bid > 0 {
			bSz = 99999999.99 / bid
		}

		aSz := utils.ParseFloat(t["askVol1"]) * cScale
		if aSz == 0.0 && ask > 0 {
			aSz = 99999999.99 / ask
		}

		cleanSym := strings.ReplaceAll(sym, "_", "")
		result = append(result, mexcFuturesTicker{
			Symbol:          cleanSym,
			BidPrice:        bid,
			AskPrice:        ask,
			BidSize:         bSz,
			AskSize:         aSz,
			Timestamp:       nowTime,
			MarkPrice:       markPrice,
			IndexPrice:      indexPrice,
			FundingRate:     fundingRate,
			NextFundingTime: nextFunding,
			FundingInterval: interval,
		})
	}
	return result, nil
}
