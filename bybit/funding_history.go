package bybit

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type bybitFundingRecord struct {
	Symbol               string `json:"symbol"`
	FundingRate          string `json:"fundingRate"`
	FundingRateTimestamp string `json:"fundingRateTimestamp"`
}

type bybitFundingResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List           []bybitFundingRecord `json:"list"`
		NextPageCursor string               `json:"nextPageCursor"`
	} `json:"result"`
}

func getBybitFundingSegment(client *http.Client, symbol string, startTime, endTime int64) ([]bybitFundingRecord, error) {
	var all []bybitFundingRecord
	cursor := ""

	query := url.Values{}
	query.Set("category", "linear")
	query.Set("symbol", symbol)
	query.Set("startTime", fmt.Sprintf("%d", startTime))
	query.Set("endTime", fmt.Sprintf("%d", endTime))
	query.Set("limit", "200")

	for {
		if cursor != "" {
			query.Del("startTime")
			query.Del("endTime")
			query.Set("cursor", cursor)
		}
		fullURL := "https://api.bybit.com/v5/market/funding/history?" + query.Encode()

		var res bybitFundingResponse
		success := false

		for retry := 0; retry < 3; retry++ {
			resp, err := client.Get(fullURL)
			if err == nil {
				errDecode := json.NewDecoder(resp.Body).Decode(&res)
				resp.Body.Close()
				if errDecode == nil && res.RetCode == 0 {
					success = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !success {
			break // Выходим из цикла пагинации при фатальной ошибке
		}

		all = append(all, res.Result.List...)
		if res.Result.NextPageCursor == "" {
			break
		}
		cursor = res.Result.NextPageCursor
	}
	return all, nil
}

func calcAllBybitCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour)

	const segmentDays = 30
	var allRecords []bybitFundingRecord

	segStart := start
	for segStart.Before(now) {
		segEnd := segStart.Add(segmentDays * 24 * time.Hour)
		if segEnd.After(now) {
			segEnd = now
		}
		records, err := getBybitFundingSegment(client, symbol, segStart.UnixMilli(), segEnd.UnixMilli())
		if err == nil {
			allRecords = append(allRecords, records...)
		}
		segStart = segEnd
	}

	for _, r := range allRecords {
		rate := utils.ParseFloat(r.FundingRate)
		ts := int64(utils.ParseFloat(r.FundingRateTimestamp))
		if ts == 0 {
			continue
		}
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
