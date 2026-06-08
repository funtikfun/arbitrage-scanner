package bitmart

import "github.com/testserver/arbitrage-scanner/models"

func FetchMargin() []models.MarginResult {
	// Изолированная маржа BitMart закрыта жесткими лимитами API,
	// отдаем nil во избежание лишней нагрузки, по аналогии с MEXC
	return nil
}