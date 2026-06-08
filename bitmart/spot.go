package bitmart

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bitmartSpotTakerFee = 0.1 // Базовая спотовая комиссия 0.1%

type bitmartSpotTickersResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		Symbol    string `json:"symbol"`
		LastPrice string `json:"last_price"`
		BidPx     string `json:"bid_px"`
		BidSz     string `json:"bid_sz"`
		AskPx     string `json:"ask_px"`
		AskSz     string `json:"ask_sz"`
	} `json:"data"`
}

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api-cloud.bitmart.com/spot/quotation/v3/tickers")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw bitmartSpotTickersResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != 1000 {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Data {
		if !strings.HasSuffix(t.Symbol, "_USDT") {
			continue
		}
		bid := utils.ParseFloat(t.BidPx)
		ask := utils.ParseFloat(t.AskPx)
		bidSz := utils.ParseFloat(t.BidSz)
		askSz := utils.ParseFloat(t.AskSz)

		if bid <= 0 {
			continue
		}

		// Конвертируем маску BTC_USDT -> BTCUSDT для сопоставления ядра
		cleanSym := strings.ReplaceAll(t.Symbol, "_", "")

		results = append(results, models.SpotResult{
			Exchange: "bitmart",
			Symbol:   cleanSym,
			Bid:      bid,
			Ask:      ask,
			BidSize:  bidSz,
			AskSize:  askSz,
			TakerFee: bitmartSpotTakerFee,
		})
	}
	return results
}
