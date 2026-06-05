package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyNode хранит 1 выделенный IP адрес и следит за его блокировками
type ProxyNode struct {
	RawURL    string
	Transport *http.Transport
	banUntil  time.Time
	mu        sync.RWMutex
}

// Проверка: Находится ли прокси под баном от бирж
func (n *ProxyNode) IsBanned() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return time.Now().Before(n.banUntil)
}

// Ставим ограничение, если словили код 429
func (n *ProxyNode) Ban(duration time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.banUntil = time.Now().Add(duration)
}

// SmartTransport перехватывает http-сообщения перед отправкой на сервер.
// Обертка вокруг стандартного транспорта Go
type SmartTransport struct {
	proxies []*ProxyNode
	direct  *http.Transport
	counter uint32
	retries int
}

// Функция сборки Транспортов
func createBaseTransport(proxyURL string) (*http.Transport, error) {
	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:      100,
		IdleConnTimeout:   90 * time.Second,
	}

	if proxyURL != "" {
		pURL, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(pURL)
	}
	return tr, nil
}

// InitSmartTransport читает proxies.json и заряжает внутренний барабан адресов
func InitSmartTransport(configPath string, maxRetries int) *SmartTransport {
	sm := &SmartTransport{
		retries: maxRetries,
	}

	// Дефолтное соединение без прокси
	dTr, _ := createBaseTransport("")
	sm.direct = dTr

	file, err := os.ReadFile(configPath)
	if err == nil {
		var list []string
		if json.Unmarshal(file, &list) == nil {
			for _, pRaw := range list {
				if tr, err := createBaseTransport(pRaw); err == nil {
					sm.proxies = append(sm.proxies, &ProxyNode{
						RawURL:    pRaw,
						Transport: tr,
					})
				} else {
					log.Printf("⚠️ Ошибка парсинга прокси %s: %v", pRaw, err)
				}
			}
		}
	}

	if len(sm.proxies) > 0 {
		log.Printf("🛡️ Менеджер прокси инициализирован: Заряжено %d IP адресов", len(sm.proxies))
	} else {
		log.Printf("🛡️ Менеджер прокси работает в Direct-режиме (без обфускации IP)")
	}

	return sm
}

// Логика "выборки следующего живого IP". Метод Round-Robin.
func (s *SmartTransport) getNextAvailableProxy() *ProxyNode {
	total := len(s.proxies)
	if total == 0 {
		return nil
	}

	// Попытка найти не забаненный прокси
	for i := 0; i < total; i++ {
		idx := atomic.AddUint32(&s.counter, 1) % uint32(total)
		node := s.proxies[idx]
		if !node.IsBanned() {
			return node
		}
	}

	// Если ВСЕ прокси в бане Cloudflare, возвращаем пустой,
	// запрос пойдет напрямую как fallback!
	return nil
}

// RoundTrip — главное "сердце". Встраивается в `http.Client`. Вызывается каждый раз когда сканер отправляет API Request.
func (s *SmartTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	// Чтобы можно было отправлять запрос заново при прокси-сбоях, читаем body (в REST GET запросах body пустой)
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}

	var lastErr error
	var resp *http.Response

	for attempt := 0; attempt < s.retries; attempt++ {
		// Клонируем запрос, чтобы контексты и Headers сохранялись в памяти при retries
		newReq := req.Clone(req.Context())
		if bodyBytes != nil {
			newReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		proxy := s.getNextAvailableProxy()
		transport := s.direct
		if proxy != nil {
			transport = proxy.Transport
		}

		// Выполняем боевой запрос через биржу
		resp, lastErr = transport.RoundTrip(newReq)

		// 1. Сетевые проблемы таймаута:
		if lastErr != nil {
			if proxy != nil {
				proxy.Ban(1 * time.Minute) // Сетевой сбой IP? Охладим на минуту!
				continue
			} else {
				time.Sleep(300 * time.Millisecond) // База тоже дает таймаут, задержимся
				continue
			}
		}

		// 2. Биржа говорит 429 - Слишком много запросов (Мы перешли черту Limit):
		if resp.StatusCode == 429 || resp.StatusCode == 403 || resp.StatusCode >= 500 {
			if proxy != nil {
				proxy.Ban(5 * time.Minute) // Обнаружен Бан IP. Блокируем его жестче!
			}

			resp.Body.Close()
			time.Sleep(1 * time.Second) // Дадим время Cloudflare разжать хватку
			continue
		}

		// 3. Отличный код (200, 400 и тд).
		// Очищаем и отдаем результат клиенту биржи в Сканер.
		return resp, nil
	}

	// Возврат последнего, если так и не получилось даже за ретраи:
	if resp != nil {
		return resp, nil
	}
	return nil, fmt.Errorf("все попытки (retries) к API провалены. Ошибка: %w", lastErr)
}
