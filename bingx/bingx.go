package bingx

import (
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 10 * time.Second}
	tickers, err := fetchBingXFuturesTickers(client)
	if err != nil {
		return nil
	}

	var results []models.FutureResult
	for _, t := range tickers {
		dev := 0.0
		if t.IndexPrice > 0 {
			dev = (t.MarkPrice - t.IndexPrice) / t.IndexPrice * 100.0
		}

		// 🔴 Множитель для цен мы берем СТРОГО из нормализатора Сканнера (1.0 для BTC/HIGH, 1000.0 для 1000PEPE)
		baseName, mult := utils.ParseSymbolMeta(t.Symbol)

		results = append(results, models.FutureResult{
			Exchange:    "bingx",
			Symbol:      t.Symbol,
			BaseCoin:    baseName,
			Multiplier:  mult, // 🔴 Здесь должен быть только чистый mult (1.0 или 1000.0)!
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
			TakerFee:    t.TakerFee,
		})
	}

	return results
}
