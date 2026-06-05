package okx

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type okxFundingRecord struct {
	FundingRate string `json:"fundingRate"`
	FundingTime string `json:"fundingTime"`
}

type okxFundingHistoryResp struct {
	Code string             `json:"code"`
	Data []okxFundingRecord `json:"data"`
}

func getAllOkxFundingHistory(client *http.Client, instId string, startTime, endTime int64) ([]okxFundingRecord, error) {
	var all []okxFundingRecord
	after := int64(0)

	for {
		url := fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate-history?instId=%s&limit=100", instId)
		if after > 0 {
			url += fmt.Sprintf("&after=%d", after)
		}

		var result okxFundingHistoryResp
		success := false

		for retry := 0; retry < 3; retry++ {
			resp, err := client.Get(url)
			if err == nil {
				errDecode := json.NewDecoder(resp.Body).Decode(&result)
				resp.Body.Close()
				if errDecode == nil && result.Code == "0" {
					success = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !success || len(result.Data) == 0 {
			break
		}

		for _, r := range result.Data {
			ts := int64(utils.ParseFloat(r.FundingTime))
			if ts >= startTime && ts <= endTime {
				all = append(all, r)
			}
		}

		if len(result.Data) < 100 {
			break
		}
		lastRecord := result.Data[len(result.Data)-1]
		after = int64(utils.ParseFloat(lastRecord.FundingTime))

		if after < startTime {
			break
		}
	}
	return all, nil
}

func calcAllOkxCumulative(instId string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour)

	records, err := getAllOkxFundingHistory(client, instId, start.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	for _, r := range records {
		rate := utils.ParseFloat(r.FundingRate)
		ts := int64(utils.ParseFloat(r.FundingTime))
		t := time.UnixMilli(ts)

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
