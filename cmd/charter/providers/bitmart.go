package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type BitmartFetcher struct{}

func (f *BitmartFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	var res []BasicKline
	// BitMart шаги: 1, 3, 5, 15, 30, 60, 120...
	bmTf := tf

	currStart := start / 1000 // BitMart хочет секунды
	currEnd := end / 1000

	for currStart < currEnd {
		var url string
		if market == "spot" {
			// Spot V3 Klines
			url = fmt.Sprintf("https://api-cloud.bitmart.com/spot/quotation/v3/klines?symbol=%s&step=%s&before=%d&after=%d",
				exactSymbol, bmTf, currEnd, currStart)
		} else {
			// Futures V2 Klines
			url = fmt.Sprintf("https://api-cloud-v2.bitmart.com/contract/public/kline?symbol=%s&step=%s&start_time=%d&end_time=%d",
				exactSymbol, bmTf, currStart, currEnd)
		}

		resp, err := client.Get(url)
		if err != nil {
			break
		}

		var raw struct {
			Code int         `json:"code"`
			Data interface{} `json:"data"` // Может быть массивом или объектом
		}

		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil || raw.Code != 1000 {
			resp.Body.Close()
			break
		}
		resp.Body.Close()

		// Парсинг данных (у BitMart это массив объектов)
		var list []map[string]interface{}
		// Для фьючерсов данные в объекте, для спота могут быть в списке.
		// Используем гибкий конвертер:
		dataBytes, _ := json.Marshal(raw.Data)
		json.Unmarshal(dataBytes, &list)

		if len(list) == 0 {
			break
		}

		lastTs := currStart
		for _, k := range list {
			// Поля: timestamp, open_price, close_price, high_price, low_price
			ts := int64(utils.ParseFloat(k["timestamp"]))
			// Если биржа вернула секунды (10 цифр), переводим в мс (13 цифр)
			if ts < 10000000000 {
				ts *= 1000
			}

			res = append(res, BasicKline{
				Timestamp: ts,
				Open:      utils.ParseFloat(k["open_price"]),
				High:      utils.ParseFloat(k["high_price"]),
				Low:       utils.ParseFloat(k["low_price"]),
				Close:     utils.ParseFloat(k["close_price"]),
			})
			if ts/1000 > lastTs {
				lastTs = ts / 1000
			}
		}

		if lastTs <= currStart {
			break
		}
		currStart = lastTs + 1
		time.Sleep(100 * time.Millisecond) // Защита от лимитов
	}

	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
