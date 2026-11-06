package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// masker скрывает ссылки во всём, что hlsprobe сохраняет и печатает: хосты
// становятся host1, host2…, значения параметров query — v1, v2…, имя
// пользователя — <user> (<USER> заглавными, <User> в смешанном регистре). Одинаковые значения получают одинаковые метки, поэтому
// по замаскированным данным видно, меняются ли edge-сервер и токены. Пути и имена
// сегментов остаются: по ним разбирается нумерация. Запросы идут по настоящим URL.
type masker struct {
	userRe *regexp.Regexp // nil — имя не маскируется

	mu     sync.Mutex
	hosts  map[string]string
	values map[string]string
}

func newMasker(user string) *masker {
	m := &masker{hosts: map[string]string{}, values: map[string]string{}}
	if user != "" {
		m.userRe = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(user))
	}
	return m
}

var (
	// hostRe — схема и хост абсолютной ссылки (в JSON косая черта может быть экранирована).
	hostRe = regexp.MustCompile(`(?i)(https?:(?:\\?/){2})([^/\\\s"'<>?#]+)`)
	// queryRe — query string: от «?» до пробела, кавычки или конца строки.
	queryRe = regexp.MustCompile(`\?([^\s"'<>#\\]*)`)
	// cookieRe — значение cookie в заголовках Set-Cookie и Cookie.
	cookieRe = regexp.MustCompile(`(^|;\s*)([^=;\s]+)=([^;]*)`)
)

// text маскирует ссылки и имя пользователя в тексте.
func (m *masker) text(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	s = hostRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := hostRe.FindStringSubmatch(match)
		return sub[1] + label(m.hosts, strings.ToLower(sub[2]), "host")
	})
	s = queryRe.ReplaceAllStringFunc(s, func(match string) string {
		pairs := strings.Split(match[1:], "&")
		for i, pair := range pairs {
			if k, v, ok := strings.Cut(pair, "="); ok && v != "" {
				pairs[i] = k + "=" + label(m.values, v, "v")
			}
		}
		return "?" + strings.Join(pairs, "&")
	})
	if m.userRe != nil {
		// Метка сохраняет регистр: по ней видно, как имя запрашивалось.
		s = m.userRe.ReplaceAllStringFunc(s, func(match string) string {
			switch match {
			case strings.ToLower(match):
				return "<user>"
			case strings.ToUpper(match):
				return "<USER>"
			default:
				return "<User>"
			}
		})
	}
	return s
}

// headers маскирует значения заголовков; cookies скрываются целиком.
func (m *masker) headers(h map[string][]string) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vals := range h {
		masked := make([]string, len(vals))
		for i, v := range vals {
			if strings.EqualFold(k, "Set-Cookie") || strings.EqualFold(k, "Cookie") {
				v = cookieRe.ReplaceAllString(v, "$1$2=<masked>")
			}
			masked[i] = m.text(v)
		}
		out[k] = masked
	}
	return out
}

// label возвращает метку для значения, заводя новую при первой встрече.
func label(labels map[string]string, value, prefix string) string {
	if l, ok := labels[value]; ok {
		return l
	}
	l := fmt.Sprintf("%s%d", prefix, len(labels)+1)
	labels[value] = l
	return l
}

// textFile сообщает, текстовый ли файл ответа (его содержимое маскируется).
func textFile(ext string) bool {
	return ext == ".m3u8" || ext == ".json"
}
