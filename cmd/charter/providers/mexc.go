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

type MexcFetcher struct{}

func (f *MexcFetcher) Fetch(client *http.Client, exactSymbol string, tf string, start, end int64, market string) []BasicKline {
	targetSym := strings.TrimSuffix(exactSymbol, "USDT")
	var res []BasicKline

	currSec := start / 1000
	endSec := end / 1000
	var stepSec int64 = 500 * 5 * 60
	if tf == "1" {
		stepSec = 500 * 60
	}

	for currSec < endSec {
		chunkEnd := currSec + stepSec
		if chunkEnd > endSec {
			chunkEnd = endSec
		}

		if market == "spot" {
			spotTf := map[string]string{"1": "1m", "5": "5m", "15": "15m"}[tf]
			if spotTf == "" {
				spotTf = "5m"
			}
			url := fmt.Sprintf("https://api.mexc.com/api/v3/klines?symbol=%sUSDT&interval=%s&startTime=%d&endTime=%d", targetSym, spotTf, currSec*1000, chunkEnd*1000)
			resp, err := client.Get(url)
			if err == nil {
				var raw [][]interface{}
				if json.NewDecoder(resp.Body).Decode(&raw) == nil {
					for _, item := range raw {
						if len(item) > 4 {
							res = append(res, BasicKline{
								Timestamp: CheckTimestamp(utils.ParseFloat(item[0])),
								Open:      utils.ParseFloat(item[1]),
								High:      utils.ParseFloat(item[2]),
								Low:       utils.ParseFloat(item[3]),
								Close:     utils.ParseFloat(item[4]),
							})
						}
					}
				}
				resp.Body.Close()
			}
		} else {
			url := fmt.Sprintf("https://contract.mexc.com/api/v1/contract/kline/%s_USDT?interval=%s&start=%d&end=%d", targetSym, map[string]string{"1": "Min1", "5": "Min5", "15": "Min15"}[tf], currSec, chunkEnd)
			resp, err := client.Get(url)
			if err == nil {
				var raw struct {
					Data struct {
						Time  []interface{} `json:"time"`
						Open  []interface{} `json:"open"`
						High  []interface{} `json:"high"`
						Low   []interface{} `json:"low"`
						Close []interface{} `json:"close"`
					} `json:"data"`
				}
				if json.NewDecoder(resp.Body).Decode(&raw) == nil && len(raw.Data.Time) > 0 {
					for i := 0; i < len(raw.Data.Time); i++ {
						res = append(res, BasicKline{
							Timestamp: CheckTimestamp(utils.ParseFloat(raw.Data.Time[i])),
							Open:      utils.ParseFloat(raw.Data.Open[i]),
							High:      utils.ParseFloat(raw.Data.High[i]),
							Low:       utils.ParseFloat(raw.Data.Low[i]),
							Close:     utils.ParseFloat(raw.Data.Close[i]),
						})
					}
				}
				resp.Body.Close()
			}
		}
		currSec = chunkEnd
		time.Sleep(30 * time.Millisecond)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Timestamp < res[j].Timestamp })
	return res
}
