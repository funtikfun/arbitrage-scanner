package bitget

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bitgetMarginTakerFee = 0.1

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("https://api.bitget.com/api/v3/market/instruments?category=MARGIN")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Code string `json:"code"`
		Data []struct {
			Symbol    string `json:"symbol"`
			QuoteCoin string `json:"quoteCoin"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != "00000" {
		return nil
	}

	var marginSyms []string
	for _, inst := range raw.Data {
		if inst.QuoteCoin == "USDT" {
			marginSyms = append(marginSyms, inst.Symbol)
		}
	}
	if len(marginSyms) == 0 {
		return nil
	}

	resp2, err := client.Get("https://api.bitget.com/api/v2/spot/market/tickers")
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()

	var rawTickers struct {
		Data []struct {
			Symbol string `json:"symbol"`
			BidPr  string `json:"bidPr"`
			AskPr  string `json:"askPr"`
			BidSz  string `json:"bidSz"`
			AskSz  string `json:"askSz"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rawTickers); err != nil {
		return nil
	}

	tickerMap := make(map[string][]float64)
	for _, t := range rawTickers.Data {
		bid := utils.ParseFloat(t.BidPr)
		ask := utils.ParseFloat(t.AskPr)
		bSz := utils.ParseFloat(t.BidSz)
		aSz := utils.ParseFloat(t.AskSz)
		if bid > 0 {
			tickerMap[t.Symbol] = []float64{bid, ask, bSz, aSz}
		}
	}

	var results []models.MarginResult
	for _, sym := range marginSyms {
		prices, ok := tickerMap[sym]
		if !ok || prices[0] <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "bitget", Symbol: sym,
			Bid: prices[0], Ask: prices[1], BidSize: prices[2], AskSize: prices[3], TakerFee: bitgetMarginTakerFee,
		})
	}
	return results
}
