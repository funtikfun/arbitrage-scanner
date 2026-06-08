package bitmart

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

const bitmartFuturesTakerFee = 0.06

type bitmartContractDetailsResponse struct {
	Code int `json:"code"`
	Data struct {
		Symbols []struct {
			Symbol               string `json:"symbol"`
			QuoteCurrency        string `json:"quote_currency"`
			LastPrice            string `json:"last_price"`
			IndexPrice           string `json:"index_price"`
			FundingRate          string `json:"funding_rate"`
			FundingIntervalHours int    `json:"funding_interval_hours"`
			Turnover24h          string `json:"turnover_24h"`
			Status               string `json:"status"` // "Trading" или "Delisted"
		} `json:"symbols"`
	} `json:"data"`
}

func FetchFutures() []models.FutureResult {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api-cloud-v2.bitmart.com/contract/public/details")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw bitmartContractDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != 1000 {
		return nil
	}

	var results []models.FutureResult
	nowTime := time.Now().UnixMilli()

	for _, t := range raw.Data.Symbols {
		// 🛡️ ЖЕСТКИЙ ФИЛЬТР: только активные торги + оборот более $1000
		if t.QuoteCurrency != "USDT" || t.Status != "Trading" || utils.ParseFloat(t.Turnover24h) < 1000 {
			continue
		}

		price := utils.ParseFloat(t.LastPrice)
		if price <= 0 {
			continue
		}

		baseName, mult := utils.ParseSymbolMeta(t.Symbol)
		interval := t.FundingIntervalHours
		if interval <= 0 {
			interval = 8
		}

		results = append(results, models.FutureResult{
			Exchange:    "bitmart",
			Symbol:      t.Symbol,
			BaseCoin:    baseName,
			Multiplier:  mult,
			Bid:         price,
			Ask:         price,
			BidSize:     999999.0 / price,
			AskSize:     999999.0 / price,
			Timestamp:   nowTime,
			Funding:     utils.ParseFloat(t.FundingRate),
			NextFunding: nowTime + int64(interval*3600*1000),
			Interval:    interval,
			MarkPrice:   price,
			IndexPrice:  utils.ParseFloat(t.IndexPrice),
			TakerFee:    bitmartFuturesTakerFee,
		})
	}
	return results
}
