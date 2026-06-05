package bybit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/testserver/arbitrage-scanner/utils"
)

type bybitFuturesTicker struct {
	Symbol          string
	BidPrice        float64
	AskPrice        float64
	MarkPrice       float64
	IndexPrice      float64
	FundingRate     float64
	NextFundingTime int64
	FundingInterval int

	// ДОБАВЛЯЕМ ОБЪЕМ И ТАЙМШТАМП К ПОЛЯМ БИРЖИ
	BidSize   float64
	AskSize   float64
	Timestamp int64
}

func fetchBybitFuturesTickers(client *http.Client) (map[string]bybitFuturesTicker, error) {
	resp1, err := client.Get("https://api.bybit.com/v5/market/tickers?category=linear")
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close()
	var raw1 struct {
		Result struct {
			List []map[string]interface{} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&raw1); err != nil {
		return nil, err
	}

	intervalMap := make(map[string]int)

	for _, statusMode := range []string{"Trading", "PreLaunch"} {
		cursor := ""
		for {
			query := url.Values{}
			query.Set("category", "linear")
			query.Set("limit", "1000")
			query.Set("status", statusMode)
			if cursor != "" {
				query.Set("cursor", cursor)
			}

			reqURL := "https://api.bybit.com/v5/market/instruments-info?" + query.Encode()
			resp2, err := client.Get(reqURL)
			if err != nil {
				break
			}

			var raw2 struct {
				RetCode int `json:"retCode"`
				Result  struct {
					List           []map[string]interface{} `json:"list"`
					NextPageCursor string                   `json:"nextPageCursor"`
				} `json:"result"`
			}
			if err := json.NewDecoder(resp2.Body).Decode(&raw2); err != nil || raw2.RetCode != 0 {
				resp2.Body.Close()
				break
			}
			resp2.Body.Close()

			for _, instr := range raw2.Result.List {
				sym := fmt.Sprintf("%v", instr["symbol"])
				mins := int(utils.ParseFloat(instr["fundingInterval"]))
				hours := mins / 60
				if hours <= 0 {
					hours = 8
				}
				intervalMap[sym] = hours
			}
			if raw2.Result.NextPageCursor == "" {
				break
			}
			cursor = raw2.Result.NextPageCursor
		}
	}

	result := make(map[string]bybitFuturesTicker)
	for _, t := range raw1.Result.List {
		sym := fmt.Sprintf("%v", t["symbol"])
		if !strings.HasSuffix(sym, "USDT") {
			continue
		}
		bid := utils.ParseFloat(t["bid1Price"])
		if bid <= 0 {
			continue
		}
		interval := intervalMap[sym]
		if interval == 0 {
			interval = 8
		}

		// ДОБАВЛЕННЫЕ ДАННЫЕ О РАЗМЕРАХ СТАКАНА У Bybit "bid1Size / ask1Size"
		result[sym] = bybitFuturesTicker{
			Symbol:    sym,
			BidPrice:  bid,
			AskPrice:  utils.ParseFloat(t["ask1Price"]),
			BidSize:   utils.ParseFloat(t["bid1Size"]), // << ВОТ ОНИ!
			AskSize:   utils.ParseFloat(t["ask1Size"]),
			Timestamp: time.Now().UnixMilli(), // ФИКСИРУЕМ ПИКУ СВЕТОФОРА

			MarkPrice:       utils.ParseFloat(t["markPrice"]),
			IndexPrice:      utils.ParseFloat(t["indexPrice"]),
			FundingRate:     utils.ParseFloat(t["fundingRate"]),
			NextFundingTime: int64(utils.ParseFloat(t["nextFundingTime"])),
			FundingInterval: interval,
		}
	}
	return result, nil
}
