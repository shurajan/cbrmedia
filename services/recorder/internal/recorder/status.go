package recorder

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Room — состояние комнаты пользователя по данным status API.
type Room struct {
	Status string // room_status
	User   string // broadcaster_username
	HLSURL string // hls_source; пусто, если стрим не идёт
}

// StatusClient опрашивает status API: GET <baseURL>/<user>/.
type StatusClient struct {
	client  *http.Client
	baseURL string
}

// NewStatusClient создаёт клиент status API.
func NewStatusClient(client *http.Client, baseURL string) *StatusClient {
	return &StatusClient{client: client, baseURL: baseURL}
}

// Fetch возвращает состояние комнаты и HTTP-статус ответа (0 — сетевая ошибка).
// Не-2xx и некорректный JSON считаются ошибкой.
func (c *StatusClient) Fetch(ctx context.Context, user string) (Room, int, error) {
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Room{}, status, fmt.Errorf("read response from %s: %w", endpoint, err)
	}
	if status < 200 || status >= 300 {
		return Room{}, status, fmt.Errorf("HTTP %d from %s: %s", status, endpoint, body)
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

// NewHTTPClient создаёт HTTP-клиент для опроса и скачивания стримов.
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
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // осознанно: см. README.md
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
