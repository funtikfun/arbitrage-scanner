package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/testserver/arbitrage-scanner/internal/engine"
	"github.com/testserver/arbitrage-scanner/internal/exchange"
	"github.com/testserver/arbitrage-scanner/internal/proxy"
	"github.com/testserver/arbitrage-scanner/utils"
)

var (
	rdb *redis.Client
	ctx = context.Background()

	// Пулинг SSE клиентов (браузеров)
	clients      = make(map[chan []byte]bool)
	clientsMutex sync.RWMutex

	// Кеширование результатов последнего Тика ОЗУ для GET-клиентов и новых заходов
	lastPayload    []byte
	lastPayloadMut sync.RWMutex
)

func main() {
	log.Println("🚀 СТАРТ ARBITRAGE SCANNER PRO (v2 Enterprise Engine)")

	// 🌐 ВНЕДРЯЕМ АБСОЛЮТНЫЙ ЩИТ ПРОКСИ (Все вызовы HTTP теперь идут через балансировщик)
	// Файл proxies.json должен лежать в internal/proxy/
	smartRouter := proxy.InitSmartTransport("internal/proxy/proxies.json", 3)
	http.DefaultTransport = smartRouter

	// 1. Инициализируем Redis (нужен для совместимости с Charter)
	rdb = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("❌ Ошибка Redis: %v", err)
	}

	// 2. Инициализируем Интерфейсы бирж
	exchange.InitAppExchanges()
	loadedProviders := exchange.GetProviders()
	log.Printf("🔌 Подключены плагины %d бирж к сканеру.", len(loadedProviders))

	// 3. Запускаем Диспетчера автономных потоков (Fast/Slow воркеры)
	engine.DispatchAgents(loadedProviders)

	// 4. Поднимаем Событийный МАТЧЕР (В оперативке), который слушает шину данных
	broadcastCh := make(chan []byte, 100)
	go engine.RunInboundSuperMatcher(ctx, rdb, broadcastCh)

	// Демон раздачи сообщений SSE-клиентам
	go handleBroadcaster(broadcastCh)

	// 5. Запускаем фоновых помощников (Конфиги и Словари)
	go configWatcherDaemon()
	go dictionaryDaemon()

	// 6. Поднимаем Web-Server (Маршруты для UI и API)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "index.html") })
	http.HandleFunc("/api/futures/list", handleApiList)
	http.HandleFunc("/futures/stream", handleSSE)
	http.HandleFunc("/chart", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "chart.html") })
	http.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "dev_tester.html") })

	log.Println("✅ Архитектура Core v2 успешно развернута. Порт :8080 прослушивается.")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

// ==========================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ==========================================

// handleBroadcaster рассылает данные из Матчера всем активным браузерам
func handleBroadcaster(broadcastCh <-chan []byte) {
	for payload := range broadcastCh {
		// Сохраняем для новых подключений
		lastPayloadMut.Lock()
		lastPayload = payload
		lastPayloadMut.Unlock()

		// Рассылаем всем SSE клиентам
		clientsMutex.RLock()
		for ch := range clients {
			select {
			case ch <- payload:
			default:
				// Канал забит, пропускаем тик
			}
		}
		clientsMutex.RUnlock()
	}
}

// handleApiList отдает текущий срез данных из памяти (для первичной загрузки страницы)
func handleApiList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	lastPayloadMut.RLock()
	data := lastPayload
	lastPayloadMut.RUnlock()

	if len(data) == 0 {
		w.Write([]byte(`{"data": []}`))
		return
	}
	w.Write([]byte(`{"data": ` + string(data) + `}`))
}

// handleSSE реализует потоковую передачу данных в браузер (Server-Sent Events)
func handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 2)

	clientsMutex.Lock()
	clients[ch] = true
	clientsMutex.Unlock()

	defer func() {
		clientsMutex.Lock()
		delete(clients, ch)
		close(ch)
		clientsMutex.Unlock()
	}()

	// Сразу отправляем текущее состояние
	lastPayloadMut.RLock()
	initSnap := lastPayload
	lastPayloadMut.RUnlock()

	if len(initSnap) > 0 {
		fmt.Fprintf(w, "event: initial\ndata: %s\n\n", initSnap)
		w.(http.Flusher).Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case bts, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: initial\ndata: %s\n\n", bts)
			w.(http.Flusher).Flush()
		}
	}
}

// configWatcherDaemon следит за файлами конфигурации без перезагрузки сервера
func configWatcherDaemon() {
	for {
		// Читаем коллизии
		if colBytes, err := os.ReadFile("collisions.json"); err == nil && len(colBytes) > 0 {
			var newCol map[string]string
			if json.Unmarshal(colBytes, &newCol) == nil {
				engine.TickerCollisions = newCol
			}
		}

		// Читаем блэклист
		if blBytes, err := os.ReadFile("blacklist.json"); err == nil && len(blBytes) > 0 {
			var newBlArray []string
			if json.Unmarshal(blBytes, &newBlArray) == nil {
				fastMap := make(map[string]bool)
				for _, coin := range newBlArray {
					fastMap[strings.ToUpper(coin)] = true
				}
				engine.GlobalBlacklist = fastMap
			}
		}
		time.Sleep(20 * time.Second)
	}
}

// dictionaryDaemon подтягивает спецификации контрактов (мультипликаторы и базы)
func dictionaryDaemon() {
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("📔 Dictionary Worker Recovered")
				}
			}()

			// Обойма бирж для словаря (Включая BitMart)
			exs := []string{"binance", "bybit", "okx", "bitget", "mexc", "kucoin", "coinex", "bingx", "bitmart"}

			tempDict := make(map[string]map[string]struct {
				BaseCoin   string
				Multiplier float64
			})
			for _, e := range exs {
				tempDict[e] = make(map[string]struct {
					BaseCoin   string
					Multiplier float64
				})
			}

			var wg sync.WaitGroup
			var dmu sync.Mutex

			// --- 1. ByBit Loader ---
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := client.Get("https://api.bybit.com/v5/market/instruments-info?category=linear&limit=1000")
				if err == nil {
					defer r.Body.Close()
					var bd struct {
						Result struct {
							List []struct{ Symbol, BaseCoin string } `json:"list"`
						} `json:"result"`
					}
					if json.NewDecoder(r.Body).Decode(&bd) == nil {
						dmu.Lock()
						for _, i := range bd.Result.List {
							base, mult := utils.ParseSymbolMeta(i.BaseCoin)
							tempDict["bybit"][i.Symbol] = struct {
								BaseCoin   string
								Multiplier float64
							}{base, mult}
						}
						dmu.Unlock()
					}
				}
			}()

			// --- 2. OKX Loader ---
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := client.Get("https://www.okx.com/api/v5/public/instruments?instType=SWAP")
				if err == nil {
					defer r.Body.Close()
					var d struct {
						Data []struct{ InstId, SettleCcy, Uly string } `json:"data"`
					}
					if json.NewDecoder(r.Body).Decode(&d) == nil {
						dmu.Lock()
						for _, sym := range d.Data {
							cleanUly := strings.ReplaceAll(sym.Uly, "-"+sym.SettleCcy, "")
							base, mult := utils.ParseSymbolMeta(cleanUly)
							tempDict["okx"][sym.InstId] = struct {
								BaseCoin   string
								Multiplier float64
							}{base, mult}
						}
						dmu.Unlock()
					}
				}
			}()

			// --- 3. BingX & BitMart Shared Contracts Logic ---
			wg.Add(1)
			go func() {
				defer wg.Done()
				// BingX
				r, err := client.Get("https://open-api.bingx.com/openApi/swap/v2/quote/contracts")
				if err == nil {
					defer r.Body.Close()
					var d struct {
						Data []struct{ Symbol string } `json:"data"`
					}
					if json.NewDecoder(r.Body).Decode(&d) == nil {
						dmu.Lock()
						for _, sym := range d.Data {
							base, mult := utils.ParseSymbolMeta(sym.Symbol)
							cleanSym := strings.ReplaceAll(sym.Symbol, "-", "")
							tempDict["bingx"][cleanSym] = struct {
								BaseCoin   string
								Multiplier float64
							}{base, mult}
						}
						dmu.Unlock()
					}
				}
				// BitMart
				r2, err2 := client.Get("https://api-cloud-v2.bitmart.com/contract/public/details")
				if err2 == nil {
					defer r2.Body.Close()
					var d2 struct {
						Data struct{ Symbols []struct{ Symbol string } } `json:"data"`
					}
					if json.NewDecoder(r2.Body).Decode(&d2) == nil {
						dmu.Lock()
						for _, s := range d2.Data.Symbols {
							base, mult := utils.ParseSymbolMeta(s.Symbol)
							tempDict["bitmart"][s.Symbol] = struct {
								BaseCoin   string
								Multiplier float64
							}{base, mult}
						}
						dmu.Unlock()
					}
				}
			}()

			wg.Wait()
			engine.MasterDictionary = tempDict
			log.Println("📔 Словари мультипликаторов и спецификации контрактов обновлены в ОЗУ.")
		}()
		time.Sleep(3 * time.Hour)
	}
}
