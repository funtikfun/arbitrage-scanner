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

type OkxFetcher struct{}

func (f *OkxFetcher) Fetch(client *http.Client, exactSymbol string, baseTf string, start, end int64, market string) []BasicKline {
	tfMap := map[string]string{"1": "1m", "5": "5m", "15": "15m"}
	val, ok := tfMap[baseTf]
	if !ok {
		val = "5m"
	}

	targetSym := strings.TrimSuffix(exactSymbol, "USDT")
	instId := targetSym + "-USDT-SWAP"
	if market == "spot" {
		instId = targetSym + "-USDT"
	}

	var res []BasicKline
	currAfter := end + 1
	for currAfter > start {
		url := fmt.Sprintf("https://www.okx.com/api/v5/market/history-candles?instId=%s&bar=%s&limit=100&after=%d", instId, val, currAfter)
		var success bool
		var responseData [][]interface{}
		for retry := 0; retry < 5; retry++ {
			resp, err := client.Get(url)
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				resp.Body.Close()
				time.Sleep(2 * time.Second)
				continue
			}
			var raw struct {
				Code string          `json:"code"`
				Data [][]interface{} `json:"data"`
			}
			errDecode := json.NewDecoder(resp.Body).Decode(&raw)
			resp.Body.Close()
			if errDecode == nil && raw.Code == "0" {
				responseData = raw.Data
				success = true
				break
			}
			time.Sleep(1 * time.Second)
		}

		if !success || len(responseData) == 0 {
			break
		}

		oldestTs := currAfter
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
		if oldestTs >= currAfter || oldestTs <= start {
			break
		}
		currAfter = oldestTs
		time.Sleep(150 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
