package coinex

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type coinexMarketInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		Market       string `json:"market"`
		ContractType string `json:"contract_type"`
		BaseCcy      string `json:"base_ccy"`
		QuoteCcy     string `json:"quote_ccy"`
	} `json:"data"`
}

type coinexTickerAll struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Date   int64                         `json:"date"`
		Ticker map[string]coinexTickerDetail `json:"ticker"`
	} `json:"data"`
}

type coinexTickerDetail struct {
	Buy             string `json:"buy"`
	BuyAmount       string `json:"buy_amount"`
	Sell            string `json:"sell"`
	SellAmount      string `json:"sell_amount"`
	Last            string `json:"last"`
	SignPrice       string `json:"sign_price"` // Это Марк-прайс (Mark Price)
	IndexPrice      string `json:"index_price"`
	FundingRateLast string `json:"funding_rate_last"`
	FundingTime     int    `json:"funding_time"` // Оставшиеся минуты до фандинга
	Period          int    `json:"period"`       // Интервал фандинга в секундах
}

type coinexFuturesTicker struct {
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

func fetchCoinExFuturesTickers(client *http.Client) ([]coinexFuturesTicker, error) {
	// 1. Быстро забираем информацию о линейных фьючерсах (Linear / USDT)
	resp1, err := client.Get("https://api.coinex.com/v2/futures/market")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()

	var marketInfo coinexMarketInfo
	if err := json.NewDecoder(resp1.Body).Decode(&marketInfo); err != nil {
		return nil, err
	}

	// Склад активных линейных рынков (исключаем инверсные контракты)
	linearMarkets := make(map[string]bool)
	for _, m := range marketInfo.Data {
		if m.ContractType == "linear" && m.QuoteCcy == "USDT" {
			linearMarkets[m.Market] = true
		}
	}

	// 2. Забираем тикеры со всей сетки фьючерсов
	resp2, err := client.Get("https://api.coinex.com/perpetual/v1/market/ticker/all")
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	var tickerAll coinexTickerAll
	if err := json.NewDecoder(resp2.Body).Decode(&tickerAll); err != nil {
		return nil, err
	}

	var result []coinexFuturesTicker
	nowMilli := time.Now().UnixMilli()

	for sym, t := range tickerAll.Data.Ticker {
		// Проверяем, что контракт торгуется в USDT и он линейный
		if !linearMarkets[sym] {
			continue
		}

		bid := utils.ParseFloat(t.Buy)
		ask := utils.ParseFloat(t.Sell)
		if bid <= 0 {
			continue // Пустой стакан отсекаем
		}

		intervalHours := t.Period / 3600
		if intervalHours <= 0 {
			intervalHours = 8
		}

		// 🔴 НОРМАЛИЗАЦИЯ ПОД ВАШИ СТАНДАРТЫ (1ч, 4ч, 8ч)
		if intervalHours >= 8 {
			intervalHours = 8
		} else if intervalHours >= 4 {
			intervalHours = 4
		} else {
			intervalHours = 1
		}

		// Высчитываем NextFundingTime: текущее время + оставшиеся минуты до сбора фандинга
		nextFunding := nowMilli + int64(t.FundingTime*60*1000)

		result = append(result, coinexFuturesTicker{
			Symbol:          sym,
			BidPrice:        bid,
			AskPrice:        ask,
			BidSize:         utils.ParseFloat(t.BuyAmount),
			AskSize:         utils.ParseFloat(t.SellAmount),
			Timestamp:       nowMilli,
			MarkPrice:       utils.ParseFloat(t.SignPrice),
			IndexPrice:      utils.ParseFloat(t.IndexPrice),
			FundingRate:     utils.ParseFloat(t.FundingRateLast),
			NextFundingTime: nextFunding,
			FundingInterval: intervalHours,
		})
	}

	return result, nil
}
