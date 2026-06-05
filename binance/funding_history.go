package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type binanceFundingRecord struct {
	FundingTime int64  `json:"fundingTime"` // миллисекунды
	FundingRate string `json:"fundingRate"`
}

func getBinanceFundingSegment(client *http.Client, symbol string, startTime, endTime int64) ([]binanceFundingRecord, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/fundingRate?symbol=%s&startTime=%d&endTime=%d&limit=1000", symbol, startTime, endTime)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var records []binanceFundingRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, err
	}
	return records, nil
}

// calcAllBinanceCumulative скачивает историю за 90 дней один раз и распределяет по периодам
func calcAllBinanceCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour) // Запрашиваем сразу максимум (90 дней)

	const segmentDays = 30
	segStart := start

	var allRecords []binanceFundingRecord

	for segStart.Before(now) {
		segEnd := segStart.Add(segmentDays * 24 * time.Hour)
		if segEnd.After(now) {
			segEnd = now
		}
		records, err := getBinanceFundingSegment(client, symbol, segStart.UnixMilli(), segEnd.UnixMilli())
		if err != nil {
			fmt.Printf("⚠️ Binance %s segment error: %v\n", symbol, err)
		} else {
			allRecords = append(allRecords, records...)
		}
		segStart = segEnd
	}

	// Считаем все интервалы за один проход по памяти
	for _, r := range allRecords {
		rate := utils.ParseFloat(r.FundingRate)
		t := time.UnixMilli(r.FundingTime)

		hours := now.Sub(t).Hours()
		if hours <= 24 {
			f1d += rate
		}
		if hours <= 3*24 {
			f3d += rate
		}
		if hours <= 7*24 {
			f7d += rate
		}
		if hours <= 30*24 {
			f30d += rate
		}
		if hours <= 90*24 {
			f90d += rate
		}
	}

	return f1d * 100, f3d * 100, f7d * 100, f30d * 100, f90d * 100
}
