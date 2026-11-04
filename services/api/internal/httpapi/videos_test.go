package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// generousTest gives handlers that may shell out to ffprobe time to finish.
var generousTest = fiber.TestConfig{Timeout: 30 * time.Second, FailOnTimeout: true}

// makeVideoDir creates baseDir/<name> containing a minimal HLS playlist.
func makeVideoDir(t *testing.T, baseDir, name string) {
	t.Helper()
	dir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:10\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:9.000,\n" +
		"segment0.ts\n" +
		"#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segment0.ts"), []byte("ts-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListVideos(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "alpha")
	makeVideoDir(t, base, "beta")
	// A hidden dir and a plain file must be ignored.
	if err := os.MkdirAll(filepath.Join(base, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory without a playlist must be skipped.
	if err := os.MkdirAll(filepath.Join(base, "no-playlist"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/videos", listVideos(base))

	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	var files []MediaFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d videos, want 2 (%+v)", len(files), files)
	}
	for _, f := range files {
		if f.Name != "alpha" && f.Name != "beta" {
			t.Errorf("unexpected video %q", f.Name)
		}
		if f.HLSURL == "" || f.ID == "" {
			t.Errorf("video %q is missing id/url: %+v", f.Name, f)
		}
	}
}

func TestStreamHLSFilePlaylist(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "alpha")

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	req := httptest.NewRequest(http.MethodGet, "/videos/alpha/playlist.m3u8", nil)
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type = %q, want application/vnd.apple.mpegurl", ct)
	}
}

func TestStreamHLSFileSegmentContentType(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "alpha")

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	req := httptest.NewRequest(http.MethodGet, "/videos/alpha/segment0.ts", nil)
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/MP2T" {
		t.Errorf("Content-Type = %q, want video/MP2T", ct)
	}
}

func TestStreamHLSFileNotFound(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "alpha")

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	req := httptest.NewRequest(http.MethodGet, "/videos/alpha/missing.m3u8", nil)
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
}

func TestDeleteVideo(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "alpha")

	app := fiber.New()
	app.Delete("/videos/:videoname", deleteVideo(base, discard))

	req := httptest.NewRequest(http.MethodDelete, "/videos/alpha", nil)
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if _, err := os.Stat(filepath.Join(base, "alpha")); !os.IsNotExist(err) {
		t.Errorf("expected video dir to be removed, stat err = %v", err)
	}
}

func doRequest(t *testing.T, app *fiber.App, method, target string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, "http://x/", nil)
	req.URL.Opaque = target // сырой путь, без нормализации net/http
	resp, err := app.Test(req, generousTest)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestStreamFileRejectsSiblingTraversal(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "a")
	makeVideoDir(t, base, "ab") // соседняя папка с общим префиксом имени

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	for _, path := range []string{
		"/videos/a/../ab/playlist.m3u8",
		"/videos/a/../../etc/passwd",
		"/videos/a/",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		req.URL.Opaque = path // сырой путь, без нормализации net/http
		resp, err := app.Test(req, generousTest)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode == fiber.StatusOK {
			t.Errorf("%s: status 200, traversal must be rejected", path)
		}
	}
}

func TestStreamFileRejectsHiddenVideo(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, ".meta")

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	if resp := doRequest(t, app, http.MethodGet, "/videos/.meta/playlist.m3u8"); resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestDeleteVideoRejectsInvalidNames(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "media")
	makeVideoDir(t, base, "alpha")
	sentinel := filepath.Join(parent, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Delete("/videos/:videoname", deleteVideo(base, discard))

	for _, target := range []string{"/videos/..", "/videos/.", "/videos/.hidden"} {
		if resp := doRequest(t, app, http.MethodDelete, target); resp.StatusCode == fiber.StatusOK {
			t.Errorf("%s: status 200, must be rejected", target)
		}
	}
	// Закодированные варианты после раскодирования тоже должны отклоняться.
	for _, target := range []string{"/videos/%2E%2E", "/videos/..%2F..", "/videos/%2E", "/videos/%2Ehidden", "/videos/%zz"} {
		if resp := doRequest(t, app, http.MethodDelete, target); resp.StatusCode == fiber.StatusOK {
			t.Errorf("%s: status 200, must be rejected", target)
		}
	}

	if _, err := os.Stat(filepath.Join(base, "alpha", "playlist.m3u8")); err != nil {
		t.Fatalf("media library damaged: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("parent directory damaged: %v", err)
	}
}

func TestStreamFileEscapedNames(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "My Video")
	makeVideoDir(t, base, "Видео")

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	for _, target := range []string{
		"/videos/My%20Video/playlist.m3u8",
		"/videos/%D0%92%D0%B8%D0%B4%D0%B5%D0%BE/playlist.m3u8",
		"/videos/My%20Video/segment0.ts",
	} {
		if resp := doRequest(t, app, http.MethodGet, target); resp.StatusCode != fiber.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, resp.StatusCode)
		}
	}
}

func TestStreamFileRejectsEncodedTraversal(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "media")
	makeVideoDir(t, base, "a")
	makeVideoDir(t, base, "ab")
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/videos/:videoname/*", streamFile(base))

	for _, target := range []string{
		"/videos/a/%2E%2E/ab/playlist.m3u8",
		"/videos/a/..%2F..%2Fsecret.txt",
		"/videos/a/%2E%2E%2F%2E%2E%2Fsecret.txt",
		"/videos/%2E%2E/secret.txt",
		"/videos/a/%zz",
	} {
		if resp := doRequest(t, app, http.MethodGet, target); resp.StatusCode == fiber.StatusOK {
			t.Errorf("%s: status 200, traversal must be rejected", target)
		}
	}
}

func TestDeleteVideoEscapedName(t *testing.T) {
	base := t.TempDir()
	makeVideoDir(t, base, "My Video")

	app := fiber.New()
	app.Delete("/videos/:videoname", deleteVideo(base, discard))

	if resp := doRequest(t, app, http.MethodDelete, "/videos/My%20Video"); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(base, "My Video")); !os.IsNotExist(err) {
		t.Errorf("video dir must be removed, stat err = %v", err)
	}
}

func TestDeleteVideoNotFound(t *testing.T) {
	app := fiber.New()
	app.Delete("/videos/:videoname", deleteVideo(t.TempDir(), discard))

	if resp := doRequest(t, app, http.MethodDelete, "/videos/missing"); resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
