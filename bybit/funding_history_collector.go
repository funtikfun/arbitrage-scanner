package bybit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

func FetchFundingHistory() []models.FundingHistoryResult {
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get("https://api.bybit.com/v5/market/tickers?category=linear")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Result struct {
			List []map[string]interface{} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var symbols []string
	for _, t := range raw.Result.List {
		sym := fmt.Sprintf("%v", t["symbol"])
		if strings.HasSuffix(sym, "USDT") {
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

			f1d, f3d, f7d, f30d, f90d := calcAllBybitCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "bybit",
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
