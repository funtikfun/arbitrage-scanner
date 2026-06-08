package bitmart

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type bitmartFundingRecord struct {
	Symbol      string `json:"symbol"`
	FundingRate string `json:"funding_rate"`
	FundingTime string `json:"funding_time"`
}

type bitmartFundingHistoryResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List []bitmartFundingRecord `json:"list"`
	} `json:"data"`
}

// Получаем 100 честных записей (Максимум биржи)
func getBitMartHistorySnapshot(client *http.Client, symbol string) ([]bitmartFundingRecord, error) {
	url := fmt.Sprintf("https://api-cloud-v2.bitmart.com/contract/public/funding-rate-history?symbol=%s&limit=100", symbol)

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result bitmartFundingHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Code != 1000 {
		return nil, fmt.Errorf("api err")
	}
	return result.Data.List, nil
}

func calcAllBitMartCumulative(symbol string, client *http.Client) (f1d, f3d, f7d, f30d, f90d float64) {
	records, err := getBitMartHistorySnapshot(client, symbol)
	if err != nil || len(records) == 0 {
		return 0, 0, 0, 0, 0
	}

	now := time.Now().UTC()
	var totalInPacket float64
	var oldestTs int64

	for i, r := range records {
		rate := utils.ParseFloat(r.FundingRate)
		ts := int64(utils.ParseFloat(r.FundingTime))
		t := time.UnixMilli(ts)

		if i == len(records)-1 {
			oldestTs = ts
		}

		hours := now.Sub(t).Hours()
		totalInPacket += rate

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
	}

	// 📊 МАТЕМАТИЧЕСКАЯ ПРОЕКЦИЯ (Учет "обрезанного" API)
	// Сколько часов истории нам реально дала биржа (например, 100 записей = 310 часов)
	coverageHours := now.Sub(time.UnixMilli(oldestTs)).Hours()

	if coverageHours > 12 { // Не прогнозируем на пустых данных
		dailyAvg := totalInPacket / (coverageHours / 24.0)

		// Если данных меньше недели — проецируем неделю
		if coverageHours < 168 && f7d == f3d {
			f7d = dailyAvg * 7
		}
		// Проецируем месяц (так как у Anthropic это всего ~4 дня истории)
		if coverageHours < 720 {
			f30d = dailyAvg * 30
		}
		// Проецируем квартал
		f90d = dailyAvg * 90
	}

	return f1d * 100, f3d * 100, f7d * 100, f30d * 100, f90d * 100
}
