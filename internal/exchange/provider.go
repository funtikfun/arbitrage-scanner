package exchange

import (
	"sync"

	"github.com/testserver/arbitrage-scanner/models"
)

// Provider описывает единый контракт. Любая новая биржа обязана уметь это делать.
// Если биржа не умеет маржу (Bingx) — она должна возвращать nil, но иметь этот метод.
type Provider interface {
	GetName() string
	FetchFutures() []models.FutureResult
	FetchSpot() []models.SpotResult
	FetchMargin() []models.MarginResult
	FetchFundingHistory() []models.FundingHistoryResult
}

// 🛡 Внутренний склад Ядра (Registry), в котором лежат заряженные плагины
var (
	mu           sync.RWMutex
	providersMap = make(map[string]Provider)
)

// Register подключает плагин в память сканера при загрузке Ядра
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providersMap[p.GetName()] = p
}

// GetProviders отдаёт массив всех загруженных плагинов-бирж на переработку диспетчерам
func GetProviders() []Provider {
	mu.RLock()
	defer mu.RUnlock()

	var list []Provider
	for _, p := range providersMap {
		list = append(list, p)
	}
	return list
}
