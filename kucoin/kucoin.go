package kucoin

import (
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const kucoinFuturesTakerFee = 0.06

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 10 * time.Second}
	tickers, err := fetchKuCoinFuturesTickers(client)
	if err != nil || len(tickers) == 0 {
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
			Exchange:    "kucoin",
			Symbol:      sym,
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
			TakerFee:    kucoinFuturesTakerFee,
		})
	}
	return results
}
