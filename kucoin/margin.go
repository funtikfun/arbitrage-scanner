package kucoin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const kucoinMarginTakerFee = 0.1

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.kucoin.com/api/v1/isolated/symbols")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var rawSymbols struct {
		Data []struct {
			Symbol        string `json:"symbol"`
			QuoteCurrency string `json:"quoteCurrency"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawSymbols); err != nil {
		return nil
	}

	var marginSyms []string
	for _, s := range rawSymbols.Data {
		if s.QuoteCurrency == "USDT" {
			marginSyms = append(marginSyms, strings.ReplaceAll(s.Symbol, "-", ""))
		}
	}
	if len(marginSyms) == 0 {
		return nil
	}

	resp2, err := client.Get("https://api.kucoin.com/api/v1/market/allTickers")
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()
	var rawTickers struct {
		Data struct {
			Ticker []struct {
				Symbol string `json:"symbol"`
				Buy    string `json:"buy"`
				Sell   string `json:"sell"`
			} `json:"ticker"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rawTickers); err != nil {
		return nil
	}
	tickerMap := make(map[string][]float64)
	for _, t := range rawTickers.Data.Ticker {
		clean := strings.ReplaceAll(t.Symbol, "-", "")
		bid := utils.ParseFloat(t.Buy)
		ask := utils.ParseFloat(t.Sell)
		if bid > 0 {
			tickerMap[clean] = []float64{bid, ask}
		}
	}

	var results []models.MarginResult
	for _, sym := range marginSyms {
		prices, ok := tickerMap[sym]
		if !ok || prices[0] <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "kucoin", Symbol: sym,
			Bid: prices[0], Ask: prices[1],
			BidSize: 0, AskSize: 0, TakerFee: kucoinMarginTakerFee,
		})
	}
	return results
}
