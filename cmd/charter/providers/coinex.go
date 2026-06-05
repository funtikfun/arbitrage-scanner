package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type CoinexFetcher struct{}

func (f *CoinexFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	coinexTf := map[string]string{"1": "1min", "5": "5min", "15": "15min"}[tf]
	if coinexTf == "" {
		coinexTf = "5min"
	}

	var res []BasicKline
	currEnd := end

	// 🔴 ЦИКЛ ПАГИНАЦИИ назад во времени, чтобы вытянуть полные 30 дней истории!
	for currEnd > start {
		var url string
		if market == "spot" {
			url = fmt.Sprintf("https://api.coinex.com/v2/spot/kline?market=%s&period=%s&limit=1000&start_time=%d&end_time=%d",
				exactSymbol, coinexTf, start, currEnd)
		} else {
			url = fmt.Sprintf("https://api.coinex.com/v2/futures/kline?market=%s&period=%s&limit=1000&start_time=%d&end_time=%d",
				exactSymbol, coinexTf, start, currEnd)
		}

		resp, err := client.Get(url)
		if err != nil {
			break
		}

		var raw struct {
			Code int `json:"code"`
			Data []struct {
				CreatedAt int64  `json:"created_at"`
				Open      string `json:"open"`
				Close     string `json:"close"`
				High      string `json:"high"`
				Low       string `json:"low"`
			} `json:"data"`
		}

		if json.NewDecoder(resp.Body).Decode(&raw) != nil || raw.Code != 0 || len(raw.Data) == 0 {
			resp.Body.Close()
			break
		}
		resp.Body.Close()

		oldestTs := currEnd
		for _, k := range raw.Data {
			ts := CheckTimestamp(float64(k.CreatedAt))
			if ts < oldestTs {
				oldestTs = ts
			}
			res = append(res, BasicKline{
				Timestamp: ts,
				Open:      utils.ParseFloat(k.Open),
				Close:     utils.ParseFloat(k.Close),
				High:      utils.ParseFloat(k.High),
				Low:       utils.ParseFloat(k.Low),
			})
		}

		// Если oldestTs не уменьшается, выходим во избежание бесконечного цикла
		if oldestTs >= currEnd {
			break
		}
		currEnd = oldestTs - 1
		time.Sleep(30 * time.Millisecond) // Защита от лимитов запросов API
	}

	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
