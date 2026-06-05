package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type BybitFetcher struct{}

func (f *BybitFetcher) Fetch(client *http.Client, exactSymbol string, baseTf string, start, end int64, market string) []BasicKline {
	tfMap := map[string]string{"1": "1", "5": "5", "15": "15"}
	val, ok := tfMap[baseTf]
	if !ok {
		val = "1"
	}

	cat := "linear"
	if market == "spot" {
		cat = "spot"
	}

	var res []BasicKline
	currEnd := end
	for currEnd > start {
		url := fmt.Sprintf("https://api.bybit.com/v5/market/kline?category=%s&symbol=%s&interval=%s&start=%d&end=%d&limit=1000", cat, exactSymbol, val, start, currEnd)
		resp, err := client.Get(url)
		if err != nil {
			break
		}
		var raw struct {
			Result struct {
				List [][]interface{} `json:"list"`
			} `json:"result"`
		}
		if json.NewDecoder(resp.Body).Decode(&raw) != nil || len(raw.Result.List) == 0 {
			resp.Body.Close()
			break
		}
		resp.Body.Close()

		oldestTs := currEnd
		for _, k := range raw.Result.List {
			if len(k) > 4 {
				ts := CheckTimestamp(utils.ParseFloat(k[0]))
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
		time.Sleep(20 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
