package bingx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

// Кумулятивный глубокий перебор ставки
func calcAllBingXCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	startTimeLimit := now.Add(-90 * 24 * time.Hour).UnixMilli()

	currentEndTimeMs := int64(0)
	var allRates []float64
	var allTimes []int64

	// Подстраиваем лимит пагинации ровно в сотку для абсолютной гарантии избежания блокировок CloudFlare BingX API.
	for pageCount := 0; pageCount < 40; pageCount++ {
		url := fmt.Sprintf("https://open-api.bingx.com/openApi/swap/v2/quote/fundingRate?symbol=%s&limit=100", symbol)
		if currentEndTimeMs > 0 {
			url = fmt.Sprintf("%s&endTime=%d", url, currentEndTimeMs)
		}

		var raw struct {
			Code int `json:"code"`
			Data []struct {
				FundingRate interface{} `json:"fundingRate"`
				FundingTime int64       `json:"fundingTime"`
			} `json:"data"`
		}

		success := false
		for retry := 0; retry < 5; retry++ {
			resp, err := client.Get(url)
			if err == nil {
				// Базовая защита сессии
				if resp.StatusCode == 429 || resp.StatusCode > 499 {
					resp.Body.Close()
					time.Sleep(3 * time.Second)
					continue
				}
				errDec := json.NewDecoder(resp.Body).Decode(&raw)
				resp.Body.Close()
				if errDec == nil && raw.Code == 0 {
					success = true
					break
				}
			}
			time.Sleep(300 * time.Millisecond) // Пружиним повторный коннект
		}

		if !success || len(raw.Data) == 0 {
			break
		}

		var oldestTimeInBatch int64 = 0

		for _, r := range raw.Data {
			ts := r.FundingTime
			if oldestTimeInBatch == 0 || ts < oldestTimeInBatch {
				oldestTimeInBatch = ts
			}

			if ts >= startTimeLimit {
				rate := utils.ParseFloat(r.FundingRate) * 100.0
				allRates = append(allRates, rate)
				allTimes = append(allTimes, ts)
			}
		}

		if oldestTimeInBatch <= startTimeLimit || len(raw.Data) < 2 {
			break
		}

		currentEndTimeMs = oldestTimeInBatch - 1
		time.Sleep(30 * time.Millisecond) // Прозрачная микропауза перехода блока
	}

	for i, rate := range allRates {
		t := time.UnixMilli(allTimes[i])
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

	return f1d, f3d, f7d, f30d, f90d
}
