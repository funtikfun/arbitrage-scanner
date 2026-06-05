package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type BinanceFetcher struct{}

func (f *BinanceFetcher) Fetch(client *http.Client, exactSymbol string, baseTf string, start, end int64, market string) []BasicKline {
	tfMap := map[string]string{"1": "1m", "5": "5m", "15": "15m"}
	val, ok := tfMap[baseTf]
	if !ok {
		val = "1m"
	}

	baseURL := "https://fapi.binance.com/fapi/v1/klines"
	if market == "spot" {
		baseURL = "https://api.binance.com/api/v3/klines"
	}

	var res []BasicKline
	curr := start
	for curr < end {
		url := fmt.Sprintf("%s?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1500", baseURL, exactSymbol, val, curr, end)
		resp, err := client.Get(url)
		if err != nil {
			break
		}
		var raw []interface{}
		if json.NewDecoder(resp.Body).Decode(&raw) != nil || len(raw) == 0 {
			resp.Body.Close()
			break
		}
		resp.Body.Close()

		lastTs := curr
		for _, r := range raw {
			if item, ok := r.([]interface{}); ok && len(item) > 4 {
				ts := CheckTimestamp(utils.ParseFloat(item[0]))
				res = append(res, BasicKline{Timestamp: ts, Open: utils.ParseFloat(item[1]), High: utils.ParseFloat(item[2]), Low: utils.ParseFloat(item[3]), Close: utils.ParseFloat(item[4])})
				if ts > lastTs {
					lastTs = ts
				}
			}
		}
		if lastTs <= curr {
			break
		}
		curr = lastTs + 1
		time.Sleep(20 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
