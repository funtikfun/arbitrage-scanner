package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type bitmartFundingRecord struct {
	Symbol      string `json:"symbol"`
	FundingRate string `json:"funding_rate"`
	FundingTime string `json:"funding_time"`
}

type bitmartResponse struct {
	Code int `json:"code"`
	Data struct {
		List []bitmartFundingRecord `json:"list"`
	} `json:"data"`
}

func main() {
	targetSymbol := "ANTHROPICUSDT"
	client := &http.Client{Timeout: 10 * time.Second}

	// 🛡️ Настройка Прокси (как в основном коде)
	proxyFile, _ := os.ReadFile("internal/proxy/proxies.json")
	var proxies []string
	json.Unmarshal(proxyFile, &proxies)
	if len(proxies) > 0 {
		pURL, _ := url.Parse(proxies[0])
		client.Transport = &http.Transport{Proxy: http.ProxyURL(pURL)}
	}

	fmt.Printf("🚀 ТЕСТ МЕТОДА: ШАГАЕМ ВПЕРЕД (FORWARD WALKING)\n")

	// Точка старта: 30 дней назад
	startTime := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()

	for page := 1; page <= 3; page++ {
		// ВНИМАНИЕ: Проверяем оба формата (иногда Bitmart хочет startTime, иногда start_time)
		// Используем snake_case как основной по документации
		urlStr := fmt.Sprintf("https://api-cloud-v2.bitmart.com/contract/public/funding-rate-history?symbol=%s&limit=100&start_time=%d",
			targetSymbol, startTime)

		fmt.Printf("\n➡️ ШАГ #%d | Запрос от: %s (%d)\n", page, time.UnixMilli(startTime).UTC().Format("02.01 15:04"), startTime)

		resp, err := client.Get(urlStr)
		if err != nil {
			fmt.Println("❌ Ошибка:", err)
			break
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result bitmartResponse
		json.Unmarshal(body, &result)

		records := result.Data.List
		count := len(records)

		if count == 0 {
			fmt.Println("⚠️ Записей не найдено.")
			break
		}

		firstTs, _ := strconv.ParseInt(records[0].FundingTime, 10, 64)
		lastTs, _ := strconv.ParseInt(records[count-1].FundingTime, 10, 64)

		fmt.Printf("   ✅ Получено: %d записей\n", count)
		fmt.Printf("   📉 Период в пакете: %s --- %s\n",
			time.UnixMilli(firstTs).UTC().Format("02.01 15:04"),
			time.UnixMilli(lastTs).UTC().Format("02.01 15:04"))

		// Если биржа вернула данные, которые мы уже видели — пагинация не работает
		if lastTs == startTime || firstTs == lastTs {
			fmt.Println("   ⛔️ API не реагирует на смещение start_time. Пагинация заблокирована.")
			break
		}

		// Следующий запрос начинаем с времени ПОСЛЕДНЕЙ записи в текущем пакете + 1 мс
		// (Так мы шагаем ИЗ ПРОШЛОГО в НАСТОЯЩЕЕ)
		startTime = lastTs + 1

		// Если последняя запись уже "свежая" (допустим за сегодня), выходим
		if time.Now().UnixMilli()-lastTs < 3600000 {
			fmt.Println("   🎯 Достигли актуальных данных.")
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
}
