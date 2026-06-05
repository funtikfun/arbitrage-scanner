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

const bybitMarginTakerFee = 0.1 // 0.1%

func FetchMargin() []models.MarginResult {
	client := &http.Client{Timeout: 10 * time.Second}

	type instrument struct {
		Symbol        string `json:"symbol"`
		MarginTrading string `json:"marginTrading"`
	}
	resp, err := client.Get("https://api.bybit.com/v5/market/instruments-info?category=spot&limit=1000")
	if err != nil {
		fmt.Printf("❌ Bybit Margin instruments: %v\n", err)
		return nil
	}
	defer resp.Body.Close()
	var raw struct {
		Result struct {
			List []instrument `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		fmt.Printf("❌ Bybit Margin decode: %v\n", err)
		return nil
	}

	var marginSyms []string
	for _, inst := range raw.Result.List {
		if inst.MarginTrading != "" && strings.HasSuffix(inst.Symbol, "USDT") {
			marginSyms = append(marginSyms, inst.Symbol)
		}
	}

	resp2, err := client.Get("https://api.bybit.com/v5/market/tickers?category=spot")
	if err != nil {
		fmt.Printf("❌ Bybit Spot tickers: %v\n", err)
		return nil
	}
	defer resp2.Body.Close()
	var tickerRaw struct {
		Result struct {
			List []map[string]interface{} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&tickerRaw); err != nil {
		fmt.Printf("❌ Bybit Spot tickers decode: %v\n", err)
		return nil
	}

	tickerMap := make(map[string][]float64)
	for _, t := range tickerRaw.Result.List {
		sym := fmt.Sprintf("%v", t["symbol"])
		bid := utils.ParseFloat(t["bid1Price"])
		ask := utils.ParseFloat(t["ask1Price"])
		bidSz := utils.ParseFloat(t["bid1Size"])
		askSz := utils.ParseFloat(t["ask1Size"])

		if bid > 0 {
			tickerMap[sym] = []float64{bid, ask, bidSz, askSz}
		}
	}

	var results []models.MarginResult
	for _, sym := range marginSyms {
		dataItem, ok := tickerMap[sym]
		if !ok || dataItem[0] <= 0 {
			continue
		}
		results = append(results, models.MarginResult{
			Exchange: "bybit",
			Symbol:   sym,
			Bid:      dataItem[0],
			Ask:      dataItem[1],
			BidSize:  dataItem[2],
			AskSize:  dataItem[3],
			TakerFee: bybitMarginTakerFee,
		})
	}

	return results
}
