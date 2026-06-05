package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const binanceSpotTakerFee = 0.1

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.binance.com/api/v3/ticker/bookTicker")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw {
		sym := fmt.Sprintf("%v", t["symbol"])
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}
		bid := utils.ParseFloat(t["bidPrice"])
		ask := utils.ParseFloat(t["askPrice"])

		bidSz := utils.ParseFloat(t["bidQty"])
		askSz := utils.ParseFloat(t["askQty"])

		if bid <= 0 {
			continue
		}
		results = append(results, models.SpotResult{
			Exchange: "binance",
			Symbol:   sym,
			Bid:      bid, Ask: ask,
			BidSize: bidSz, AskSize: askSz,
			TakerFee: binanceSpotTakerFee,
		})
	}
	return results
}
