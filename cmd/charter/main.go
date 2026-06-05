package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/testserver/arbitrage-scanner/cmd/charter/providers"
	"github.com/testserver/arbitrage-scanner/internal/proxy"
	"github.com/testserver/arbitrage-scanner/utils"

	mexcproto "github.com/MoyuFunding/exchange-pb/go/pkg/mexc"
	"google.golang.org/protobuf/proto"
)

var (
	rdb        *redis.Client
	ctx        = context.Background()
	wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	contractMultiplierCache sync.Map

	wsProxyList   []string
	wsProxyListMu sync.RWMutex
)

// === МОДЕЛИ CHARTER-CORE ===
type SpreadCandle struct {
	Time  string  `json:"t"`
	Open  float64 `json:"o"`
	High  float64 `json:"h"`
	Low   float64 `json:"l"`
	Close float64 `json:"c"`
}
type ClientUpdatePkg struct {
	EnterSpread float64   `json:"enter_spread"`
	ExitSpread  float64   `json:"exit_spread"`
	L_Asks      []OrderBk `json:"l_asks"`
	L_Bids      []OrderBk `json:"l_bids"`
	S_Asks      []OrderBk `json:"s_asks"`
	S_Bids      []OrderBk `json:"s_bids"`
}
type OrderBk struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}
type obManager struct {
	mu       sync.RWMutex
	Bids     map[float64]float64
	Asks     map[float64]float64
	refCount int
	isDead   bool
}

func newOb() *obManager {
	return &obManager{
		Bids: make(map[float64]float64),
		Asks: make(map[float64]float64),
	}
}

var hubL2 = struct {
	sync.Mutex
	dict map[string]*obManager
}{
	dict: make(map[string]*obManager),
}

func loadProxiesForSockets(path string) {
	bytesFile, err := os.ReadFile(path)
	if err == nil {
		var list []string
		if json.Unmarshal(bytesFile, &list) == nil {
			wsProxyListMu.Lock()
			wsProxyList = list
			wsProxyListMu.Unlock()
		}
	}
}

func fetchDynamicSocketDialer() *websocket.Dialer {
	dlr := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}
	wsProxyListMu.RLock()
	total := len(wsProxyList)
	if total > 0 {
		idx := rand.Intn(total)
		rawTarget := wsProxyList[idx]
		if proxyURL, pErr := url.Parse(rawTarget); pErr == nil {
			dlr.Proxy = http.ProxyURL(proxyURL)
		}
	}
	wsProxyListMu.RUnlock()
	return dlr
}

func main() {
	loadProxiesForSockets("internal/proxy/proxies.json")
	smartRouter := proxy.InitSmartTransport("internal/proxy/proxies.json", 3)
	http.DefaultTransport = smartRouter

	rdb = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("❌ Ошибка Redis Chart DB: %v", err)
	}

	http.HandleFunc("/api/chart/spread", corsWrapper(handleChartSpread))
	http.HandleFunc("/api/live/depth", handleMultiplexerLiveClient)
	http.HandleFunc("/api/chart/funding", corsWrapper(handleChartFunding))

	log.Println("📊 MICROSERVICE CHARTER ЗАПУЩЕН V12 PRO (WebSockets Tunelled)")
	http.ListenAndServe(":8081", nil)
}

func corsWrapper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func formatFloat(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func extractPureMarketIdentifierAndSetTf(ex string) (string, string) {
	strV := strings.ToLower(ex)
	pureName := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strV, "_futures", ""), "_spot", ""), "_margin", "")
	targetMarket := "futures"
	if strings.Contains(strV, "_spot") || strings.Contains(strV, "_margin") {
		targetMarket = "spot"
	}
	return pureName, targetMarket
}

func evaluatePriceCorrelation(pA, pB float64) (scale float64, ok bool) {
	if pA <= 0 || pB <= 0 {
		return 1, false
	}
	r := pB / pA
	for _, k := range []float64{0.0001, 0.001, 0.01, 0.1, 1.0, 10.0, 100.0, 1000.0, 10000.0} {
		if r > (k*0.7) && r < (k*1.3) {
			return k, true
		}
	}
	return 1, false
}

func handleChartSpread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()

	buyExName, mB := extractPureMarketIdentifierAndSetTf(q.Get("buyEx"))
	sellExName, mS := extractPureMarketIdentifierAndSetTf(q.Get("sellEx"))
	bSym, sSym := strings.ToUpper(q.Get("bSym")), strings.ToUpper(q.Get("sSym"))

	tfMinutes := utils.ParseFloat(q.Get("tf"))
	if tfMinutes <= 0 {
		tfMinutes = 5
	}

	baseTf := "1"
	if tfMinutes >= 5 && tfMinutes < 15 {
		baseTf = "5"
	} else if tfMinutes >= 15 {
		baseTf = "15"
	}

	cacheK := fmt.Sprintf("chart:hist:v50:%s:%s:%s:%s:%s", q.Get("buyEx"), q.Get("sellEx"), bSym, sSym, q.Get("tf"))
	if cd, err := rdb.Get(ctx, cacheK).Result(); err == nil {
		fmt.Fprint(w, cd)
		return
	}

	now := time.Now().UTC()
	endTime := now.UnixMilli()
	startTime := now.Add(-10 * 24 * time.Hour).UnixMilli()

	cl := &http.Client{Timeout: 15 * time.Second}
	var bLines, sLines []providers.BasicKline
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		if bF := providers.GetProvider(buyExName); bF != nil {
			bLines = bF.Fetch(cl, bSym, baseTf, startTime, endTime, mB)
		}
	}()
	go func() {
		defer wg.Done()
		if sF := providers.GetProvider(sellExName); sF != nil {
			sLines = sF.Fetch(cl, sSym, baseTf, startTime, endTime, mS)
		}
	}()
	wg.Wait()

	if len(bLines) == 0 || len(sLines) == 0 {
		fmt.Fprint(w, "[]")
		return
	}

	bMap := make(map[int64]providers.BasicKline)
	sMap := make(map[int64]providers.BasicKline)
	for _, k := range bLines {
		bMap[k.Timestamp] = k
	}
	for _, k := range sLines {
		sMap[k.Timestamp] = k
	}

	startMax := math.Max(float64(bLines[0].Timestamp), float64(sLines[0].Timestamp))
	endMin := math.Min(float64(bLines[len(bLines)-1].Timestamp), float64(sLines[len(sLines)-1].Timestamp))

	_, bM := utils.ParseSymbolMeta(bSym)
	_, sM := utils.ParseSymbolMeta(sSym)
	scaleAdj := bM / sM

	if alg, iv := evaluatePriceCorrelation(bLines[len(bLines)-1].Close/bM, sLines[len(sLines)-1].Close/sM); iv {
		scaleAdj = scaleAdj / alg
	}

	type BaseSpread struct {
		TimeMs                 int64
		Open, High, Low, Close float64
	}
	var baseSpreads []BaseSpread

	baseTfMs := int64(utils.ParseFloat(baseTf) * 60000)
	syncRoot := int64(startMax) - (int64(startMax) % baseTfMs)
	var fFillB, fFillS *providers.BasicKline

	for st := syncRoot; st <= int64(endMin); st += baseTfMs {
		if kB, ok := bMap[st]; ok {
			fFillB = &kB
		}
		if kS, ok := sMap[st]; ok {
			fFillS = &kS
		}
		if fFillB == nil || fFillS == nil || fFillB.Open <= 0 || fFillS.Open <= 0 {
			continue
		}

		sO := fFillS.Open * scaleAdj
		sC := fFillS.Close * scaleAdj
		sH := fFillS.High * scaleAdj
		sL := fFillS.Low * scaleAdj

		pO := (sO/fFillB.Open - 1) * 100
		pC := (sC/fFillB.Close - 1) * 100
		pH_a := (sH/fFillB.Low - 1) * 100
		pL_a := (sL/fFillB.High - 1) * 100
		pH_s := (sH/fFillB.High - 1) * 100
		pL_s := (sL/fFillB.Low - 1) * 100

		limitUp := math.Max(pO, math.Max(pC, math.Max(pH_s+(pH_a-pH_s)*0.35, pL_s+(pL_a-pL_s)*0.35)))
		limitDn := math.Min(pO, math.Min(pC, math.Min(pH_s+(pH_a-pH_s)*0.35, pL_s+(pL_a-pL_s)*0.35)))
		if limitUp > 45 || limitDn < -45 {
			continue
		}

		baseSpreads = append(baseSpreads, BaseSpread{TimeMs: st, Open: pO, High: limitUp, Low: limitDn, Close: pC})
	}

	var aggr []SpreadCandle
	targetTfMs := int64(tfMinutes * 60000)

	if len(baseSpreads) > 0 {
		baseMap := make(map[int64]BaseSpread)
		for _, b := range baseSpreads {
			baseMap[b.TimeMs] = b
		}

		syncRootAggr := baseSpreads[0].TimeMs - (baseSpreads[0].TimeMs % targetTfMs)
		lastPoint := baseSpreads[len(baseSpreads)-1].TimeMs
		var lCF float64
		drawn := false

		for pt := syncRootAggr; pt <= lastPoint; pt += targetTfMs {
			var ax []BaseSpread

			for walk := pt; walk < pt+targetTfMs; walk += baseTfMs {
				if bc, o := baseMap[walk]; o {
					ax = append(ax, bc)
				}
			}

			if len(ax) == 0 {
				continue
			}

			top := -99999.0
			btm := 99999.0

			// ⚡ ПОЛНОСТЬЮ ЧИСТЫЙ СТРОГИЙ КОД GO ⚡
			for _, cx := range ax {
				if cx.High > top {
					top = cx.High
				}
				if cx.Low < btm {
					btm = cx.Low
				}
			}

			op := ax[0].Open
			cls := ax[len(ax)-1].Close

			if drawn {
				op = lCF
			}

			if top < math.Max(op, cls) {
				top = math.Max(op, cls)
			}
			if btm > math.Min(op, cls) {
				btm = math.Min(op, cls)
			}

			aggr = append(aggr, SpreadCandle{
				Time:  time.UnixMilli(pt).UTC().Format("2006-01-02 15:04:05"),
				Open:  formatFloat(op),
				High:  formatFloat(top),
				Low:   formatFloat(btm),
				Close: formatFloat(cls),
			})

			lCF = cls
			drawn = true
		}
	}

	resJs, _ := json.Marshal(aggr)
	rdb.Set(ctx, cacheK, resJs, 40*time.Second)
	w.Write(resJs)
}

func handleMultiplexerLiveClient(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	q := r.URL.Query()
	bE, mB := extractPureMarketIdentifierAndSetTf(q.Get("buyEx"))
	sE, mS := extractPureMarketIdentifierAndSetTf(q.Get("sellEx"))
	bSym, sSym := strings.ToUpper(q.Get("bSym")), strings.ToUpper(q.Get("sSym"))
	LK, SK := q.Get("buyEx")+":"+bSym, q.Get("sellEx")+":"+sSym

	hubL2.Lock()
	if _, o := hubL2.dict[LK]; !o {
		hubL2.dict[LK] = newOb()
		go wsCoreSpider(bE, mB, bSym, hubL2.dict[LK])
	}
	hubL2.dict[LK].refCount++

	if _, o := hubL2.dict[SK]; !o {
		hubL2.dict[SK] = newOb()
		go wsCoreSpider(sE, mS, sSym, hubL2.dict[SK])
	}
	hubL2.dict[SK].refCount++
	hubL2.Unlock()

	defer func() {
		hubL2.Lock()
		hubL2.dict[LK].refCount--
		if hubL2.dict[LK].refCount <= 0 {
			hubL2.dict[LK].isDead = true
			delete(hubL2.dict, LK)
		}
		hubL2.dict[SK].refCount--
		if hubL2.dict[SK].refCount <= 0 {
			hubL2.dict[SK].isDead = true
			delete(hubL2.dict, SK)
		}
		hubL2.Unlock()
	}()

	tk := time.NewTicker(300 * time.Millisecond)
	defer tk.Stop()

	for {
		<-tk.C
		if hubL2.dict[LK].isDead || hubL2.dict[SK].isDead {
			return
		}

		hubL2.dict[LK].mu.RLock()
		hubL2.dict[SK].mu.RLock()

		la := extrTp(hubL2.dict[LK].Asks, false)
		lb := extrTp(hubL2.dict[LK].Bids, true)
		sa := extrTp(hubL2.dict[SK].Asks, false)
		sb := extrTp(hubL2.dict[SK].Bids, true)

		hubL2.dict[LK].mu.RUnlock()
		hubL2.dict[SK].mu.RUnlock()

		updatePkg := ClientUpdatePkg{
			EnterSpread: 0,
			ExitSpread:  0,
			L_Asks:      la,
			L_Bids:      lb,
			S_Asks:      sa,
			S_Bids:      sb,
		}

		if conn.WriteJSON(updatePkg) != nil {
			return
		}
	}
}

func extrTp(m map[float64]float64, reverseSort bool) []OrderBk {
	var pr []float64
	for k := range m {
		pr = append(pr, k)
	}

	if reverseSort {
		sort.Sort(sort.Reverse(sort.Float64Slice(pr)))
	} else {
		sort.Float64s(pr)
	}

	var rx []OrderBk
	t := len(pr)
	if t > 7 {
		t = 7
	}

	for i := 0; i < t; i++ {
		rx = append(rx, OrderBk{Price: pr[i], Qty: m[pr[i]]})
	}
	return rx
}

func getKucoinWsUrl(cl *http.Client, mrk string) (string, error) {
	ur := "https://api-futures.kucoin.com/api/v1/bullet-public"
	if mrk == "spot" {
		ur = "https://api.kucoin.com/api/v1/bullet-public"
	}
	resp, e := cl.Post(ur, "application/json", nil)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()

	var raw struct {
		Data struct {
			Token string `json:"token"`
			IS    []struct {
				End string `json:"endpoint"`
			} `json:"instanceServers"`
		} `json:"data"`
	}

	if json.NewDecoder(resp.Body).Decode(&raw) != nil || len(raw.Data.IS) == 0 {
		return "", fmt.Errorf("bad token")
	}

	return fmt.Sprintf("%s?token=%s", raw.Data.IS[0].End, raw.Data.Token), nil
}

func getLotSizeScale(ex, mk, sm string) float64 {
	if mk == "spot" {
		return 1.0
	}
	mkY := fmt.Sprintf("%s:%s", ex, sm)
	if val, ok := contractMultiplierCache.Load(mkY); ok {
		return val.(float64)
	}

	cl := &http.Client{Timeout: 8 * time.Second}
	vl := 1.0

	switch ex {
	case "mexc":
		rp, er := cl.Get("https://contract.mexc.com/api/v1/contract/detail")
		if er == nil {
			defer rp.Body.Close()
			var raw struct {
				Data []map[string]interface{} `json:"data"`
			}
			if json.NewDecoder(rp.Body).Decode(&raw) == nil {
				tt := strings.ToUpper(sm)
				if !strings.Contains(tt, "_") {
					tt = strings.TrimSuffix(tt, "USDT") + "_USDT"
				}
				for _, r := range raw.Data {
					if fmt.Sprintf("%v", r["symbol"]) == tt {
						if sv := utils.ParseFloat(r["contractSize"]); sv > 0 {
							vl = sv
							break
						}
					}
				}
			}
		}
	case "bingx":
		rp, er := cl.Get("https://open-api.bingx.com/openApi/swap/v2/quote/contracts")
		if er == nil {
			defer rp.Body.Close()
			var raw struct {
				Data []struct {
					Symbol string
					Size   interface{}
				} `json:"data"`
			}
			if json.NewDecoder(rp.Body).Decode(&raw) == nil {
				for _, r := range raw.Data {
					if strings.ReplaceAll(r.Symbol, "-", "") == strings.ToUpper(sm) {
						if sv := utils.ParseFloat(r.Size); sv > 0 {
							vl = sv
							break
						}
					}
				}
			}
		}
	}

	contractMultiplierCache.Store(mkY, vl)
	return vl
}

func clM(m map[float64]float64) {
	for k := range m {
		delete(m, k)
	}
}

// wsCoreSpider подключается к WebSocket биржи через пул прокси-адресов
func wsCoreSpider(ex, mrkt, sym string, memory *obManager) {
	upS := strings.ToUpper(sym)
	lwS := strings.ToLower(sym)
	volScale := getLotSizeScale(ex, mrkt, upS)

	for !memory.isDead {
		var wsP, inOp string
		switch ex {
		case "binance":
			wsP = "wss://stream.binance.com:9443/stream?streams=" + lwS + "@depth5@100ms"
			if mrkt != "spot" {
				wsP = "wss://fstream.binance.com/stream?streams=" + lwS + "@depth5@100ms"
			}
		case "bybit":
			ct := "linear"
			if mrkt == "spot" {
				ct = "spot"
			}
			wsP = "wss://stream.bybit.com/v5/public/" + ct
			inOp = `{"op":"subscribe","args":["orderbook.50.` + upS + `"]}`
		case "okx":
			wsP = "wss://ws.okx.com:8443/ws/v5/public"
			tg := strings.ReplaceAll(upS, "USDT", "-USDT-SWAP")
			if mrkt == "spot" {
				tg = strings.ReplaceAll(upS, "USDT", "-USDT")
			}
			inOp = `{"op":"subscribe","args":[{"channel":"books5","instId":"` + tg + `"}]}`
		case "bitget":
			wsP = "wss://ws.bitget.com/v2/ws/public"
			iT := "USDT-FUTURES"
			if mrkt == "spot" {
				iT = "SPOT"
			}
			inOp = `{"op":"subscribe","args":[{"instType":"` + iT + `","channel":"books15","instId":"` + upS + `"}]}`
		case "mexc":
			wsP = "wss://contract.mexc.com/edge"
			inOp = `{"method":"sub.depth","param":{"symbol":"` + strings.TrimSuffix(upS, "USDT") + "_USDT" + `"}}`
			if mrkt == "spot" {
				wsP = "wss://wbs-api.mexc.com/ws"
				inOp = `{"method":"SUBSCRIPTION","params":["spot@public.limit.depth.v3.api.pb@` + upS + `@5"]}`
			}
		case "kucoin":
			ph, e := getKucoinWsUrl(&http.Client{Timeout: 8 * time.Second}, mrkt)
			if e != nil {
				time.Sleep(3 * time.Second)
				continue
			}
			wsP = ph
			inOp = `{"id":1545910660740,"type":"subscribe","topic":"/contractMarket/level2Depth5:` + upS + `","response":true}`
			if mrkt == "spot" {
				mdS := upS
				if !strings.Contains(mdS, "-") {
					mdS = strings.TrimSuffix(mdS, "USDT") + "-USDT"
				}
				inOp = `{"id":1545910660740,"type":"subscribe","topic":"/spotMarket/level2Depth5:` + mdS + `","response":true}`
			}
		case "coinex":
			wsP = "wss://perpetual.coinex.com/"
			if mrkt == "spot" {
				wsP = "wss://socket.coinex.com/"
			}
			inOp = `{"id":15,"method":"depth.subscribe","params":["` + upS + `",5,"0",true]}`
		case "bingx":
			bSy := upS
			if len(upS) > 4 && strings.HasSuffix(upS, "USDT") {
				bSy = strings.TrimSuffix(upS, "USDT") + "-USDT"
			}
			wsP = "wss://open-api-swap.bingx.com/swap-market"
			if mrkt == "spot" {
				wsP = "wss://open-api-ws.bingx.com/market"
			}
			inOp = `{"id":"1","reqType":"sub","dataType":"` + bSy + `@depth20"}`
		default:
			return
		}

		socketRouterEngine := fetchDynamicSocketDialer()
		conn, _, e := socketRouterEngine.Dial(wsP, nil)
		if e != nil {
			time.Sleep(4 * time.Second)
			continue
		}

		if inOp != "" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(inOp))
		}

		pingBreak := make(chan struct{})
		go func(c *websocket.Conn, q chan struct{}) {
			tr := time.NewTicker(15 * time.Second)
			defer tr.Stop()
			for {
				select {
				case <-q:
					return
				case <-tr.C:
					if memory.isDead {
						return
					}
					var px []byte
					switch ex {
					case "bybit":
						px = []byte(`{"op":"ping"}`)
					case "okx", "bitget":
						px = []byte("ping")
					case "kucoin":
						px = []byte(`{"id":"1545910660740","type":"ping"}`)
					case "mexc":
						px = []byte(`{"method":"ping"}`)
						if mrkt == "spot" {
							px = []byte(`{"method":"PING"}`)
						}
					case "coinex":
						px = []byte(`{"id":1000,"method":"server.ping","params":[]}`)
					}
					if len(px) > 0 {
						c.SetWriteDeadline(time.Now().Add(5 * time.Second))
						_ = c.WriteMessage(websocket.TextMessage, px)
					}
				}
			}
		}(conn, pingBreak)

		for {
			if memory.isDead {
				break
			}
			_, btR, hEr := conn.ReadMessage()
			if hEr != nil {
				break
			}

			var mT []byte
			if len(btR) > 2 && btR[0] == 0x1f && btR[1] == 0x8b {
				rZ, erZ := gzip.NewReader(bytes.NewReader(btR))
				if erZ == nil {
					deC, ecZ := io.ReadAll(rZ)
					if ecZ == nil {
						mT = deC
					}
					rZ.Close()
				}
			} else {
				mT = btR
			}

			if mT == nil {
				continue
			}

			memory.mu.Lock()

			switch ex {
			case "bingx":
				sb := string(mT)
				if sb == "Ping" || sb == "ping" {
					_ = conn.WriteMessage(websocket.TextMessage, []byte("Pong"))
					memory.mu.Unlock()
					continue
				}

				var rawX struct {
					DT string `json:"dataType"`
					DD struct {
						Asks [][]interface{} `json:"asks"`
						Bids [][]interface{} `json:"bids"`
					} `json:"data"`
				}

				if json.Unmarshal(mT, &rawX) == nil && strings.Contains(rawX.DT, "depth") {
					clM(memory.Asks)
					clM(memory.Bids)

					for _, u := range rawX.DD.Asks {
						if len(u) >= 2 {
							price := utils.ParseFloat(u[0])
							qty := utils.ParseFloat(u[1]) * volScale
							memory.Asks[price] = qty
						}
					}
					for _, u := range rawX.DD.Bids {
						if len(u) >= 2 {
							price := utils.ParseFloat(u[0])
							qty := utils.ParseFloat(u[1]) * volScale
							memory.Bids[price] = qty
						}
					}
				}

			case "binance":
				var bX struct {
					D struct {
						A    [][]interface{} `json:"a"`
						B    [][]interface{} `json:"b"`
						Asks [][]interface{} `json:"asks"`
						Bids [][]interface{} `json:"bids"`
					} `json:"data"`
				}
				if json.Unmarshal(mT, &bX) == nil {
					xa := bX.D.A
					xb := bX.D.B
					if len(bX.D.Asks) > 0 {
						xa = bX.D.Asks
					}
					if len(bX.D.Bids) > 0 {
						xb = bX.D.Bids
					}

					if len(xa) > 0 || len(xb) > 0 {
						clM(memory.Asks)
						clM(memory.Bids)
						for _, e := range xa {
							memory.Asks[utils.ParseFloat(e[0])] = utils.ParseFloat(e[1]) * volScale
						}
						for _, e := range xb {
							memory.Bids[utils.ParseFloat(e[0])] = utils.ParseFloat(e[1]) * volScale
						}
					}
				}

			case "okx", "bitget":
				var dx struct {
					Action string `json:"action"`
					D      []struct {
						A [][]string `json:"asks"`
						B [][]string `json:"bids"`
					} `json:"data"`
				}
				if json.Unmarshal(mT, &dx) == nil && len(dx.D) > 0 {
					if dx.Action == "snapshot" || ex == "okx" {
						clM(memory.Asks)
						clM(memory.Bids)
					}
					for _, u := range dx.D[0].A {
						p := utils.ParseFloat(u[0])
						q := utils.ParseFloat(u[1])
						if q <= 0 {
							delete(memory.Asks, p)
						} else {
							memory.Asks[p] = q * volScale
						}
					}
					for _, u := range dx.D[0].B {
						p := utils.ParseFloat(u[0])
						q := utils.ParseFloat(u[1])
						if q <= 0 {
							delete(memory.Bids, p)
						} else {
							memory.Bids[p] = q * volScale
						}
					}
				}

			case "bybit":
				var yy struct {
					Typ string `json:"type"`
					D   struct {
						A [][]string `json:"a"`
						B [][]string `json:"b"`
					} `json:"data"`
				}
				if json.Unmarshal(mT, &yy) == nil {
					if yy.Typ == "snapshot" {
						clM(memory.Asks)
						clM(memory.Bids)
					}
					for _, u := range yy.D.A {
						p := utils.ParseFloat(u[0])
						q := utils.ParseFloat(u[1])
						if q <= 0 {
							delete(memory.Asks, p)
						} else {
							memory.Asks[p] = q * volScale
						}
					}
					for _, u := range yy.D.B {
						p := utils.ParseFloat(u[0])
						q := utils.ParseFloat(u[1])
						if q <= 0 {
							delete(memory.Bids, p)
						} else {
							memory.Bids[p] = q * volScale
						}
					}
				}

			case "kucoin", "coinex", "mexc":
				if ex == "kucoin" {
					var kv struct {
						T string `json:"type"`
						D struct {
							A [][]interface{} `json:"asks"`
							B [][]interface{} `json:"bids"`
						} `json:"data"`
					}
					if json.Unmarshal(mT, &kv) == nil && kv.T == "message" {
						clM(memory.Asks)
						clM(memory.Bids)
						for _, e := range kv.D.A {
							memory.Asks[utils.ParseFloat(e[0])] = utils.ParseFloat(e[1]) * volScale
						}
						for _, e := range kv.D.B {
							memory.Bids[utils.ParseFloat(e[0])] = utils.ParseFloat(e[1]) * volScale
						}
					}
				}
				if ex == "coinex" {
					var cf struct {
						Method string        `json:"method"`
						Params []interface{} `json:"params"`
					}
					if json.Unmarshal(mT, &cf) == nil && cf.Method == "depth.update" && len(cf.Params) >= 2 {
						clA, _ := cf.Params[0].(bool)
						if clA {
							clM(memory.Asks)
							clM(memory.Bids)
						}
						if ptMap, mX := cf.Params[1].(map[string]interface{}); mX {
							if askL, qR := ptMap["asks"].([]interface{}); qR {
								for _, rt := range askL {
									rw, oW := rt.([]interface{})
									if oW && len(rw) >= 2 {
										pr := utils.ParseFloat(rw[0])
										qty := utils.ParseFloat(rw[1])
										if qty <= 0 {
											delete(memory.Asks, pr)
										} else {
											memory.Asks[pr] = qty * volScale
										}
									}
								}
							}
							if bidL, qB := ptMap["bids"].([]interface{}); qB {
								for _, rt := range bidL {
									rw, oW := rt.([]interface{})
									if oW && len(rw) >= 2 {
										pr := utils.ParseFloat(rw[0])
										qty := utils.ParseFloat(rw[1])
										if qty <= 0 {
											delete(memory.Bids, pr)
										} else {
											memory.Bids[pr] = qty * volScale
										}
									}
								}
							}
						}
					}
				}
				if ex == "mexc" {
					if mrkt == "spot" {
						pbObj := &mexcproto.PushDataV3ApiWrapper{}
						if proto.Unmarshal(mT, pbObj) == nil {
							dz := pbObj.GetPublicLimitDepths()
							if dz != nil {
								clM(memory.Asks)
								clM(memory.Bids)
								for _, g := range dz.GetAsks() {
									memory.Asks[utils.ParseFloat(g.GetPrice())] = utils.ParseFloat(g.GetQuantity()) * volScale
								}
								for _, g := range dz.GetBids() {
									memory.Bids[utils.ParseFloat(g.GetPrice())] = utils.ParseFloat(g.GetQuantity()) * volScale
								}
							}
						}
					} else {
						var mt struct {
							Cnl string `json:"channel"`
							Dat struct {
								A [][]interface{} `json:"asks"`
								B [][]interface{} `json:"bids"`
							} `json:"data"`
						}
						if json.Unmarshal(mT, &mt) == nil && strings.Contains(mt.Cnl, "depth") {
							for _, g := range mt.Dat.A {
								px := utils.ParseFloat(g[0])
								qty := utils.ParseFloat(g[1])
								if qty <= 0 {
									delete(memory.Asks, px)
								} else {
									memory.Asks[px] = qty * volScale
								}
							}
							for _, g := range mt.Dat.B {
								px := utils.ParseFloat(g[0])
								qty := utils.ParseFloat(g[1])
								if qty <= 0 {
									delete(memory.Bids, px)
								} else {
									memory.Bids[px] = qty * volScale
								}
							}
						}
					}
				}
			}
			memory.mu.Unlock()
		}

		close(pingBreak)
		_ = conn.Close()
		time.Sleep(3 * time.Second)
	}
}

func handleChartFunding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ex := strings.ToLower(r.URL.Query().Get("ex"))
	ex = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(ex, "_futures", ""), "_spot", ""), "_margin", "")
	sym := strings.ToUpper(r.URL.Query().Get("sym"))

	cacheKey := fmt.Sprintf("funding:hist:v9:%s:%s", ex, sym)
	if cd, err := rdb.Get(ctx, cacheKey).Result(); err == nil {
		fmt.Fprint(w, cd)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	type FundResp struct {
		Timestamp int64   `json:"timestamp"`
		Rate      float64 `json:"rate"`
	}
	var results []FundResp

	stLim := time.Now().Add(-30 * time.Hour * 24).UnixMilli()
	nMs := time.Now().UnixMilli()

	switch ex {
	case "binance":
		rp, er := client.Get(fmt.Sprintf("https://fapi.binance.com/fapi/v1/fundingRate?symbol=%s&limit=1000", sym))
		if er == nil {
			var rs []map[string]interface{}
			if json.NewDecoder(rp.Body).Decode(&rs) == nil {
				for _, u := range rs {
					results = append(results, FundResp{
						Timestamp: int64(utils.ParseFloat(u["fundingTime"])),
						Rate:      utils.ParseFloat(u["fundingRate"]),
					})
				}
			}
			rp.Body.Close()
		}
	case "bybit":
		cur := ""
		for {
			rp, er := client.Get(fmt.Sprintf("https://api.bybit.com/v5/market/funding/history?category=linear&symbol=%s&limit=200&cursor=%s", sym, cur))
			if er != nil {
				break
			}

			var mF struct {
				RtC int `json:"retCode"`
				Re  struct {
					Lst  []map[string]interface{} `json:"list"`
					NexC string                   `json:"nextPageCursor"`
				} `json:"result"`
			}

			if json.NewDecoder(rp.Body).Decode(&mF) != nil || mF.RtC != 0 || len(mF.Re.Lst) == 0 {
				rp.Body.Close()
				break
			}

			bO := false
			for _, mK := range mF.Re.Lst {
				tk := int64(utils.ParseFloat(mK["fundingRateTimestamp"]))
				if tk < stLim {
					bO = true
				}
				results = append(results, FundResp{Timestamp: tk, Rate: utils.ParseFloat(mK["fundingRate"])})
			}
			rp.Body.Close()

			if bO || mF.Re.NexC == "" || len(results) >= 1000 {
				break
			}
			cur = mF.Re.NexC
			time.Sleep(15 * time.Millisecond)
		}
	case "bingx":
		bS := strings.TrimSuffix(sym, "USDT") + "-USDT"
		cuMs := int64(0)
		fkA := 0

		for pg := 0; pg < 15; pg++ {
			urL := fmt.Sprintf("https://open-api.bingx.com/openApi/swap/v2/quote/fundingRate?symbol=%s&limit=100", bS)
			if cuMs > 0 {
				urL = fmt.Sprintf("%s&endTime=%d", urL, cuMs)
			}

			rrS, ecR := client.Get(urL)
			if ecR != nil {
				break
			}
			if rrS.StatusCode == 429 {
				rrS.Body.Close()
				time.Sleep(3 * time.Second)
				continue
			}

			var raW struct {
				Cd int `json:"code"`
				Dx []struct {
					Tm int64       `json:"fundingTime"`
					Rt interface{} `json:"fundingRate"`
				} `json:"data"`
			}

			if json.NewDecoder(rrS.Body).Decode(&raW) != nil || raW.Cd != 0 || len(raW.Dx) == 0 {
				rrS.Body.Close()
				break
			}
			rrS.Body.Close()

			oeZ := int64(0)
			reN := false
			for _, vz := range raW.Dx {
				ttX := vz.Tm
				if ttX < stLim {
					reN = true
				}
				results = append(results, FundResp{Timestamp: ttX, Rate: utils.ParseFloat(vz.Rt)})
				fkA++
				if oeZ == 0 || ttX < oeZ {
					oeZ = ttX
				}
			}

			if reN || oeZ <= stLim || len(raW.Dx) < 2 || fkA >= 1200 {
				break
			}
			cuMs = oeZ - 1
			time.Sleep(20 * time.Millisecond)
		}
	case "mexc":
		cSz := strings.TrimSuffix(sym, "USDT") + "_USDT"
		pZ := 1
		for {
			rpX, ehC := client.Get(fmt.Sprintf("https://contract.mexc.com/api/v1/contract/funding_rate/history?symbol=%s&page_size=100&page_num=%d", cSz, pZ))
			if ehC != nil {
				break
			}

			var mBf struct {
				Sc bool `json:"success"`
				Ds struct {
					Rst []map[string]interface{} `json:"resultList"`
					Tp  int                      `json:"totalPage"`
				} `json:"data"`
			}

			if json.NewDecoder(rpX.Body).Decode(&mBf) != nil || !mBf.Sc || len(mBf.Ds.Rst) == 0 {
				rpX.Body.Close()
				break
			}
			rpX.Body.Close()

			ykE := false
			for _, yI := range mBf.Ds.Rst {
				vTc := int64(utils.ParseFloat(yI["settleTime"]))
				if vTc < stLim {
					ykE = true
				}
				results = append(results, FundResp{Timestamp: vTc, Rate: utils.ParseFloat(yI["fundingRate"])})
			}
			if ykE || pZ >= mBf.Ds.Tp || len(results) >= 1000 {
				break
			}
			pZ++
			time.Sleep(20 * time.Millisecond)
		}
	case "okx":
		cMz := strings.TrimSuffix(sym, "USDT") + "-USDT-SWAP"
		nC := ""
		for {
			uT := fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate-history?instId=%s&limit=100", cMz)
			if nC != "" {
				uT += "&after=" + nC
			}

			gRt, yC := client.Get(uT)
			if yC != nil {
				break
			}

			var qK struct {
				Cc string                   `json:"code"`
				Qs []map[string]interface{} `json:"data"`
			}
			if json.NewDecoder(gRt.Body).Decode(&qK) != nil || qK.Cc != "0" || len(qK.Qs) == 0 {
				gRt.Body.Close()
				break
			}
			gRt.Body.Close()

			uEo := false
			mkS := int64(0)
			for _, mxA := range qK.Qs {
				tR := int64(utils.ParseFloat(mxA["fundingTime"]))
				if tR < stLim {
					uEo = true
				}
				results = append(results, FundResp{Timestamp: tR, Rate: utils.ParseFloat(mxA["fundingRate"])})
				if mkS == 0 || tR < mkS {
					mkS = tR
				}
			}
			if uEo || mkS == 0 || len(results) >= 1000 {
				break
			}
			nC = fmt.Sprintf("%d", mkS)
			time.Sleep(30 * time.Millisecond)
		}
	case "bitget":
		pK := 1
		for {
			rBx, fGh := client.Get(fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/history-fund-rate?symbol=%s&productType=usdt-futures&pageNo=%d&pageSize=100", sym, pK))
			if fGh != nil {
				break
			}

			var sHl struct {
				Cc string                   `json:"code"`
				Sd []map[string]interface{} `json:"data"`
			}
			if json.NewDecoder(rBx.Body).Decode(&sHl) != nil || sHl.Cc != "00000" || len(sHl.Sd) == 0 {
				rBx.Body.Close()
				break
			}
			rBx.Body.Close()

			trV := false
			for _, dfX := range sHl.Sd {
				ttR := int64(utils.ParseFloat(dfX["fundingTime"]))
				if ttR < stLim {
					trV = true
				}
				results = append(results, FundResp{Timestamp: ttR, Rate: utils.ParseFloat(dfX["fundingRate"])})
			}
			if trV || len(results) >= 1000 {
				break
			}
			pK++
			time.Sleep(20 * time.Millisecond)
		}
	case "kucoin":
		cG := stLim
		for cG < nMs {
			cwR := cG + (10 * 24 * 3600 * 1000)
			if cwR > nMs {
				cwR = nMs
			}

			bCx := false
			var dfM []map[string]interface{}

			for xT := 0; xT < 5; xT++ {
				rgF, evR := client.Get(fmt.Sprintf("https://api-futures.kucoin.com/api/v1/contract/funding-rates?symbol=%s&from=%d&to=%d", sym, cG, cwR))
				if evR == nil {
					if rgF.StatusCode == 429 || rgF.StatusCode >= 500 {
						rgF.Body.Close()
						time.Sleep(2 * time.Second)
						continue
					}
					var mkN struct {
						Cv string                   `json:"code"`
						Yy []map[string]interface{} `json:"data"`
					}
					egP := json.NewDecoder(rgF.Body).Decode(&mkN)
					rgF.Body.Close()

					if egP == nil && mkN.Cv == "200000" {
						dfM = mkN.Yy
						bCx = true
						break
					}
				}
				time.Sleep(400 * time.Millisecond)
			}

			if bCx && len(dfM) > 0 {
				for _, dS := range dfM {
					results = append(results, FundResp{
						Timestamp: int64(utils.ParseFloat(dS["timepoint"])),
						Rate:      utils.ParseFloat(dS["fundingRate"]),
					})
				}
			}
			cG = cwR + 1
			time.Sleep(100 * time.Millisecond)
		}
	case "coinex":
		pxB := 1
		for {
			oBk, pbO := client.Get(fmt.Sprintf("https://api.coinex.com/v2/futures/funding-rate-history?market=%s&limit=100&page=%d", sym, pxB))
			if pbO != nil {
				break
			}

			var cxE struct {
				CfA int `json:"code"`
				DhO []struct {
					Yt  int64  `json:"funding_time"`
					RcR string `json:"actual_funding_rate"`
				} `json:"data"`
			}

			if json.NewDecoder(oBk.Body).Decode(&cxE) != nil || cxE.CfA != 0 || len(cxE.DhO) == 0 {
				oBk.Body.Close()
				break
			}
			oBk.Body.Close()

			eEa := false
			for _, zI := range cxE.DhO {
				tcU := zI.Yt
				if tcU < stLim {
					eEa = true
				}
				results = append(results, FundResp{Timestamp: tcU, Rate: utils.ParseFloat(zI.RcR)})
			}

			if eEa || len(results) >= 1000 {
				break
			}
			pxB++
			time.Sleep(20 * time.Millisecond)
		}
	}

	if results == nil {
		results = []FundResp{}
	}

	bG, _ := json.Marshal(results)
	rdb.Set(ctx, cacheKey, bG, 2*time.Hour)
	w.Write(bG)
}
