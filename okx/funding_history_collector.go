package okx

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

func FetchFundingHistory() []models.FundingHistoryResult {
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get("https://www.okx.com/api/v5/public/instruments?instType=SWAP")
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

	var symbols []string
	for _, inst := range raw.Data {
		if strings.HasSuffix(inst.InstId, "-USDT-SWAP") {
			symbol := strings.ReplaceAll(inst.InstId, "-", "")
			symbol = strings.TrimSuffix(symbol, "SWAP")
			symbols = append(symbols, symbol)
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

			instId := strings.ReplaceAll(s, "USDT", "-USDT-SWAP")
			f1d, f3d, f7d, f30d, f90d := calcAllOkxCumulative(instId, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "okx",
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
