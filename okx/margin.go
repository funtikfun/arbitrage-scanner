package okx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const okxMarginTakerFee = 0.1

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("https://www.okx.com/api/v5/public/instruments?instType=MARGIN")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var raw struct {
		Data []struct {
			InstId string `json:"instId"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var marginSymbols []string
	for _, inst := range raw.Data {
		if strings.HasSuffix(inst.InstId, "-USDT") {
			marginSymbols = append(marginSymbols, strings.ReplaceAll(inst.InstId, "-", ""))
		}
	}

	resp2, err := client.Get("https://www.okx.com/api/v5/market/tickers?instType=SPOT")
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()
	var rawTickers struct {
		Data []struct {
			InstId string `json:"instId"`
			BidPx  string `json:"bidPx"`
			AskPx  string `json:"askPx"`
			BidSz  string `json:"bidSz"`
			AskSz  string `json:"askSz"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rawTickers); err != nil {
		return nil
	}

	tickerMap := make(map[string][]float64)
	for _, t := range rawTickers.Data {
		symbol := strings.ReplaceAll(t.InstId, "-", "")
		bid := utils.ParseFloat(t.BidPx)
		ask := utils.ParseFloat(t.AskPx)
		bSz := utils.ParseFloat(t.BidSz)
		aSz := utils.ParseFloat(t.AskSz)
		if bid > 0 {
			tickerMap[symbol] = []float64{bid, ask, bSz, aSz}
		}
	}

	var results []models.MarginResult
	for _, sym := range marginSymbols {
		prices, ok := tickerMap[sym]
		if !ok || prices[0] <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "okx", Symbol: sym,
			Bid: prices[0], Ask: prices[1],
			BidSize: prices[2], AskSize: prices[3],
			TakerFee: okxMarginTakerFee,
		})
	}
	return results
}
