package main

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// masker скрывает ссылки во всём, что hlsprobe сохраняет и печатает: хосты
// (в ссылках и любые доменные имена в тексте) становятся host1, host2…, значения параметров query — v1, v2…, имя
// пользователя — <user> (<USER> заглавными, <User> в смешанном регистре). Одинаковые значения получают одинаковые метки, поэтому
// по замаскированным данным видно, меняются ли edge-сервер и токены. Пути и имена
// сегментов остаются: по ним разбирается нумерация. Запросы идут по настоящим URL.
type masker struct {
	userRe *regexp.Regexp // nil — имя не маскируется

	mu     sync.Mutex
	hosts  map[string]string
	values map[string]string
	// known — хосты запросов и их домены второго уровня: последняя страховка,
	// вычищаются из текста в любом окружении, которое не поймали регулярки.
	known []knownDomain
}

type knownDomain struct {
	domain string
	re     *regexp.Regexp
	label  string
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
	hostRe = regexp.MustCompile(`(?i)((?:https?|wss?):(?:\\?/){2})([^/\\\s"'<>?#;,()\[\]{}|]+)`)
	// queryRe — query string: от «?» до пробела, кавычки или конца строки.
	queryRe = regexp.MustCompile(`\?([^\s"'<>#\\]*)`)
	// domainRe — доменное имя вне ссылки: в заголовках (CSP: wss://*.cdn.com),
	// в JSON с экранированными символами (\u002F) и просто в тексте.
	domainRe = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+([a-z]{2,24})\b`)
	// cookieRe — значение cookie в заголовках Set-Cookie и Cookie.
	cookieRe = regexp.MustCompile(`(^|;\s*)([^=;\s]+)=([^;]*)`)
)

// remember запоминает хост URL запроса (и его домен второго уровня), чтобы
// вычищать их из всех сохраняемых текстов.
func (m *masker) remember(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return
	}
	host := strings.ToLower(u.Hostname())
	m.mu.Lock()
	defer m.mu.Unlock()
	candidates := []string{host}
	if parts := strings.Split(host, "."); len(parts) > 2 {
		candidates = append(candidates, strings.Join(parts[len(parts)-2:], "."))
	}
	for _, d := range candidates {
		if !strings.Contains(d, ".") || slices.ContainsFunc(m.known, func(k knownDomain) bool { return k.domain == d }) {
			continue
		}
		m.known = append(m.known, knownDomain{
			domain: d,
			re:     regexp.MustCompile(`(?i)` + regexp.QuoteMeta(d)),
			label:  label(m.hosts, d, "host"),
		})
	}
	// Длинные имена — раньше коротких, иначе от edge.cdn.com останется «edge.host2».
	sort.Slice(m.known, func(i, j int) bool { return len(m.known[i].domain) > len(m.known[j].domain) })
}

// text маскирует ссылки и имя пользователя в тексте.
func (m *masker) text(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	s = hostRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := hostRe.FindStringSubmatch(match)
		return sub[1] + label(m.hosts, strings.ToLower(sub[2]), "host")
	})
	if m.userRe != nil {
		s = m.maskUser(s)
	}
	s = m.maskDomains(s)
	s = queryRe.ReplaceAllStringFunc(s, func(match string) string {
		pairs := strings.Split(match[1:], "&")
		for i, pair := range pairs {
			if k, v, ok := strings.Cut(pair, "="); ok && v != "" {
				pairs[i] = k + "=" + label(m.values, v, "v")
			}
		}
		return "?" + strings.Join(pairs, "&")
	})
	for _, k := range m.known {
		s = k.re.ReplaceAllString(s, k.label)
	}
	return s
}

// maskUser заменяет имя пользователя меткой, сохраняющей регистр: по ней видно,
// как имя запрашивалось.
func (m *masker) maskUser(s string) string {
	return m.userRe.ReplaceAllStringFunc(s, func(match string) string {
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

// maskDomains заменяет доменные имена вне ссылок — в том числе в путях и в тексте
// HTML. Не трогает имена файлов (s5.ts, player.js) и куски длинных имён с точками
// (origin.name.01…): после них точка и сразу буква или цифра.
func (m *masker) maskDomains(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range domainRe.FindAllStringSubmatchIndex(s, -1) {
		start, end := loc[0], loc[1]
		tld := strings.ToLower(s[loc[2]:loc[3]])
		continued := end+1 < len(s) && s[end] == '.' && isAlnum(s[end+1])
		if fileExts[tld] || continued {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(label(m.hosts, strings.ToLower(s[start:end]), "host"))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
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

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// fileExts — «зоны», которые на деле расширения файлов: такие имена не маскируются.
var fileExts = map[string]bool{
	"ts": true, "js": true, "mjs": true, "css": true, "html": true, "htm": true, "json": true, "xml": true,
	"txt": true, "png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "webp": true, "ico": true,
	"mp": true, "m": true, "aac": true, "key": true, "vtt": true, "webm": true, "mov": true, "woff": true,
}

// textFile сообщает, текстовый ли файл ответа (его содержимое маскируется).
func textFile(ext string) bool {
	return ext == ".m3u8" || ext == ".json"
}
