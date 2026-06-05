package bingx

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

	resp, err := client.Get("https://open-api.bingx.com/openApi/swap/v2/quote/contracts")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var cInfo bingxQuoteContractData
	if err := json.NewDecoder(resp.Body).Decode(&cInfo); err != nil {
		return nil
	}

	var symbols []string
	for _, m := range cInfo.Data {
		if (m.Status == 1 || m.Status == 2) && strings.HasSuffix(m.Symbol, "-USDT") {
			if strings.Contains(m.Symbol, "USDC") {
				continue
			}
			symbols = append(symbols, m.Symbol)
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 7) // Легчайше проедает базу за пару минут!

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }() // Вырваны слипы блокировщика за ненадобностью с проксей

			f1d, f3d, f7d, f30d, f90d := calcAllBingXCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)
			cleanSym := strings.ReplaceAll(s, "-", "")

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "bingx",
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
