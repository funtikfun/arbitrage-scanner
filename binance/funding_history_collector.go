package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

func FetchFundingHistory() []models.FundingHistoryResult {
	client := &http.Client{Timeout: 15 * time.Second}
	var symbols []string
	resp, err := client.Get("https://fapi.binance.com/fapi/v1/ticker/bookTicker")
	if err != nil {
		return nil
	}
	var rawTickers []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawTickers); err != nil {
		resp.Body.Close()
		return nil
	}
	resp.Body.Close()

	for _, t := range rawTickers {
		sym := fmt.Sprintf("%v", t["symbol"])
		if len(sym) > 4 && sym[len(sym)-4:] == "USDT" {
			symbols = append(symbols, sym)
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			f1d, f3d, f7d, f30d, f90d := calcAllBinanceCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s) // <— Встроенная метрика связи для мэтчинга

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "binance",
				Symbol:     s,
				BaseCoin:   baseCoin,
				Funding1D:  f1d,
				Funding3D:  f3d,
				Funding7D:  f7d,
				Funding30D: f30d,
				Funding90D: f90d,
			})
			mu.Unlock()
		}(sym)
	}

	wg.Wait()
	return results
}
