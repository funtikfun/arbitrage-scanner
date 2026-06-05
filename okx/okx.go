package okx

import (
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const okxFuturesTakerFee = 0.05

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 10 * time.Second}
	tickers, err := fetchOkxFuturesTickers(client)
	if err != nil {
		return nil
	}

	var results []models.FutureResult
	for _, t := range tickers {
		dev := 0.0
		if t.IndexPrice > 0 {
			dev = (t.MarkPrice - t.IndexPrice) / t.IndexPrice * 100.0
		}
		baseName, mult := utils.ParseSymbolMeta(t.Symbol)

		results = append(results, models.FutureResult{
			Exchange:    "okx",
			Symbol:      t.Symbol,
			BaseCoin:    baseName,
			Multiplier:  mult,
			Bid:         t.BidPrice,
			Ask:         t.AskPrice,
			BidSize:     t.BidSize,
			AskSize:     t.AskSize,
			Timestamp:   t.Timestamp,
			Funding:     t.FundingRate,
			NextFunding: t.NextFundingTime,
			Interval:    t.FundingInterval,
			MarkPrice:   t.MarkPrice,
			IndexPrice:  t.IndexPrice,
			Deviation:   dev,
			TakerFee:    okxFuturesTakerFee,
		})
	}
	return results
}
