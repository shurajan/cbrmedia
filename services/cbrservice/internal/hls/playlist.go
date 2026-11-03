package hls

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type encryptionKey struct {
	method string
	uri    string
	iv     []byte
}

type playlistSegment struct {
	uri      string
	duration float64
	seq      int
	key      *encryptionKey
	mapURI   string // init-сегмент fMP4 (#EXT-X-MAP), действующий для сегмента
}

type mediaPlaylist struct {
	targetDuration float64
	endList        bool
	segments       []playlistSegment
}

// variant — вариант потока из master-плейлиста.
type variant struct {
	uri       string
	bandwidth int
}

// resolveMediaPlaylist загружает masterURL и, если это master-плейлист,
// возвращает URL варианта с максимальным битрейтом. Иначе URL уже указывает
// на media-плейлист и возвращается без изменений.
func resolveMediaPlaylist(ctx context.Context, client *http.Client, masterURL string) (string, variant, error) {
	text, _, err := fetchText(ctx, client, masterURL)
	if err != nil {
		return "", variant{}, err
	}
	if !isMasterPlaylist(text) {
		return masterURL, variant{}, nil // уже media-плейлист
	}

	best, ok := bestVariant(text)
	if !ok {
		return "", variant{}, errors.New("master playlist had no usable variant")
	}
	mediaURL, err := resolveAgainst(masterURL, best.uri)
	if err != nil {
		return "", variant{}, err
	}
	return mediaURL, best, nil
}

// isMasterPlaylist сообщает, перечисляет ли плейлист варианты потока.
func isMasterPlaylist(text string) bool {
	return strings.Contains(text, "#EXT-X-STREAM-INF")
}

// variantURL возвращает абсолютный URL лучшего варианта master-плейлиста.
func variantURL(masterURL, text string) (string, bool) {
	best, ok := bestVariant(text)
	if !ok {
		return "", false
	}
	u, err := resolveAgainst(masterURL, best.uri)
	return u, err == nil
}

func resolveAgainst(baseURL, ref string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	return resolveURI(base, ref), nil
}

// bestVariant выбирает вариант с максимальным BANDWIDTH; при равенстве — последний.
func bestVariant(text string) (variant, bool) {
	var best variant
	found := false

	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		attrs := parseAttributes(line[strings.Index(line, ":")+1:])
		bw, _ := strconv.Atoi(attrs["BANDWIDTH"])
		// URI — следующая непустая строка, не являющаяся комментарием.
		for j := i + 1; j < len(lines); j++ {
			uri := strings.TrimSpace(lines[j])
			if uri == "" || strings.HasPrefix(uri, "#") {
				continue
			}
			if bw >= best.bandwidth {
				best = variant{uri: uri, bandwidth: bw}
				found = true
			}
			i = j
			break
		}
	}
	return best, found
}

func parseMediaPlaylist(text, playlistURL string) (*mediaPlaylist, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, err
	}

	pl := &mediaPlaylist{}
	seq := 0
	var curKey *encryptionKey
	var curMap string
	var pendingDur float64

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			pl.targetDuration, _ = strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)

		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")))

		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			pl.endList = true

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs := parseAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if uri := attrs["URI"]; uri != "" {
				curMap = resolveURI(base, uri)
			}

		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			curKey = parseKey(base, strings.TrimPrefix(line, "#EXT-X-KEY:"))

		case strings.HasPrefix(line, "#EXTINF:"):
			val := strings.TrimPrefix(line, "#EXTINF:")
			if c := strings.Index(val, ","); c >= 0 {
				val = val[:c]
			}
			pendingDur, _ = strconv.ParseFloat(strings.TrimSpace(val), 64)

		case strings.HasPrefix(line, "#"):
			// прочие теги игнорируются

		default:
			// URI медиа-сегмента
			pl.segments = append(pl.segments, playlistSegment{
				uri:      resolveURI(base, line),
				duration: pendingDur,
				seq:      seq,
				key:      curKey,
				mapURI:   curMap,
			})
			seq++
			pendingDur = 0
		}
	}

	return pl, nil
}

func parseKey(base *url.URL, attrStr string) *encryptionKey {
	attrs := parseAttributes(attrStr)
	k := &encryptionKey{method: attrs["METHOD"]}
	if uri := attrs["URI"]; uri != "" {
		k.uri = resolveURI(base, uri)
	}
	if ivStr := attrs["IV"]; ivStr != "" {
		ivStr = strings.TrimPrefix(strings.TrimPrefix(ivStr, "0x"), "0X")
		if b, err := hex.DecodeString(ivStr); err == nil {
			k.iv = b
		}
	}
	if k.method == "" {
		k.method = "NONE"
	}
	return k
}

// resolveURI разрешает (возможно относительную) ссылку относительно URL плейлиста.
func resolveURI(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// parseAttributes разбирает список атрибутов HLS, например
// METHOD=AES-128,URI="key.bin",IV=0x00..., в map.
func parseAttributes(s string) map[string]string {
	attrs := make(map[string]string)
	var key, val strings.Builder
	inKey, inQuotes := true, false

	flush := func() {
		k := strings.TrimSpace(key.String())
		if k != "" {
			attrs[k] = val.String()
		}
		key.Reset()
		val.Reset()
		inKey = true
	}

	for _, r := range s {
		switch {
		case inKey && r == '=':
			inKey = false
		case r == '"':
			inQuotes = !inQuotes
		case r == ',' && !inQuotes:
			flush()
		case inKey:
			key.WriteRune(r)
		default:
			val.WriteRune(r)
		}
	}
	flush()
	return attrs
}
