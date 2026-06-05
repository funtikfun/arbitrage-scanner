package bybit

import (
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bybitFuturesTakerFee = 0.055

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 10 * time.Second}
	tickers, err := fetchBybitFuturesTickers(client)
	if err != nil {
		return nil
	}

	var results []models.FutureResult
	for sym, t := range tickers {
		dev := 0.0
		if t.IndexPrice > 0 {
			dev = (t.MarkPrice - t.IndexPrice) / t.IndexPrice * 100.0
		}
		baseName, mult := utils.ParseSymbolMeta(sym)

		results = append(results, models.FutureResult{
			Exchange:   "bybit",
			Symbol:     sym,
			BaseCoin:   baseName,
			Multiplier: mult,
			Bid:        t.BidPrice,
			Ask:        t.AskPrice,

			// --- ПРОБРАСЫВАЕМ В ОБЩЕЕ ЯДРО ---
			BidSize:   t.BidSize,
			AskSize:   t.AskSize,
			Timestamp: t.Timestamp,

			Funding:     t.FundingRate,
			NextFunding: t.NextFundingTime,
			Interval:    t.FundingInterval,
			MarkPrice:   t.MarkPrice,
			IndexPrice:  t.IndexPrice,
			Deviation:   dev,
			TakerFee:    bybitFuturesTakerFee,
		})
	}
	return results
}
