package coinex

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const coinexSpotTakerFee = 0.2 // Базовая спотовая комиссия 0.2%

// Объявляем общую структуру тикеров V1, содержащую bids/asks,
// которая будет доступна для спота и для маржи
type CoinexSpotTickerAll struct {
	Code int `json:"code"`
	Data struct {
		Date   int64 `json:"date"`
		Ticker map[string]struct {
			Buy        string `json:"buy"`
			BuyAmount  string `json:"buy_amount"`
			Sell       string `json:"sell"`
			SellAmount string `json:"sell_amount"`
			Last       string `json:"last"`
		} `json:"ticker"`
	} `json:"data"`
}

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.coinex.com/v1/market/ticker/all")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw CoinexSpotTickerAll
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var results []models.SpotResult
	for sym, t := range raw.Data.Ticker {
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}
		bid := utils.ParseFloat(t.Buy)
		ask := utils.ParseFloat(t.Sell)
		bidSz := utils.ParseFloat(t.BuyAmount)
		askSz := utils.ParseFloat(t.SellAmount)

		if bid <= 0 {
			continue
		}
		results = append(results, models.SpotResult{
			Exchange: "coinex",
			Symbol:   sym,
			Bid:      bid, Ask: ask,
			BidSize: bidSz, AskSize: askSz,
			TakerFee: coinexSpotTakerFee,
		})
	}
	return results
}
