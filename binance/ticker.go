package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type binanceTicker struct {
	Symbol          string
	BidPrice        float64
	AskPrice        float64
	BidSize         float64
	AskSize         float64
	Timestamp       int64
	MarkPrice       float64
	IndexPrice      float64
	LastFundingRate float64
	NextFundingTime int64
	FundingInterval int
}

func fetchBinanceTickers(client *http.Client) (map[string]binanceTicker, error) {
	// 1. bookTicker
	resp1, err := client.Get("https://fapi.binance.com/fapi/v1/ticker/bookTicker")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()
	var rawPrices []map[string]interface{}
	if err := json.NewDecoder(resp1.Body).Decode(&rawPrices); err != nil {
		return nil, err
	}

	type pxSz struct{ Bid, Ask, Bq, Aq float64 }
	priceMap := make(map[string]pxSz)

	for _, p := range rawPrices {
		sym := fmt.Sprintf("%v", p["symbol"])
		priceMap[sym] = pxSz{
			Bid: utils.ParseFloat(p["bidPrice"]),
			Ask: utils.ParseFloat(p["askPrice"]),
			Bq:  utils.ParseFloat(p["bidQty"]),
			Aq:  utils.ParseFloat(p["askQty"]),
		}
	}

	// 2. premiumIndex
	resp2, err := client.Get("https://fapi.binance.com/fapi/v1/premiumIndex")
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var rawPrem []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&rawPrem); err != nil {
		return nil, err
	}

	// 3. fundingInfo
	intervalMap := make(map[string]int)
	resp3, err := client.Get("https://fapi.binance.com/fapi/v1/fundingInfo")
	if err == nil {
		defer resp3.Body.Close()
		var fundingInfo []struct {
			Symbol               string `json:"symbol"`
			FundingIntervalHours int    `json:"fundingIntervalHours"`
		}
		if err := json.NewDecoder(resp3.Body).Decode(&fundingInfo); err == nil {
			for _, fi := range fundingInfo {
				if fi.FundingIntervalHours > 0 {
					intervalMap[fi.Symbol] = fi.FundingIntervalHours
				}
			}
		}
	}

	// 4. Сборка результата
	result := make(map[string]binanceTicker)
	nowTime := time.Now().UnixMilli()

	for _, pr := range rawPrem {
		sym := fmt.Sprintf("%v", pr["symbol"])
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}
		prices, ok := priceMap[sym]
		if !ok || prices.Bid <= 0 {
			continue
		}

		interval := intervalMap[sym]
		if interval == 0 {
			interval = 8
		}

		result[sym] = binanceTicker{
			Symbol:          sym,
			BidPrice:        prices.Bid,
			AskPrice:        prices.Ask,
			BidSize:         prices.Bq,
			AskSize:         prices.Aq,
			Timestamp:       nowTime,
			MarkPrice:       utils.ParseFloat(pr["markPrice"]),
			IndexPrice:      utils.ParseFloat(pr["indexPrice"]),
			LastFundingRate: utils.ParseFloat(pr["lastFundingRate"]),
			NextFundingTime: int64(utils.ParseFloat(pr["nextFundingTime"])),
			FundingInterval: interval,
		}
	}
	return result, nil
}
