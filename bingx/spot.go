package bingx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bingxSpotTakerFee = 0.1

type bingxSpotTickerResp struct {
	Code int `json:"code"`
	Data []struct {
		Symbol   string      `json:"symbol"`
		BidPrice interface{} `json:"bidPrice"`
		BidQty   interface{} `json:"bidQty"` // Реальный объем
		AskPrice interface{} `json:"askPrice"`
		AskQty   interface{} `json:"askQty"` // Реальный объем
	} `json:"data"`
}

func FetchSpot() []models.SpotResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://open-api.bingx.com/openApi/spot/v1/ticker/24hr")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw bingxSpotTickerResp
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != 0 {
		return nil
	}

	var results []models.SpotResult
	for _, t := range raw.Data {
		if !strings.HasSuffix(t.Symbol, "-USDT") {
			continue
		}

		bid := utils.ParseFloat(t.BidPrice)
		ask := utils.ParseFloat(t.AskPrice)
		bidSz := utils.ParseFloat(t.BidQty)
		askSz := utils.ParseFloat(t.AskQty)

		if bid <= 0 || ask <= 0 {
			continue
		}

		cleanSym := strings.ReplaceAll(t.Symbol, "-", "")

		results = append(results, models.SpotResult{
			Exchange: "bingx",
			Symbol:   cleanSym,
			Bid:      bid,
			Ask:      ask,
			BidSize:  bidSz,
			AskSize:  askSz,
			TakerFee: bingxSpotTakerFee,
		})
	}
	return results
}
