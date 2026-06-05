package coinex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type coinexFundingRecord struct {
	Market            string `json:"market"`
	FundingTime       int64  `json:"funding_time"` // В миллисекундах!
	ActualFundingRate string `json:"actual_funding_rate"`
}

type coinexFundingRateHistoryResp struct {
	Code int                   `json:"code"`
	Data []coinexFundingRecord `json:"data"`
}

func getCoinExFundingHistory(client *http.Client, market string) ([]coinexFundingRecord, error) {
	var all []coinexFundingRecord

	// Фандинг у CoinEx начисляется каждые 8 или 24 часа.
	// За 90 дней накопится ~270 записей. Скачаем 3 страницы по 100 за раз.
	for page := 1; page <= 3; page++ {
		url := fmt.Sprintf("https://api.coinex.com/v2/futures/funding-rate-history?market=%s&limit=100&page=%d", market, page)
		resp, err := client.Get(url)
		if err != nil {
			return all, err
		}
		var result coinexFundingRateHistoryResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return all, err
		}
		resp.Body.Close()

		if result.Code != 0 || len(result.Data) == 0 {
			break
		}
		all = append(all, result.Data...)
		if len(result.Data) < 100 {
			break
		}
	}
	return all, nil
}

func calcAllCoinExCumulative(market string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	records, err := getCoinExFundingHistory(client, market)
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	now := time.Now().UTC()

	for _, r := range records {
		rate := utils.ParseFloat(r.ActualFundingRate)
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

	// Возвращаем в виде процентов (как требует сканнер)
	return f1d * 100, f3d * 100, f7d * 100, f30d * 100, f90d * 100
}
