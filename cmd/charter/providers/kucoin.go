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

type KucoinFetcher struct{}

func (f *KucoinFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	var res []BasicKline
	curr := start

	// В API кукоина индексы данных возвращаются разными (напр. Опен это индекс 1 или 2 и тд). Опеределим динамические счетчики колонок!
	var oIdx, cIdx, hIdx, lIdx = 1, 4, 2, 3
	if market == "spot" {
		oIdx, cIdx, hIdx, lIdx = 1, 2, 3, 4
	}

	for curr < end {
		var url string
		if market == "spot" {
			spotSym := exactSymbol
			if !strings.Contains(spotSym, "-") {
				spotSym = strings.TrimSuffix(spotSym, "USDT") + "-USDT"
			}
			tfMap := map[string]string{"1": "1min", "5": "5min", "15": "15min"}[tf]
			if tfMap == "" {
				tfMap = "5min"
			}
			url = fmt.Sprintf("https://api.kucoin.com/api/v1/market/candles?type=%s&symbol=%s&startAt=%d&endAt=%d", tfMap, spotSym, curr/1000, end/1000)
		} else {
			tfMap := map[string]string{"1": "1", "5": "5", "15": "15"}[tf]
			url = fmt.Sprintf("https://api-futures.kucoin.com/api/v1/kline/query?symbol=%s&granularity=%s&from=%d&to=%d", exactSymbol, tfMap, curr, end)
		}

		var success bool
		var responseData [][]interface{}
		for retry := 0; retry < 5; retry++ {
			resp, err := client.Get(url)
			if err != nil || resp.StatusCode == 429 || resp.StatusCode >= 500 {
				time.Sleep(1 * time.Second)
				continue
			}
			var raw struct {
				Code string          `json:"code"`
				Data [][]interface{} `json:"data"`
			}
			if json.NewDecoder(resp.Body).Decode(&raw) == nil && raw.Code == "200000" {
				responseData = raw.Data
				success = true
				resp.Body.Close()
				break
			}
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
		}

		if !success || len(responseData) == 0 {
			break
		}

		lastTs := curr
		for _, k := range responseData {
			if len(k) > 4 {
				ts := CheckTimestamp(utils.ParseFloat(k[0]))
				res = append(res, BasicKline{
					Timestamp: ts, Open: utils.ParseFloat(k[oIdx]), Close: utils.ParseFloat(k[cIdx]), High: utils.ParseFloat(k[hIdx]), Low: utils.ParseFloat(k[lIdx]),
				})
				if ts > lastTs {
					lastTs = ts
				}
			}
		}
		if lastTs <= curr {
			break
		}
		curr = lastTs + 1
		time.Sleep(100 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
