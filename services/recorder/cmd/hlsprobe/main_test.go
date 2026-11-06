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
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=10\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=99,RESOLUTION=1280x720\nhigh.m3u8\n")
		case "/hls/low.m3u8", "/hls/high.m3u8":
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

	code := run([]string{"-base-url", srv.URL + "/api", "-out", out, "-reloads", "2", "https://site/female/Alice/"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	summary := stdout.String()
	for _, want := range []string{
		"status_raw_case", "Alice", "HTTP 404", // регистр и несуществующий пользователь
		"BANDWIDTH=99 RESOLUTION=1280x720",
		"reload 0: seq=5", "endlist=true",
		"segment1", "hls_source changed: false",
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
}

func TestProbeUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
