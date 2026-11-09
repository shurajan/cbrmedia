// Package statusapi — клиент внешнего status API: состояние комнаты пользователя
// и текущая ссылка на HLS-стрим. Нужен recorder (опрос и запись) и api (живой просмотр).
package statusapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Room — состояние комнаты пользователя по данным status API.
type Room struct {
	Status string // room_status
	User   string // broadcaster_username
	HLSURL string // hls_source; пусто, если стрим не идёт
}

// Client опрашивает status API: GET <baseURL>/<user>/.
type Client struct {
	client  *http.Client
	baseURL string
}

// NewClient создаёт клиент status API.
func NewClient(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL}
}

// Пределы одного запроса к status API: транспорт ограничивает только соединение
// и заголовки, без общего таймаута зависшее тело ответа остановило бы опрос.
var statusTimeout = 30 * time.Second // переменная — чтобы тесты могли её сократить

const (
	statusMaxBody  = 1 << 20 // ответ — небольшой JSON
	errorBodyLimit = 200     // сколько тела ошибки (часто HTML-страница) попадает в лог
)

// Fetch возвращает состояние комнаты и HTTP-статус ответа (0 — сетевая ошибка).
// Не-2xx и некорректный JSON считаются ошибкой.
func (c *Client) Fetch(ctx context.Context, user string) (Room, int, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	endpoint := c.baseURL + "/" + user + "/"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Room{}, 0, fmt.Errorf("build request %s: %w", endpoint, err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Room{}, 0, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, statusMaxBody))
	if status < 200 || status >= 300 {
		// Статус известен и без тела — по нему вызывающий решает, что делать.
		if len(body) > errorBodyLimit {
			body = append(body[:errorBodyLimit:errorBodyLimit], "…"...)
		}
		return Room{}, status, &StatusError{
			Code:       status,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			Message:    fmt.Sprintf("HTTP %d from %s: %s", status, endpoint, body),
		}
	}
	if err != nil {
		return Room{}, status, fmt.Errorf("read response from %s: %w", endpoint, err)
	}

	// Разбор через map, чтобы неожиданные типы полей не ломали опрос.
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return Room{}, status, fmt.Errorf("decode JSON from %s: %w", endpoint, err)
	}
	str := func(key string) string {
		s, _ := data[key].(string)
		return s
	}
	return Room{
		Status: str("room_status"),
		User:   str("broadcaster_username"),
		HLSURL: str("hls_source"),
	}, status, nil
}

// StatusError — ответ status API не 2xx.
type StatusError struct {
	Code       int
	RetryAfter time.Duration // из заголовка Retry-After; 0 — не задан
	Message    string
}

func (e *StatusError) Error() string { return e.Message }

// RetryAfter возвращает Retry-After из ошибки Fetch или 0.
func RetryAfter(err error) time.Duration {
	var se *StatusError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}

// parseRetryAfter разбирает Retry-After: число секунд или HTTP-дата. 0 — нет или некорректен.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return max(time.Duration(sec)*time.Second, 0)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// NewHTTPClient создаёт HTTP-клиент для опроса status API (и скачивания стримов в recorder).
//
// Таймауты покрывают установку соединения и ожидание заголовков, но не чтение
// тела, поэтому долгие загрузки сегментов не прерываются.
// Проверка TLS-сертификатов отключена.
func NewHTTPClient(proxy string, log *slog.Logger) (*http.Client, error) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // осознанно: см. recorder/README.md
	}
	log.Warn("TLS verification is DISABLED for status polling and stream downloads")

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", proxy, err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		log.Info("using proxy for status polling and downloads", "proxy", proxy)
	}

	return &http.Client{Transport: transport}, nil
}
