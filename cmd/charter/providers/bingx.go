package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type BingxFetcher struct{}

func (f *BingxFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	// BingX Timeframes: 1m, 5m, 15m
	bxTf := map[string]string{"1": "1m", "5": "5m", "15": "15m"}[tf]
	if bxTf == "" {
		bxTf = "5m"
	}

	sym := exactSymbol
	if !strings.Contains(sym, "-") && strings.HasSuffix(sym, "USDT") {
		sym = strings.TrimSuffix(sym, "USDT") + "-USDT"
	}

	var res []BasicKline
	currEnd := end

	for currEnd > start {
		var url string
		if market == "spot" {
			url = fmt.Sprintf("https://open-api.bingx.com/openApi/spot/v1/market/kline?symbol=%s&interval=%s&limit=1000&startTime=%d&endTime=%d",
				sym, bxTf, start, currEnd)
		} else {
			// Для фьючерсов другой Endpoint
			url = fmt.Sprintf("https://open-api.bingx.com/openApi/swap/v3/quote/klines?symbol=%s&interval=%s&limit=1000&startTime=%d&endTime=%d",
				sym, bxTf, start, currEnd)
		}

		var success bool
		var raw struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}

		for retry := 0; retry < 4; retry++ {
			resp, err := client.Get(url)
			if err == nil {
				// Базовая защита сессии
				if resp.StatusCode == 429 || resp.StatusCode >= 500 {
					resp.Body.Close()
					time.Sleep(2 * time.Second)
					continue
				}

				if json.NewDecoder(resp.Body).Decode(&raw) == nil && raw.Code == 0 {
					success = true
				}
				resp.Body.Close()
				break
			}
			time.Sleep(300 * time.Millisecond)
		}

		if !success {
			break
		}

		oldestTs := currEnd
		itemsAdded := 0

		if market == "spot" {
			// Спотовый API V1 в Bingx возвращает всегда [{time: x, open: y...}], НО мы оставляем страховочный слой под массив (v2)
			var spotDataMap []map[string]interface{}
			var spotDataArr [][]interface{}

			if err := json.Unmarshal(raw.Data, &spotDataMap); err == nil && len(spotDataMap) > 0 {
				for _, k := range spotDataMap {
					if timeVal, ok := k["time"]; ok && timeVal != nil {
						ts := CheckTimestamp(utils.ParseFloat(timeVal))
						if ts < oldestTs {
							oldestTs = ts
						}
						if ts >= start && ts <= end {
							res = append(res, BasicKline{
								Timestamp: ts,
								Open:      utils.ParseFloat(k["open"]),
								Close:     utils.ParseFloat(k["close"]),
								High:      utils.ParseFloat(k["high"]),
								Low:       utils.ParseFloat(k["low"]),
							})
							itemsAdded++
						}
					}
				}
			} else if err := json.Unmarshal(raw.Data, &spotDataArr); err == nil && len(spotDataArr) > 0 {
				for _, k := range spotDataArr {
					if len(k) >= 5 {
						// V2 Arrays order: [timestamp, open, high, low, close]
						ts := CheckTimestamp(utils.ParseFloat(k[0]))
						if ts < oldestTs {
							oldestTs = ts
						}
						if ts >= start && ts <= end {
							res = append(res, BasicKline{
								Timestamp: ts,
								Open:      utils.ParseFloat(k[1]),
								Close:     utils.ParseFloat(k[4]),
								High:      utils.ParseFloat(k[2]),
								Low:       utils.ParseFloat(k[3]),
							})
							itemsAdded++
						}
					}
				}
			}

		} else {
			var futData []struct {
				Time  float64     `json:"time"`
				Open  interface{} `json:"open"`
				Close interface{} `json:"close"`
				High  interface{} `json:"high"`
				Low   interface{} `json:"low"`
			}
			if json.Unmarshal(raw.Data, &futData) == nil {
				for _, k := range futData {
					ts := CheckTimestamp(k.Time)
					if ts < oldestTs {
						oldestTs = ts
					}
					if ts >= start && ts <= end {
						res = append(res, BasicKline{
							Timestamp: ts,
							Open:      utils.ParseFloat(k.Open),
							Close:     utils.ParseFloat(k.Close),
							High:      utils.ParseFloat(k.High),
							Low:       utils.ParseFloat(k.Low),
						})
						itemsAdded++
					}
				}
			}
		}

		if oldestTs >= currEnd || itemsAdded == 0 {
			break
		}
		currEnd = oldestTs - 1
		time.Sleep(20 * time.Millisecond) // Софт таймер, защита Cloudflare Charter MS от теневого бана
	}

	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
