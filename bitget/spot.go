package bitget

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bitgetSpotTakerFee = 0.1

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.bitget.com/api/v2/spot/market/tickers")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Data []struct {
			Symbol string `json:"symbol"`
			BidPr  string `json:"bidPr"`
			AskPr  string `json:"askPr"`
			BidSz  string `json:"bidSz"`
			AskSz  string `json:"askSz"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Data {
		if !strings.HasSuffix(t.Symbol, "USDT") {
			continue
		}
		bid := utils.ParseFloat(t.BidPr)
		ask := utils.ParseFloat(t.AskPr)

		bidSz := utils.ParseFloat(t.BidSz)
		askSz := utils.ParseFloat(t.AskSz)

		if bid <= 0 {
			continue
		}
		results = append(results, models.SpotResult{
			Exchange: "bitget", Symbol: t.Symbol,
			Bid: bid, Ask: ask, BidSize: bidSz, AskSize: askSz, TakerFee: bitgetSpotTakerFee,
		})
	}
	return results
}
