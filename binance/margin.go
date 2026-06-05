package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const binanceMarginTakerFee = 0.1

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("https://api.binance.com/api/v3/exchangeInfo")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var info struct {
		Symbols []struct {
			Symbol         string     `json:"symbol"`
			Status         string     `json:"status"`
			QuoteAsset     string     `json:"quoteAsset"`
			PermissionSets [][]string `json:"permissionSets"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil
	}

	var marginSymbols []string
	for _, s := range info.Symbols {
		if s.Status != "TRADING" || s.QuoteAsset != "USDT" {
			continue
		}
		hasMargin := false
		for _, permSet := range s.PermissionSets {
			for _, p := range permSet {
				if p == "MARGIN" {
					hasMargin = true
					break
				}
			}
			if hasMargin {
				break
			}
		}
		if hasMargin {
			marginSymbols = append(marginSymbols, s.Symbol)
		}
	}

	if len(marginSymbols) == 0 {
		return nil
	}

	respT, err := client.Get("https://api.binance.com/api/v3/ticker/bookTicker")
	if err != nil {
		return nil
	}
	defer respT.Body.Close()

	var raw []map[string]interface{}
	if err := json.NewDecoder(respT.Body).Decode(&raw); err != nil {
		return nil
	}

	tickerMap := make(map[string][]float64)
	for _, t := range raw {
		sym := fmt.Sprintf("%v", t["symbol"])
		bid := utils.ParseFloat(t["bidPrice"])
		ask := utils.ParseFloat(t["askPrice"])
		bSz := utils.ParseFloat(t["bidQty"])
		aSz := utils.ParseFloat(t["askQty"])
		if bid > 0 {
			tickerMap[sym] = []float64{bid, ask, bSz, aSz}
		}
	}

	var results []models.MarginResult
	for _, sym := range marginSymbols {
		prices, ok := tickerMap[sym]
		if !ok || prices[0] <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "binance", Symbol: sym,
			Bid: prices[0], Ask: prices[1],
			BidSize: prices[2], AskSize: prices[3],
			TakerFee: binanceMarginTakerFee,
		})
	}
	return results
}
