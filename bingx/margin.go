package bingx

import "github.com/testserver/arbitrage-scanner/models"

func FetchMargin() []models.MarginResult {
	// BingX изолированно не передаёт маржу спота в общедоступных анонимных рыночных API-ответах.
	// Отдаём nil, чтобы Сканер сэкономил CPU.
	return nil
}
