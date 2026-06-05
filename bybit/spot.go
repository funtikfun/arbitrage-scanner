package bybit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bybitSpotTakerFee = 0.1 // 0.1%

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.bybit.com/v5/market/tickers?category=spot")
	if err != nil {
		fmt.Printf("❌ Bybit Spot: %v\n", err)
		return nil
	}
	defer resp.Body.Close()
	var raw struct {
		Result struct {
			List []map[string]interface{} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		fmt.Printf("❌ Bybit Spot decode: %v\n", err)
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Result.List {
		sym := fmt.Sprintf("%v", t["symbol"])
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}

		bid := utils.ParseFloat(t["bid1Price"])
		ask := utils.ParseFloat(t["ask1Price"])
		// Тот самый вытаскивающий обьем!
		bidSz := utils.ParseFloat(t["bid1Size"])
		askSz := utils.ParseFloat(t["ask1Size"])

		if bid <= 0 {
			continue
		}
		results = append(results, models.SpotResult{
			Exchange: "bybit",
			Symbol:   sym,
			Bid:      bid,
			Ask:      ask,
			BidSize:  bidSz,
			AskSize:  askSz,
			TakerFee: bybitSpotTakerFee,
		})
	}
	return results
}
