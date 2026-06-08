package engine

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testserver/arbitrage-scanner/models"
	"github.com/testserver/arbitrage-scanner/utils"
)

// InternalAsset используется внутри RAM, связывает сырую строку с точными множителями.
type InternalAsset struct {
	Raw  models.FutureResult
	Base string
	Mult float64
}

// Заглушки, к которым Ядро прикрепляет Конфигурационные файлы
var (
	GlobalBlacklist  = make(map[string]bool)
	TickerCollisions = make(map[string]string)
	MasterDictionary = make(map[string]map[string]struct {
		BaseCoin   string
		Multiplier float64
	})
)

// Универсальное применение алиасов/масок имен коинов
func resolveCollision(exchange, baseCoin string) string {
	if override, exists := TickerCollisions[exchange+":"+baseCoin]; exists {
		return override
	}
	return baseCoin
}

// ==============================================
// 🎯 ЖЕЛЕЗОБЕТОННЫЙ 3D MATCHER СОБЫТИЙ ОЗУ
// ==============================================

func RunInboundSuperMatcher(ctx context.Context, rdb *redis.Client, broadcastCh chan<- []byte) {
	log.Println("⚡ 3D Matcher запущен. Исключены конфликты и перемешивания направлений Спот/Маржа/Фьючи!")

	bookFutures := make(map[string]map[string][]InternalAsset)
	bookSpot := make(map[string]map[string][]InternalAsset)
	bookMargin := make(map[string]map[string][]InternalAsset)

	slowFundingDB := make(map[string]map[string]models.FundingHistoryResult)

	dirtyCoins := make(map[string]struct{})
	finalOppBoard := make(map[string][]models.Opportunity)

	computeTicker := time.NewTicker(350 * time.Millisecond)
	defer computeTicker.Stop()

	for {
		select {
		case event := <-FundingUpdateBus:
			exName := event.ExchangeName
			if slowFundingDB[exName] == nil {
				slowFundingDB[exName] = make(map[string]models.FundingHistoryResult)
			}
			for _, item := range event.FundingData {
				bBase := resolveCollision(exName, item.BaseCoin)
				slowFundingDB[exName][bBase] = item
				dirtyCoins[bBase] = struct{}{}

				// 🟢 DEBUG №1: Проверяем, залетает ли живой фандинг по NEAR в память ОЗУ
				if bBase == "NEAR" && exName == "bitmart" {
					log.Printf("⚙️ [DEBUG 1] Сохранен фандинг BitMart по NEAR: 1d=%f, 3d=%f, 7d=%f", item.Funding1D, item.Funding3D, item.Funding7D)
				}
			}

		case event := <-MarketUpdateBus:
			poolKey := event.ExchangeName + "_" + event.MarketType

			switch event.MarketType {
			case "futures":
				bookFutures[poolKey] = make(map[string][]InternalAsset)
				for _, row := range event.FuturesData {
					base, mult := extractCoinMeta(event.ExchangeName, row.Symbol)
					bookFutures[poolKey][base] = append(bookFutures[poolKey][base], InternalAsset{Raw: row, Base: base, Mult: mult})
					dirtyCoins[base] = struct{}{}
				}
			case "spot":
				bookSpot[poolKey] = make(map[string][]InternalAsset)
				for _, row := range event.SpotData {
					base, mult := extractCoinMeta(event.ExchangeName, row.Symbol)
					frProxy := fabricateSpotPlaceholder(row)
					bookSpot[poolKey][base] = append(bookSpot[poolKey][base], InternalAsset{Raw: frProxy, Base: base, Mult: mult})
					dirtyCoins[base] = struct{}{}
				}
			case "margin":
				bookMargin[poolKey] = make(map[string][]InternalAsset)
				for _, row := range event.MarginData {
					base, mult := extractCoinMeta(event.ExchangeName, row.Symbol)
					frProxy := fabricateMarginPlaceholder(row)
					bookMargin[poolKey][base] = append(bookMargin[poolKey][base], InternalAsset{Raw: frProxy, Base: base, Mult: mult})
					dirtyCoins[base] = struct{}{}
				}
			}

		case <-computeTicker.C:
			if len(dirtyCoins) == 0 {
				continue
			}

			var activeSpot, activeMargin, activeFutures []string
			for k := range bookSpot {
				activeSpot = append(activeSpot, k)
			}
			for k := range bookMargin {
				activeMargin = append(activeMargin, k)
			}
			for k := range bookFutures {
				activeFutures = append(activeFutures, k)
			}

			for coin := range dirtyCoins {
				if GlobalBlacklist[coin] {
					continue
				}

				finalOppBoard[coin] = []models.Opportunity{}
				var resultsForCoin []models.Opportunity

				runDirectionEval := func(exBKey, exSKey string, bAsk, bBid, bMult, sAsk, sBid, sMult, bSz, sSz, bTStmp, sTStmp float64,
					bRaw, sRaw *models.FutureResult) {
					if bAsk <= 0 {
						bAsk = bBid
					}
					if sBid <= 0 {
						sBid = sAsk
					}

					if bAsk > 0 && sBid > 0 {
						tD := bTStmp - sTStmp
						if tD < 0 {
							tD = -tD
						}

						if tD <= 15000 || bTStmp == 0 || sTStmp == 0 {
							spread := (((sBid / sMult) - (bAsk / bMult)) / (bAsk / bMult)) * 100.0

							if spread >= -10.0 && spread <= 25.0 {
								cBaseB := exBKey[:len(exBKey)-len("_"+strings.Split(exBKey, "_")[1])]
								cBaseS := exSKey[:len(exSKey)-len("_"+strings.Split(exSKey, "_")[1])]

								hB := slowFundingDB[cBaseB][coin]
								hS := slowFundingDB[cBaseS][coin]

								// 🟢 DEBUG №2: Смотрим, с какими значениями фандинга собирается связка NEAR
								if coin == "NEAR" && (cBaseB == "bitmart" || cBaseS == "bitmart") {
									log.Printf("⚙️ [DEBUG 2] Сборка связки NEAR: %s (1d=%f) -> %s (1d=%f)", cBaseB, hB.Funding1D, cBaseS, hS.Funding1D)
								}

								opp := buildOppModel(coin, exBKey, exSKey, bRaw, sRaw, spread, hB, hS, safeVolLimit(bAsk, bSz), safeVolLimit(sBid, sSz))
								opp.AskPrice = bAsk
								opp.BidPrice = sBid
								resultsForCoin = append(resultsForCoin, opp)
							}
						}
					}
				}

				// ШАГ #1: СПОТ -> ФЬЮЧЕРСЫ
				for _, bExKey := range activeSpot {
					for _, bToken := range bookSpot[bExKey][coin] {
						for _, sExKey := range activeFutures {
							for _, sToken := range bookFutures[sExKey][coin] {
								runDirectionEval(bExKey, sExKey,
									bToken.Raw.Ask, bToken.Raw.Bid, bToken.Mult,
									sToken.Raw.Ask, sToken.Raw.Bid, sToken.Mult,
									bToken.Raw.AskSize, sToken.Raw.BidSize,
									float64(bToken.Raw.Timestamp), float64(sToken.Raw.Timestamp), &bToken.Raw, &sToken.Raw)
							}
						}
					}
				}

				// ШАГ #2: ФЬЮЧЕРСЫ -> ФЬЮЧЕРСЫ
				for i := 0; i < len(activeFutures); i++ {
					kFutB := activeFutures[i]
					for j := i + 1; j < len(activeFutures); j++ {
						kFutS := activeFutures[j]

						for _, fut1 := range bookFutures[kFutB][coin] {
							for _, fut2 := range bookFutures[kFutS][coin] {
								runDirectionEval(kFutB, kFutS,
									fut1.Raw.Ask, fut1.Raw.Bid, fut1.Mult,
									fut2.Raw.Ask, fut2.Raw.Bid, fut2.Mult,
									fut1.Raw.AskSize, fut2.Raw.BidSize, float64(fut1.Raw.Timestamp), float64(fut2.Raw.Timestamp), &fut1.Raw, &fut2.Raw)

								runDirectionEval(kFutS, kFutB,
									fut2.Raw.Ask, fut2.Raw.Bid, fut2.Mult,
									fut1.Raw.Ask, fut1.Raw.Bid, fut1.Mult,
									fut2.Raw.AskSize, fut1.Raw.BidSize, float64(fut2.Raw.Timestamp), float64(fut1.Raw.Timestamp), &fut2.Raw, &fut1.Raw)
							}
						}
					}
				}

				// ШАГ #3: ФЬЮЧЕРСЫ -> МАРЖА
				for _, bExKey := range activeFutures {
					for _, bToken := range bookFutures[bExKey][coin] {
						for _, sExKey := range activeMargin {
							for _, sToken := range bookMargin[sExKey][coin] {
								runDirectionEval(bExKey, sExKey,
									bToken.Raw.Ask, bToken.Raw.Bid, bToken.Mult,
									sToken.Raw.Ask, sToken.Raw.Bid, sToken.Mult,
									bToken.Raw.AskSize, sToken.Raw.BidSize,
									float64(bToken.Raw.Timestamp), float64(sToken.Raw.Timestamp), &bToken.Raw, &sToken.Raw)
							}
						}
					}
				}

				finalOppBoard[coin] = resultsForCoin
			}

			dirtyCoins = make(map[string]struct{})

			var outboundSlices []models.Opportunity
			for _, oL := range finalOppBoard {
				outboundSlices = append(outboundSlices, oL...)
			}
			if len(outboundSlices) > 0 {
				jPayload, _ := json.Marshal(outboundSlices)
				broadcastCh <- jPayload
			}
		}
	}
}

// ----------------- Хелперы ниже -----------------

func extractCoinMeta(exchange string, sym string) (string, float64) {
	var bc string
	var m float64
	if data, exists := MasterDictionary[exchange][sym]; exists && data.BaseCoin != "" {
		bc, m = data.BaseCoin, data.Multiplier
	} else {
		bc, m = utils.ParseSymbolMeta(sym)
	}
	return resolveCollision(exchange, bc), m
}

func safeVolLimit(price, size float64) float64 {
	vol := price * size
	if vol <= 0 {
		return 999999
	} // Абсолют пустого ордербука Спота в 9999$ Долл Доступа к Арбе (Свежие АС)
	return vol
}

func fabricateSpotPlaceholder(s models.SpotResult) models.FutureResult {
	return models.FutureResult{Exchange: "spot", Symbol: s.Symbol, BaseCoin: s.BaseCoin, Multiplier: s.Multiplier, Bid: s.Bid, Ask: s.Ask, BidSize: s.BidSize, AskSize: s.AskSize, TakerFee: s.TakerFee}
}

func fabricateMarginPlaceholder(m models.MarginResult) models.FutureResult {
	return models.FutureResult{Exchange: "margin", Symbol: m.Symbol, BaseCoin: m.BaseCoin, Multiplier: m.Multiplier, Bid: m.Bid, Ask: m.Ask, BidSize: m.BidSize, AskSize: m.AskSize, TakerFee: m.TakerFee}
}

// Плотное скрепление Выполненной Утилитарной Логики Монеты! Больше ни капли "Крывых Инверсий имен"!
func buildOppModel(c, exBName, exSName string, b, s *models.FutureResult, spread float64, hB, hS models.FundingHistoryResult, buyCap, sellCap float64) models.Opportunity {
	return models.Opportunity{
		Coin:            c,
		BuyExchange:     exBName,
		SellExchange:    exSName,
		BuySymbol:       b.Symbol,
		SellSymbol:      s.Symbol,
		Spread:          spread,
		BuyCapacityUSD:  buyCap,
		SellCapacityUSD: sellCap,
		BuyInterval:     b.Interval,
		SellInterval:    s.Interval,
		BuyCommission:   b.TakerFee,
		SellCommission:  s.TakerFee,
		BuyDeviation:    b.Deviation,
		SellDeviation:   s.Deviation,
		BuyMarkPrice:    b.MarkPrice,
		SellMarkPrice:   s.MarkPrice,
		BuyIndexPrice:   b.IndexPrice,
		SellIndexPrice:  s.IndexPrice,
		FundingRates:    map[string]float64{exBName: b.Funding * 100, exSName: s.Funding * 100},
		AccumulatedBuy:  map[string]float64{"1d": hB.Funding1D, "3d": hB.Funding3D, "7d": hB.Funding7D, "30d": hB.Funding30D, "90d": hB.Funding90D},
		AccumulatedSell: map[string]float64{"1d": hS.Funding1D, "3d": hS.Funding3D, "7d": hS.Funding7D, "30d": hS.Funding30D, "90d": hS.Funding90D},
	}
}
