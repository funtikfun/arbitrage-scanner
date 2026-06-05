package bitget

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type bitgetFuturesTicker struct {
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

func fetchBitgetFuturesTickers(client *http.Client) ([]bitgetFuturesTicker, error) {
	resp1, err := client.Get("https://api.bitget.com/api/v2/mix/market/tickers?productType=usdt-futures")
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

	intervalMap := make(map[string]int)
	resp2, err := client.Get("https://api.bitget.com/api/v2/mix/market/contracts?productType=usdt-futures")
	if err == nil {
		defer resp2.Body.Close()
		var rawM struct {
			Data []map[string]interface{} `json:"data"`
		}
		if json.NewDecoder(resp2.Body).Decode(&rawM) == nil {
			for _, c := range rawM.Data {
				sym := fmt.Sprintf("%v", c["symbol"])
				iv := int(utils.ParseFloat(c["fundInterval"]))
				if iv <= 0 {
					iv = 8
				}
				intervalMap[sym] = iv
			}
		}
	}

	var result []bitgetFuturesTicker
	nowSeconds := time.Now().UTC().Unix()
	nowMilli := time.Now().UnixMilli()

	for _, t := range rawT.Data {
		sym := fmt.Sprintf("%v", t["symbol"])
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}
		bid := utils.ParseFloat(t["bidPr"])
		if bid <= 0 {
			continue
		}

		interval := intervalMap[sym]
		if interval <= 0 {
			interval = 8
		}
		intervalSeconds := int64(interval * 3600)
		nextFunding := ((nowSeconds / intervalSeconds) + 1) * intervalSeconds * 1000

		result = append(result, bitgetFuturesTicker{
			Symbol:          sym,
			BidPrice:        bid,
			AskPrice:        utils.ParseFloat(t["askPr"]),
			BidSize:         utils.ParseFloat(t["bidSz"]),
			AskSize:         utils.ParseFloat(t["askSz"]),
			Timestamp:       nowMilli,
			MarkPrice:       utils.ParseFloat(t["markPrice"]),
			IndexPrice:      utils.ParseFloat(t["indexPrice"]),
			FundingRate:     utils.ParseFloat(t["fundingRate"]),
			NextFundingTime: nextFunding,
			FundingInterval: interval,
		})
	}
	return result, nil
}
