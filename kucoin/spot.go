package kucoin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const kucoinSpotTakerFee = 0.1

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.kucoin.com/api/v1/market/allTickers")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Data struct {
			Ticker []struct {
				Symbol string `json:"symbol"`
				Buy    string `json:"buy"`
				Sell   string `json:"sell"`
			} `json:"ticker"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Data.Ticker {
		if !strings.HasSuffix(t.Symbol, "-USDT") {
			continue
		}
		bid := utils.ParseFloat(t.Buy)
		ask := utils.ParseFloat(t.Sell)
		if bid <= 0 {
			continue
		}
		clean := strings.ReplaceAll(t.Symbol, "-", "")

		results = append(results, models.SpotResult{
			Exchange: "kucoin", Symbol: clean, Bid: bid, Ask: ask,
			BidSize: 0, AskSize: 0, TakerFee: kucoinSpotTakerFee,
		})
	}
	return results
}
