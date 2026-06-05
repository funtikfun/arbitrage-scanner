package coinex

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

func FetchFundingHistory() []models.FundingHistoryResult {
	client := &http.Client{Timeout: 15 * time.Second}

	// 1. Выгружаем список фьючерсов
	resp, err := client.Get("https://api.coinex.com/v2/futures/market")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var markets coinexMarketInfo
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil
	}

	var symbols []string
	for _, m := range markets.Data {
		if m.ContractType == "linear" && m.QuoteCcy == "USDT" {
			symbols = append(symbols, m.Market)
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // Не душим API, скачиваем параллельно в 5 потоков

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			f1d, f3d, f7d, f30d, f90d := calcAllCoinExCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "coinex",
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
