package exchange

import (
	"github.com/testserver/arbitrage-scanner/models"

	"github.com/testserver/arbitrage-scanner/binance"
	"github.com/testserver/arbitrage-scanner/bingx"
	"github.com/testserver/arbitrage-scanner/bitget"
	"github.com/testserver/arbitrage-scanner/bitmart"
	"github.com/testserver/arbitrage-scanner/bybit"
	"github.com/testserver/arbitrage-scanner/coinex"
	"github.com/testserver/arbitrage-scanner/kucoin"
	"github.com/testserver/arbitrage-scanner/mexc"
	"github.com/testserver/arbitrage-scanner/okx"
)

// wrapperStruct реализует интерфейс Provider, инкапсулируя функции из старых модулей
type wrapperStruct struct {
	name     string
	fFutures func() []models.FutureResult
	fSpot    func() []models.SpotResult
	fMargin  func() []models.MarginResult
	fFunding func() []models.FundingHistoryResult
}

func (w *wrapperStruct) GetName() string { return w.name }
func (w *wrapperStruct) FetchFutures() []models.FutureResult {
	if w.fFutures != nil {
		return w.fFutures()
	}
	return nil
}
func (w *wrapperStruct) FetchSpot() []models.SpotResult {
	if w.fSpot != nil {
		return w.fSpot()
	}
	return nil
}
func (w *wrapperStruct) FetchMargin() []models.MarginResult {
	if w.fMargin != nil {
		return w.fMargin()
	}
	return nil
}
func (w *wrapperStruct) FetchFundingHistory() []models.FundingHistoryResult {
	if w.fFunding != nil {
		return w.fFunding()
	}
	return nil
}

// nullMargin – заглушка для бирж (как MEXC), которые пока не передают Маржу
func nullMargin() []models.MarginResult { return nil }

// InitAppExchanges вызывает регистрацию старых пакетов в новую Корпоративную Экосистему
func InitAppExchanges() {
	// Binance
	Register(&wrapperStruct{"binance", binance.FetchFutures, binance.FetchSpot, binance.FetchMargin, binance.FetchFundingHistory})
	// ByBit
	Register(&wrapperStruct{"bybit", bybit.FetchFutures, bybit.FetchSpot, bybit.FetchMargin, bybit.FetchFundingHistory})
	// OKX
	Register(&wrapperStruct{"okx", okx.FetchFutures, okx.FetchSpot, okx.FetchMargin, okx.FetchFundingHistory})
	// BitGet
	Register(&wrapperStruct{"bitget", bitget.FetchFutures, bitget.FetchSpot, bitget.FetchMargin, bitget.FetchFundingHistory})
	// MEXC (нет FetchMargin у MEXC, вставляем пустышку nullMargin)
	Register(&wrapperStruct{"mexc", mexc.FetchFutures, mexc.FetchSpot, nullMargin, mexc.FetchFundingHistory})
	// KuCoin
	Register(&wrapperStruct{"kucoin", kucoin.FetchFutures, kucoin.FetchSpot, kucoin.FetchMargin, kucoin.FetchFundingHistory})
	// CoinEx
	Register(&wrapperStruct{"coinex", coinex.FetchFutures, coinex.FetchSpot, coinex.FetchMargin, coinex.FetchFundingHistory})
	// BingX (наша доработанная)
	Register(&wrapperStruct{"bingx", bingx.FetchFutures, bingx.FetchSpot, bingx.FetchMargin, bingx.FetchFundingHistory})
	// BitMart
	Register(&wrapperStruct{"bitmart", bitmart.FetchFutures, bitmart.FetchSpot, nullMargin, bitmart.FetchFundingHistory})
}
