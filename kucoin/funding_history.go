package kucoin

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

type kuCoinFundingRecord struct {
	FundingRate float64 `json:"fundingRate"`
	Timepoint   int64   `json:"timepoint"`
}

type kuCoinFundingResponse struct {
	Code string                `json:"code"`
	Data []kuCoinFundingRecord `json:"data"`
}

func getKuCoinFundingHistory(client *http.Client, symbol string, startTime, endTime int64) ([]kuCoinFundingRecord, error) {
	symbolWithM := symbol + "M"
	const segmentDuration = 10 * 24 * time.Hour
	var allRecords []kuCoinFundingRecord

	segStart := time.UnixMilli(startTime)
	finish := time.UnixMilli(endTime)

	for segStart.Before(finish) {
		segEnd := segStart.Add(segmentDuration)
		if segEnd.After(finish) {
			segEnd = finish
		}
		url := fmt.Sprintf("https://api-futures.kucoin.com/api/v1/contract/funding-rates?symbol=%s&from=%d&to=%d",
			symbolWithM, segStart.UnixMilli(), segEnd.UnixMilli())

		var result kuCoinFundingResponse
		success := false

		// Увеличили количество попыток до 5. И добавили обработку 429
		for retry := 0; retry < 5; retry++ {
			resp, err := client.Get(url)
			if err == nil {
				if resp.StatusCode == 429 {
					resp.Body.Close()
					time.Sleep(2 * time.Second) // Жесткий сон при бане
					continue
				}
				errDecode := json.NewDecoder(resp.Body).Decode(&result)
				resp.Body.Close()
				if errDecode == nil && result.Code == "200000" {
					success = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if success {
			allRecords = append(allRecords, result.Data...)
			segStart = segEnd // Переходим к следующему куску ТОЛЬКО при успехе
		} else {
			// Если биржа намертво заблокировала, прерываем этот токен, чтобы не зависнуть
			break
		}
	}
	return allRecords, nil
}

func calcAllKuCoinCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour)

	records, err := getKuCoinFundingHistory(client, symbol, start.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	for _, r := range records {
		rate := r.FundingRate
		t := time.UnixMilli(r.Timepoint)

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

	round := func(v float64) float64 { return math.Round(v*100*10000) / 10000 }
	return round(f1d), round(f3d), round(f7d), round(f30d), round(f90d)
}
