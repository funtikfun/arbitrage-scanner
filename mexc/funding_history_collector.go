package mexc

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
	resp, err := client.Get("https://contract.mexc.com/api/v1/contract/funding_rate")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || !raw.Success {
		return nil
	}

	var symbols []string
	for _, f := range raw.Data {
		sym := fmt.Sprintf("%v", f["symbol"])
		if strings.HasSuffix(sym, "_USDT") {
			symbols = append(symbols, sym)
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 8) // На мексе 8 параллельных нитей стабильны

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cleanSym := strings.ReplaceAll(s, "_", "")
			f1d, f3d, f7d, f30d, f90d := calcAllMexcCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(cleanSym)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "mexc",
				Symbol:     cleanSym,
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
