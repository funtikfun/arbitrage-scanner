package okx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const okxSpotTakerFee = 0.1

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://www.okx.com/api/v5/market/tickers?instType=SPOT")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Data []struct {
			InstId string `json:"instId"`
			BidPx  string `json:"bidPx"`
			AskPx  string `json:"askPx"`
			BidSz  string `json:"bidSz"`
			AskSz  string `json:"askSz"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Data {
		if !strings.HasSuffix(t.InstId, "-USDT") {
			continue
		}
		bid := utils.ParseFloat(t.BidPx)
		ask := utils.ParseFloat(t.AskPx)

		bidSz := utils.ParseFloat(t.BidSz)
		askSz := utils.ParseFloat(t.AskSz)

		if bid <= 0 {
			continue
		}
		symbol := strings.ReplaceAll(t.InstId, "-", "")
		results = append(results, models.SpotResult{
			Exchange: "okx",
			Symbol:   symbol,
			Bid:      bid, Ask: ask,
			BidSize: bidSz, AskSize: askSz,
			TakerFee: okxSpotTakerFee,
		})
	}
	return results
}
