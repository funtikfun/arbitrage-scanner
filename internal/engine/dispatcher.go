package engine

import (
	"log"
	"time"

	"github.com/testserver/arbitrage-scanner/internal/exchange"
	"github.com/testserver/arbitrage-scanner/models"
)

// MarketEvent — это универсальная "посылка" от любого нашего независимого агента на конвейер
type MarketEvent struct {
	ExchangeName string
	MarketType   string // "futures", "spot", "margin"
	FuturesData  []models.FutureResult
	SpotData     []models.SpotResult
	MarginData   []models.MarginResult
}

// FundingEvent — отдельная "посылка" с кумулятивными фандингами (т.к. они обновляются реже)
type FundingEvent struct {
	ExchangeName string
	FundingData  []models.FundingHistoryResult
}

// ШИНА ДАННЫХ (Global Channels) — Конвейеры, куда агенты бросают свежие цифры в оперативной памяти (ОЗУ)
var (
	MarketUpdateBus  = make(chan MarketEvent, 200) // Буферизированный канал на 200 потоков цен
	FundingUpdateBus = make(chan FundingEvent, 50) // Канал для истории
)

// DispatchAgents — Метод старта всей корпоративной машины 🚀
// Ядро сканера вызовет этот метод всего один раз, передав массив наших подключенных Бирж
func DispatchAgents(providers []exchange.Provider) {
	log.Printf("🤖 Запуск диспетчера: %d площадок передано в работу.", len(providers))

	for _, p := range providers {
		exName := p.GetName()

		// Запускаем Агентов изолированными пулами (Гортуниами)
		go agentFutures(p, exName)
		go agentSpot(p, exName)
		go agentMargin(p, exName)
		go agentFunding(p, exName)
	}
}

// ⚡ Агент Фьючерсов: долбит площадку и прокидывает цены в шину Ядра без Redis'а!
func agentFutures(p exchange.Provider, name string) {
	for {
		start := time.Now()
		data := p.FetchFutures()

		if len(data) > 0 {
			// Выкидываем посылку на конвейер Матчера:
			MarketUpdateBus <- MarketEvent{
				ExchangeName: name,
				MarketType:   "futures",
				FuturesData:  data,
			}
		}

		sleepToProtectAPI(start, len(data) > 0)
	}
}

// ⚡ Агент Спота
func agentSpot(p exchange.Provider, name string) {
	for {
		start := time.Now()
		data := p.FetchSpot()

		if len(data) > 0 {
			MarketUpdateBus <- MarketEvent{
				ExchangeName: name,
				MarketType:   "spot",
				SpotData:     data,
			}
		}

		sleepToProtectAPI(start, len(data) > 0)
	}
}

// ⚡ Агент Маржи
func agentMargin(p exchange.Provider, name string) {
	for {
		start := time.Now()
		data := p.FetchMargin()

		if len(data) > 0 {
			MarketUpdateBus <- MarketEvent{
				ExchangeName: name,
				MarketType:   "margin",
				MarginData:   data,
			}
		}

		sleepToProtectAPI(start, len(data) > 0)
	}
}

// 🐢 Агент Фандинга (Сбор исторического жира) - медленный, просыпается редко
func agentFunding(p exchange.Provider, name string) {
	time.Sleep(2 * time.Second) // Дадим фьючам загрузиться первыми перед тяжелыми запросами истории

	for {
		start := time.Now()
		data := p.FetchFundingHistory()
		sleepDur := 30 * time.Minute

		if len(data) > 0 {
			FundingUpdateBus <- FundingEvent{
				ExchangeName: name,
				FundingData:  data,
			}
			log.Printf("📥 [%s] База фандингов затянута в шину памяти: %d тикеров.", name, len(data))
		} else {
			sleepDur = 5 * time.Minute // Если ошибка API (бан), повторим не через 30м, а через 5м.
		}

		time.Sleep(sleepDur - time.Since(start))
	}
}

// Вспомогательная утилита динамического таймаута:
// Бережем API бирж и CPU — спим 3 секунды, если была провальная выборка, либо летим дальше через секунду!
func sleepToProtectAPI(startTime time.Time, success bool) {
	elapsed := time.Since(startTime)
	if !success {
		time.Sleep(5 * time.Second)
	} else if wait := (3 * time.Second) - elapsed; wait > 0 {
		time.Sleep(wait)
	} else {
		// Даже при молниеносном получении отдаем минимальную паузу на передышку процессору
		time.Sleep(250 * time.Millisecond)
	}
}
