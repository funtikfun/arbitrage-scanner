package binance

import (
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const binanceTakerFee = 0.04

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 15 * time.Second}
	tickers, err := fetchBinanceTickers(client)
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
			Exchange:    "binance",
			Symbol:      sym,
			BaseCoin:    baseName,
			Multiplier:  mult,
			Bid:         t.BidPrice,
			Ask:         t.AskPrice,
			BidSize:     t.BidSize,
			AskSize:     t.AskSize,
			Timestamp:   t.Timestamp,
			Funding:     t.LastFundingRate,
			NextFunding: t.NextFundingTime,
			Interval:    t.FundingInterval,
			MarkPrice:   t.MarkPrice,
			IndexPrice:  t.IndexPrice,
			Deviation:   dev,
			TakerFee:    binanceTakerFee,
		})
	}
	return results
}
