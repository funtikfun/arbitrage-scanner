package mexc

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type mexcFundingRecord struct {
	FundingRate interface{} `json:"fundingRate"`
	SettleTime  int64       `json:"settleTime"`
}

type mexcFundingHistoryResp struct {
	Success bool `json:"success"`
	Data    struct {
		ResultList []mexcFundingRecord `json:"resultList"`
		TotalPage  int                 `json:"totalPage"`
	} `json:"data"`
}

func getMexcFundingHistory(client *http.Client, symbol string, startTime, endTime int64) ([]mexcFundingRecord, error) {
	const pageSize = 100
	var all []mexcFundingRecord
	pageNum := 1

	for {
		url := fmt.Sprintf("https://contract.mexc.com/api/v1/contract/funding_rate/history?symbol=%s&page_num=%d&page_size=%d",
			symbol, pageNum, pageSize)

		var result mexcFundingHistoryResp
		success := false

		for retry := 0; retry < 5; retry++ {
			resp, err := client.Get(url)
			if err == nil {
				if resp.StatusCode == 429 {
					resp.Body.Close()
					time.Sleep(2 * time.Second) // Защита от 429
					continue
				}
				errDecode := json.NewDecoder(resp.Body).Decode(&result)
				resp.Body.Close()
				if errDecode == nil && result.Success {
					success = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !success || len(result.Data.ResultList) == 0 {
			break // Обрываем, если страницы кончились или фатальный бан
		}

		for _, r := range result.Data.ResultList {
			if r.SettleTime >= startTime && r.SettleTime <= endTime {
				all = append(all, r)
			}
		}
		if pageNum >= result.Data.TotalPage || pageNum > 100 {
			break
		}
		pageNum++
	}
	return all, nil
}

func calcAllMexcCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour)

	records, err := getMexcFundingHistory(client, symbol, start.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	for _, r := range records {
		rate := utils.ParseFloat(r.FundingRate)
		t := time.UnixMilli(r.SettleTime)

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
