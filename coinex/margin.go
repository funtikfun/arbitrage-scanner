package coinex

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const coinexMarginTakerFee = 0.2 // Маржинальная комиссия

type coinexSpotMarketStatus struct {
	Code int `json:"code"`
	Data []struct {
		Market            string `json:"market"`
		IsMarginAvailable bool   `json:"is_margin_available"`
		QuoteCcy          string `json:"quote_ccy"`
	} `json:"data"`
}

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Узнаем, у каких спотовых рынков включена маржинальная торговля в USDT
	resp1, err := client.Get("https://api.coinex.com/v2/spot/market")
	if err != nil {
		return nil
	}
	defer resp1.Body.Close()

	var markets coinexSpotMarketStatus
	if err := json.NewDecoder(resp1.Body).Decode(&markets); err != nil {
		return nil
	}

	marginMarkets := make(map[string]bool)
	for _, m := range markets.Data {
		if m.IsMarginAvailable && m.QuoteCcy == "USDT" {
			marginMarkets[m.Market] = true
		}
	}

	if len(marginMarkets) == 0 {
		return nil
	}

	// 2. Стягиваем тикеры стаканов
	resp2, err := client.Get("https://api.coinex.com/v1/market/ticker/all")
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()

	var tickers CoinexSpotTickerAll
	if err := json.NewDecoder(resp2.Body).Decode(&tickers); err != nil {
		return nil
	}

	var results []models.MarginResult
	for sym, t := range tickers.Data.Ticker {
		if !marginMarkets[sym] {
			continue
		}
		bid := utils.ParseFloat(t.Buy)
		ask := utils.ParseFloat(t.Sell)
		bidSz := utils.ParseFloat(t.BuyAmount)
		askSz := utils.ParseFloat(t.SellAmount)

		if bid <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "coinex",
			Symbol:   sym,
			Bid:      bid, Ask: ask,
			BidSize: bidSz, AskSize: askSz,
			TakerFee: coinexMarginTakerFee,
		})
	}
	return results
}
