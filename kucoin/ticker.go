package kucoin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type kuCoinFuturesTicker struct {
	Symbol          string
	BidPrice        float64
	AskPrice        float64
	BidSize         float64
	AskSize         float64
	Timestamp       int64
	MarkPrice       float64
	IndexPrice      float64
	FundingRate     float64
	NextFundingTime int64
	FundingInterval int
}

func fetchKuCoinFuturesTickers(client *http.Client) (map[string]kuCoinFuturesTicker, error) {
	// 1. ПОЧИНКА ФОРМАТА API КУКОИНА
	resp1, err := client.Get("https://api-futures.kucoin.com/api/v1/allTickers")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()

	// Используем интерфейсы для защиты от краша типов GO
	var rawTicks struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&rawTicks); err != nil {
		return nil, err
	}

	type pxSz struct{ Bid, Ask, Bq, Aq float64 }
	tickerMap := make(map[string]pxSz)
	for _, tk := range rawTicks.Data {
		sym := fmt.Sprintf("%v", tk["symbol"])
		if strings.Contains(sym, "USDT") {
			tickerMap[sym] = pxSz{
				Bid: utils.ParseFloat(tk["bestBidPrice"]),
				Ask: utils.ParseFloat(tk["bestAskPrice"]),
				Bq:  utils.ParseFloat(tk["bestBidSize"]),
				Aq:  utils.ParseFloat(tk["bestAskSize"]),
			}
		}
	}

	// 2. ДЕТАЛИ ИНСТРУМЕНТОВ И СТАВОК
	resp2, err := client.Get("https://api-futures.kucoin.com/api/v1/contracts/active")
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	var rawContracts struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rawContracts); err != nil {
		return nil, err
	}

	result := make(map[string]kuCoinFuturesTicker)
	nowTime := time.Now().UnixMilli()

	for _, c := range rawContracts.Data {
		sym := fmt.Sprintf("%v", c["symbol"])
		prices, ok := tickerMap[sym]
		if !ok || prices.Bid <= 0 {
			continue
		}

		intervalHours := int(utils.ParseFloat(c["fundingRateGranularity"]) / 3600000)
		if intervalHours <= 0 {
			intervalHours = 8
		}

		// Кукоин измеряет "1 лот", находим кэф
		contractMultiplier := utils.ParseFloat(c["multiplier"])
		if contractMultiplier <= 0 {
			contractMultiplier = 1.0
		}

		result[sym] = kuCoinFuturesTicker{
			Symbol:   sym,
			BidPrice: prices.Bid,
			AskPrice: prices.Ask,

			// ПРЕОБРАЗОВАНИЕ К НОРМАЛИЗОВАННОЙ ЁМКОСТИ К БАЗОВОМУ АКТИВУ
			BidSize: prices.Bq * contractMultiplier,
			AskSize: prices.Aq * contractMultiplier,

			Timestamp:       nowTime,
			MarkPrice:       utils.ParseFloat(c["markPrice"]),
			IndexPrice:      utils.ParseFloat(c["indexPrice"]),
			FundingRate:     utils.ParseFloat(c["fundingFeeRate"]),
			NextFundingTime: int64(utils.ParseFloat(c["nextFundingRateDateTime"])),
			FundingInterval: intervalHours,
		}
	}
	return result, nil
}
