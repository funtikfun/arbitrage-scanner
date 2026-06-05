package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// Безопасное приведение данных API для вытягивания процентов с любыми запятыми (Защита типов GoLang)
func ParseFloat(v interface{}) float64 {
	if v == nil {
		return 0.0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprintf("%v", val)), 64)
		return f
	}
}

// Хардкодим каноничные маски для «разброда названий» на биржах (Всё будет сводиться к эталонным именам слева)
// Можно расширять этот словарь до бесконечности любыми мемкоинами при надобности!
var aliasMapping = map[string]string{
	"LUNA2":  "LUNA",
	"MIOTA":  "IOTA",
	"PEPE2":  "PEPE2.0",
	"XBT":    "BTC",
	"BCHABC": "BCH",
	"BCC":    "BCH",
}

// Это наш универсальный парсер, используемый нашим новым Демоном:
// Принимает Базу Актива биржи и возвращает ЕДИНЫЙ кристальный "Символ без нулей" с Истинным кратным объёмом
func ParseSymbolMeta(sym string) (baseName string, multiplier float64) {
	s := strings.ToUpper(sym)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")

	// Жесткая вырезка всех хвостов (Спот, Контракты, Деривативы)
	suffixes := []string{"USDTM", "USDT", "USDC", "SWAP", "PERP", "USD"}
	for _, suff := range suffixes {
		if strings.HasSuffix(s, suff) {
			s = strings.TrimSuffix(s, suff)
			break
		}
	}

	multiplier = 1.0 // Эталон лотности контрактов по умолчанию для биткоина/альтов

	// Блокировщик расчётов для классических цифровых имён: если Биржа прислала токен типа 1INCH — оставляем 1
	if strings.Contains(s, "1INCH") {
		return applyAliases(s), multiplier
	}

	// Точно ловим привязку "Мульты-Мемоинвест": вытаскиваем цифры, если они вставлены Биржами прямо в имя (1000PEPE)
	prefixes := []string{"10000000", "1000000", "100000", "10000", "1000", "100", "10"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			cleanStr := strings.TrimPrefix(s, p)
			if val, err := strconv.ParseFloat(p, 64); err == nil {
				// Защита, чтобы после отрезания множителя от "100USDT" (которое стало пустой строкой "") всё работало четко
				if len(cleanStr) > 0 {
					multiplier = val
					s = cleanStr
					break
				}
			}
		}
	}

	return applyAliases(s), multiplier
}

// Заворачиваем под общие корпоративные словари алиасов! LUNA2 сольётся с LUNA автоматически.
func applyAliases(base string) string {
	if canonical, ok := aliasMapping[base]; ok {
		return canonical
	}
	return base
}
