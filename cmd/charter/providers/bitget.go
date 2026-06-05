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

type BitgetFetcher struct{}

func (f *BitgetFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	var res []BasicKline
	currEnd := end
	for currEnd > start {
		var url string
		if market == "spot" {
			val := map[string]string{"1": "1min", "5": "5min", "15": "15min"}[tf]
			spotSym := exactSymbol
			if !strings.HasSuffix(spotSym, "USDT") {
				spotSym += "USDT"
			}
			url = fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/history-candles?symbol=%s&granularity=%s&limit=200&startTime=%d&endTime=%d", spotSym, val, start, currEnd)
		} else {
			val := map[string]string{"1": "1m", "5": "5m", "15": "15m"}[tf]
			url = fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/history-candles?symbol=%s&productType=USDT-FUTURES&granularity=%s&limit=200&startTime=%d&endTime=%d", exactSymbol, val, start, currEnd)
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
			if json.NewDecoder(resp.Body).Decode(&raw) == nil && raw.Code == "00000" {
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

		oldestTs := currEnd
		for _, k := range responseData {
			if len(k) > 4 {
				ts := CheckTimestamp(utils.ParseFloat(k[0]))
				if ts < start {
					continue
				}
				res = append(res, BasicKline{Timestamp: ts, Open: utils.ParseFloat(k[1]), High: utils.ParseFloat(k[2]), Low: utils.ParseFloat(k[3]), Close: utils.ParseFloat(k[4])})
				if ts < oldestTs {
					oldestTs = ts
				}
			}
		}
		if oldestTs >= currEnd {
			break
		}
		currEnd = oldestTs - 1
		time.Sleep(100 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
