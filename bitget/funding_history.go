package bitget

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type bitgetFundingRecord struct {
	FundingRate string `json:"fundingRate"`
	FundingTime string `json:"fundingTime"`
}

func getBitgetFundingHistory(client *http.Client, symbol string, startTime, endTime int64) ([]bitgetFundingRecord, error) {
	const pageSize = 100
	var all []bitgetFundingRecord
	pageNo := 1

	for {
		url := fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/history-fund-rate?symbol=%s&productType=usdt-futures&pageNo=%d&pageSize=%d",
			symbol, pageNo, pageSize)
		resp, err := client.Get(url)
		if err != nil {
			return all, err
		}
		var result struct {
			Code string                `json:"code"`
			Data []bitgetFundingRecord `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return all, err
		}
		resp.Body.Close()

		if result.Code != "00000" || len(result.Data) == 0 {
			break
		}

		for _, r := range result.Data {
			ts := int64(utils.ParseFloat(r.FundingTime))
			if ts >= startTime && ts <= endTime {
				all = append(all, r)
			}
		}

		if len(result.Data) < pageSize || pageNo > 100 {
			break
		}
		pageNo++
	}
	return all, nil
}

func calcAllBitgetCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour)

	records, err := getBitgetFundingHistory(client, symbol, start.UnixMilli(), now.UnixMilli())
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

	return f1d * 100, f3d * 100, f7d * 100, f30d * 100, f90d * 100
}
