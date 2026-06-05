package providers

import "net/http"

type BasicKline struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
}

type Fetcher interface {
	Fetch(client *http.Client, symbol string, baseTf string, start, end int64, market string) []BasicKline
}

func GetProvider(exName string) Fetcher {
	switch exName {
	case "binance":
		return &BinanceFetcher{}
	case "bybit":
		return &BybitFetcher{}
	case "mexc":
		return &MexcFetcher{}
	case "okx":
		return &OkxFetcher{}
	case "bitget":
		return &BitgetFetcher{}
	case "kucoin":
		return &KucoinFetcher{}
	case "coinex": // 🔴 Регистрируем CoinEx
		return &CoinexFetcher{}
	case "bingx":
		return &BingxFetcher{} // 🔴 Добавьте вот эту строчку сюда!
	}
	return nil
}

func CheckTimestamp(val float64) int64 {
	if val < 100000000000 {
		return int64(val * 1000)
	}
	return int64(val)
}
