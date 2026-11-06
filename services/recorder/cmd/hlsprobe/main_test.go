package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Фейковые status API и CDN: alice в эфире, остальных нет.
func fakeSite(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/alice/":
			fmt.Fprintf(w, `{"room_status":"public","hls_source":"%s/hls/master.m3u8?token=1"}`, srv.URL)
		case "/hls/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",URI=\"audio.m3u8\"\n"+
				"#EXT-X-STREAM-INF:BANDWIDTH=10\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=99,RESOLUTION=1280x720,AUDIO=\"aud\"\nhigh.m3u8\n")
		case "/hls/low.m3u8", "/hls/high.m3u8", "/hls/audio.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:5\n#EXTINF:1,\ns5.ts?t=a\n#EXTINF:1,\ns6.ts?t=a\n#EXT-X-ENDLIST\n")
		case "/hls/s5.ts", "/hls/s6.ts":
			fmt.Fprint(w, "SEGMENT")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeSavesResponses(t *testing.T) {
	srv := fakeSite(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := run([]string{"-base-url", srv.URL + "/api", "-out", out, "-reloads", "2",
		"-nonexistent", "neverexistinguserbla", "https://site/female/Alice/"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	summary := stdout.String()
	for _, want := range []string{
		"status_raw_case", "<User>", "<USER>", "HTTP 404", // регистр и несуществующий пользователь
		"BANDWIDTH=99 RESOLUTION=1280x720",
		"reload 0 +0s: seq=5", "endlist=true",
		"segment1", "hls_source changed: false", "neverexistinguserbla",
		"--- master text:", "GROUP-ID=\"aud\"", "--- media_reload0 text:",
		"== rendition0_now", "video seq at last reload=5", "audio0_segment1",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
	for _, f := range []string{"summary.txt", "requests.jsonl"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Error(err)
		}
	}
}

func TestProbeNotLive(t *testing.T) {
	srv := fakeSite(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-base-url", srv.URL + "/api", "-out", t.TempDir(), "bob"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stream is not live") {
		t.Errorf("summary:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "<user>zz0probe404") {
		t.Errorf("summary:\n%s", stdout.String())
	}
}

func TestProbeUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

func TestProbeReloadsForDuration(t *testing.T) {
	// Плейлист без ENDLIST: перезагрузки идут, пока не пройдёт -duration (target 1 с).
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/live/":
			fmt.Fprintf(w, `{"room_status":"public","hls_source":"%s/media.m3u8"}`, srv.URL)
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\ns1.ts\n")
		default:
			fmt.Fprint(w, "SEG")
		}
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-base-url", srv.URL + "/api", "-out", t.TempDir(), "-reloads", "1", "-duration", "1500ms", "live"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if n := strings.Count(stdout.String(), "\nreload "); n < 2 {
		t.Errorf("%d reloads, want them to continue for -duration:\n%s", n, stdout.String())
	}
}

func TestMaskerHidesLinks(t *testing.T) {
	m := newMasker("alice")
	in := `{"hls_source":"https:\/\/edge1.cdn.com\/alice\/master.m3u8?session=abc&x=1"}
https://edge1.cdn.com/v1/origin.alice.01/seg_3_6646_video.m4s?session=abc
https://EDGE2.cdn.com/Alice/x.m3u8?session=def
chunk.m4s?session=abc
ALICE`
	want := `{"hls_source":"https:\/\/host1\/<user>\/master.m3u8?session=v1&x=v2"}
https://host1/v1/origin.<user>.01/seg_3_6646_video.m4s?session=v1
https://host2/<User>/x.m3u8?session=v3
chunk.m4s?session=v1
<USER>`
	if got := m.text(in); got != want {
		t.Errorf("masked:\n%s\nwant:\n%s", got, want)
	}

	h := m.headers(map[string][]string{
		"Set-Cookie": {"sid=secret; Path=/; Domain=edge1.cdn.com"},
		"Location":   {"https://edge1.cdn.com/next?session=abc"},
	})
	if got := h["Set-Cookie"][0]; strings.Contains(got, "secret") {
		t.Errorf("cookie not masked: %s", got)
	}
	if got := h["Location"][0]; got != "https://host1/next?session=v1" {
		t.Errorf("Location = %s", got)
	}
}

func TestProbeSavedFilesAreMasked(t *testing.T) {
	srv := fakeSite(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-base-url", srv.URL + "/api", "-out", out, "-reloads", "1", "alice"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), host) || strings.Contains(strings.ToLower(string(b)), "alice") || strings.Contains(string(b), "token=1") {
			t.Errorf("%s is not masked:\n%s", e.Name(), b)
		}
	}
	if strings.Contains(stdout.String(), host) {
		t.Error("printed summary is not masked")
	}
}
