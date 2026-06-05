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

	// Пулинг SSE клиентов
	clients      = make(map[chan []byte]bool)
	clientsMutex sync.RWMutex

	// Кеширование результатов последнего Тика ОЗУ для GET-клиентов и API
	lastPayload    []byte
	lastPayloadMut sync.RWMutex
)

func main() {
	log.Println("🚀 СТАРТ ARBITRAGE SCANNER PRO (v2 Enterprise Engine)")

	smartRouter := proxy.InitSmartTransport("internal/proxy/proxies.json", 3)
	http.DefaultTransport = smartRouter

	rdb = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("❌ Ошибка Redis: %v", err)
	}

	exchange.InitAppExchanges()
	providers := exchange.GetProviders()
	log.Printf("🔌 Подключены плагины %d бирж к сканеру.", len(providers))

	engine.DispatchAgents(providers)

	broadcastCh := make(chan []byte, 10)
	go engine.RunInboundSuperMatcher(ctx, rdb, broadcastCh)
	go handleBroadcaster(broadcastCh)

	go configWatcherDaemon()
	go dictionaryDaemon()

	// 💡 ИСЦЕЛЕННАЯ ССЫЛКА НА ДОМЕННУЮ КОРНЕВУЮ АДРЕСАЦИЮ:
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "index.html") })
	http.HandleFunc("/api/futures/list", handleApiList)
	http.HandleFunc("/futures/stream", handleSSE)
	http.HandleFunc("/chart", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "chart.html") })
	http.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "dev_tester.html") })

	log.Println("✅ Архитектура Core v2 успешно развернута. Сервер прослушивает 0.0.0.0:8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// ==========================================
// ВСПОМОГАТЕЛЬНЫЕ Web/Event ФУНКЦИИ ВЕТВЛЕНИЯ
// ==========================================

// Консьерж событий: Рассылает одну находку в Матчере на всех браузеров мира.
func handleBroadcaster(broadcastCh <-chan []byte) {
	for payload := range broadcastCh {
		// Зафиксируем свежий слепок для новых подключающихся REST-клиентов:
		lastPayloadMut.Lock()
		lastPayload = payload
		lastPayloadMut.Unlock()

		clientsMutex.RLock()
		for ch := range clients {
			select {
			case ch <- payload:
			default:
			}
		}
		clientsMutex.RUnlock()
	}
}

// Выдача статической текущей таблицы RAM
func handleApiList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	lastPayloadMut.RLock()
	data := lastPayload
	lastPayloadMut.RUnlock()

	if len(data) == 0 {
		w.Write([]byte(`{"data": []}`))
		return
	}
	w.Write([]byte(`{"data": ` + string(data) + `}`))
}

// Труба бесконечной Event-stream подписки
func handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 1)

	clientsMutex.Lock()
	clients[ch] = true
	clientsMutex.Unlock()

	defer func() {
		clientsMutex.Lock()
		delete(clients, ch)
		close(ch)
		clientsMutex.Unlock()
	}()

	// Моментально скармливаем крайний буфер Матчера
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

// 🛡️ Safe-config Reader: атомарная и безблоковая замена ссылок конфигураций Ядра из JSON
func configWatcherDaemon() {
	for {
		// Никаких жестких линукс путей. Работаем чисто от корня сборки:
		if colBytes, err := os.ReadFile("collisions.json"); err == nil && len(colBytes) > 0 {
			var newCol map[string]string
			if json.Unmarshal(colBytes, &newCol) == nil {
				engine.TickerCollisions = newCol
			}
		}

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
		time.Sleep(15 * time.Second)
	}
}

// Парсер-помощник Биржевых спецификаций лотов контрактов (CtVal, Multipliers, Size)
func dictionaryDaemon() {
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("Dictionary Panicked and Recovered")
				}
			}()

			tempDict := make(map[string]map[string]struct {
				BaseCoin   string
				Multiplier float64
			})
			for _, e := range []string{"binance", "bybit", "okx", "bitget", "mexc", "kucoin", "coinex", "bingx"} {
				tempDict[e] = make(map[string]struct {
					BaseCoin   string
					Multiplier float64
				})
			}

			var wg sync.WaitGroup
			var dmu sync.Mutex

			// --- ByBit (Size Base) ---
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

			// --- OKX (Uly CtVal Base) ---
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

			// --- BingX ---
			wg.Add(1)
			go func() {
				defer wg.Done()
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
			}()

			// Остальные площадки без Size / Default-Lot под капотом парсятся базовой регуляркой из utils

			wg.Wait()
			engine.MasterDictionary = tempDict
			log.Println("📔 Ядро успешно обновило Токены в памяти: Multiplier Data & Contracts Dictionaries")
		}()
		time.Sleep(3 * time.Hour)
	}
}
