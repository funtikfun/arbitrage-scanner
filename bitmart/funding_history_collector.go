package bitmart

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

	resp, err := client.Get("https://api-cloud-v2.bitmart.com/contract/public/details")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw bitmartContractDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != 1000 {
		return nil
	}

	var symbols []string
	for _, t := range raw.Data.Symbols {
		if t.QuoteCurrency == "USDT" {
			// 🛡️ АМПУТАЦИЯ 40034 ОШИБОК API: Собираем историю ИСКЛЮЧИТЕЛЬНО по активно-горячим тикерам с нормальным дневным прокрутом:
			if utils.ParseFloat(t.Turnover24h) >= 1000.0 {
				symbols = append(symbols, t.Symbol)
			}
		}
	}

	var results []models.FundingHistoryResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Мгновенная прокачка токенов — после фильтрации их будет всего ~130. Конвейер съест всё без CF таймаутов в один 2х канальный поток!
	sem := make(chan struct{}, 2)

	for _, sym := range symbols {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			f1d, f3d, f7d, f30d, f90d := calcAllBitMartCumulative(s, client)
			baseCoin, _ := utils.ParseSymbolMeta(s)

			mu.Lock()
			results = append(results, models.FundingHistoryResult{
				Exchange:   "bitmart",
				Symbol:     s,
				BaseCoin:   baseCoin,
				Funding1D:  f1d,
				Funding3D:  f3d,
				Funding7D:  f7d,
				Funding30D: f30d,
				Funding90D: f90d,
			})
			mu.Unlock()

			// Короткий отдых между итерациями, защита потоковой ленты BitMart от rate ban
			time.Sleep(200 * time.Millisecond)
		}(sym)
	}

	wg.Wait()
	return results
}
