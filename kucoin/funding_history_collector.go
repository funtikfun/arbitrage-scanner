package kucoin

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
	resp, err := client.Get("https://api-futures.kucoin.com/api/v1/contracts/active")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw struct {
		Data []struct {
			Symbol string `json:"symbol"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	var symbols []string
	for _, c := range raw.Data {
		if strings.HasSuffix(c.Symbol, "USDTM") {
			symbols = append(symbols, strings.TrimSuffix(c.Symbol, "M"))
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Теперь штурмуем базу кукоин 10 многопоточными потоками параллельно!
	sem := make(chan struct{}, 10)

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }() // Вырезан Time.Sleep из блока возврата, теперь воркер мгновенно затягивает след монету

			f1d, f3d, f7d, f30d, f90d := calcAllKuCoinCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "kucoin",
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
