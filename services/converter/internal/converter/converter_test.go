package converter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeFFmpeg — скрипт вместо ffmpeg: последний аргумент — путь плейлиста.
// FAKE_FFMPEG=fail — ошибка, =partial — плейлист без #EXT-X-ENDLIST,
// =hang — висит до сигнала. Аргументы пишутся в args.txt рядом со скриптом.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	script := `#!/bin/sh
echo "$@" > "` + filepath.Join(dir, "args.txt") + `"
for last; do :; done
case "$FAKE_FFMPEG" in
fail) echo "Invalid data found when processing input" >&2; exit 1 ;;
partial) printf '#EXTM3U\n#EXTINF:6,\n00000.ts\n' > "$last"; exit 0 ;;
hang) exec sleep 30 ;;
esac
printf '#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\n00000.ts\n#EXT-X-ENDLIST\n' > "$last"
printf 'seg' > "$(dirname "$last")/00000.ts"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type env struct {
	conv       *Converter
	recordings string
	media      string
}

func newEnv(t *testing.T, ffmpeg string) env {
	t.Helper()
	root := t.TempDir()
	e := env{recordings: filepath.Join(root, "recordings"), media: filepath.Join(root, "media")}
	for _, d := range []string{e.recordings, e.media} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.conv = &Converter{Recordings: e.recordings, Media: e.media, FFmpeg: ffmpeg, Log: discard}
	return e
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

func TestPendingSelectsFinishedRecordings(t *testing.T) {
	e := newEnv(t, "")
	e.conv.MinAge = time.Minute
	now := time.Now()

	for _, name := range []string{
		"b_part1.ts", "a_part1.MP4", // готовые записи
		"c_part1.ts.download", // ещё пишется
		".hidden.ts", "notes.txt",
		"fresh.ts", // слишком свежий
	} {
		writeFile(t, filepath.Join(e.recordings, name), "x")
	}
	if err := os.MkdirAll(filepath.Join(e.recordings, "dir.ts"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-time.Hour)
	for _, name := range []string{"b_part1.ts", "a_part1.MP4", "c_part1.ts.download", ".hidden.ts", "notes.txt"} {
		if err := os.Chtimes(filepath.Join(e.recordings, name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	got, err := e.conv.pending(now)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i] = filepath.Base(got[i])
	}
	if fmt.Sprint(got) != "[a_part1.MP4 b_part1.ts]" {
		t.Errorf("pending = %v", got)
	}
}

func TestFFmpegArgs(t *testing.T) {
	ts := strings.Join(ffmpegArgs("/r/x.ts", "/m/tmp", 6), " ")
	for _, want := range []string{"-c copy", "-map 0:v? -map 0:a?", "-hls_time 6", "-hls_playlist_type vod",
		"-hls_segment_type mpegts", "-hls_segment_filename /m/tmp/%05d.ts", "/m/tmp/playlist.m3u8"} {
		if !strings.Contains(ts, want) {
			t.Errorf("ts args %q missing %q", ts, want)
		}
	}
	mp4 := strings.Join(ffmpegArgs("/r/x.mp4", "/m/tmp", 6), " ")
	for _, want := range []string{"-hls_segment_type fmp4", "-hls_fmp4_init_filename init.mp4", "/m/tmp/%05d.m4s"} {
		if !strings.Contains(mp4, want) {
			t.Errorf("mp4 args %q missing %q", mp4, want)
		}
	}
	if !strings.HasSuffix(ts, "playlist.m3u8") {
		t.Error("output playlist must be the last argument")
	}
}

func TestConvertPublishesAndRemovesSource(t *testing.T) {
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.recordings, "bob_2026-11-03_10-15_part1.ts"), "raw")

	if n := e.conv.RunOnce(context.Background()); n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	if got := list(t, e.recordings); got != "" {
		t.Errorf("recordings = %q, want empty", got)
	}
	if got := list(t, e.media); got != "bob_2026-11-03_10-15_part1/" {
		t.Fatalf("media = %q", got)
	}
	if got := list(t, filepath.Join(e.media, "bob_2026-11-03_10-15_part1")); got != "00000.ts playlist.m3u8" {
		t.Errorf("video dir = %q", got)
	}
}

func TestConvertDoesNotOverwriteExistingVideo(t *testing.T) {
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.media, "rec", "playlist.m3u8"), "old")
	writeFile(t, filepath.Join(e.recordings, "rec.ts"), "raw")

	e.conv.RunOnce(context.Background())

	if got := list(t, e.media); got != "rec/ rec_2/" {
		t.Fatalf("media = %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(e.media, "rec", "playlist.m3u8")); string(b) != "old" {
		t.Errorf("existing video overwritten: %q", b)
	}
}

func TestConvertFailureMovesSourceToFailed(t *testing.T) {
	for _, mode := range []string{"fail", "partial"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("FAKE_FFMPEG", mode)
			e := newEnv(t, fakeFFmpeg(t))
			writeFile(t, filepath.Join(e.recordings, "bad.ts"), "raw")
			writeFile(t, filepath.Join(e.recordings, FailedDir, "bad.ts"), "earlier failure")

			if n := e.conv.RunOnce(context.Background()); n != 0 {
				t.Fatalf("published = %d, want 0", n)
			}
			if got := list(t, e.media); got != "" {
				t.Errorf("media = %q, want empty (temp dir must be removed)", got)
			}
			if got := list(t, e.recordings); got != "failed/" {
				t.Errorf("recordings = %q", got)
			}
			if got := list(t, filepath.Join(e.recordings, FailedDir)); got != "bad.ts bad_2.ts" {
				t.Errorf("failed = %q", got)
			}
			// Повторный проход не трогает failed/.
			if n := e.conv.RunOnce(context.Background()); n != 0 {
				t.Errorf("second pass published %d", n)
			}
		})
	}
}

func TestConvertInterruptedKeepsSource(t *testing.T) {
	t.Setenv("FAKE_FFMPEG", "hang")
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.recordings, "rec.ts"), "raw")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	e.conv.RunOnce(ctx)

	if got := list(t, e.recordings); got != "rec.ts" {
		t.Errorf("recordings = %q, source must stay for the next run", got)
	}
	if got := list(t, e.media); got != "" {
		t.Errorf("media = %q, temp dir must be removed", got)
	}
}

func TestRunOnceRemovesStaleTempDirs(t *testing.T) {
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.media, tmpPrefix+"crashed", "00000.ts"), "x")
	writeFile(t, filepath.Join(e.media, "video", "playlist.m3u8"), "x")

	e.conv.RunOnce(context.Background())

	if got := list(t, e.media); got != "video/" {
		t.Errorf("media = %q", got)
	}
}

const finishedPlaylist = "#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\n00000.ts\n#EXT-X-ENDLIST\n"

func TestRunOncePublishesFinishedTempWithoutSource(t *testing.T) {
	// Служба упала между удалением исходника и публикацией: готовое видео не теряется.
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.media, tmpPrefix+"rec", playlistName), finishedPlaylist)
	writeFile(t, filepath.Join(e.media, tmpPrefix+"rec", "00000.ts"), "seg")

	if n := e.conv.RunOnce(context.Background()); n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	if got := list(t, e.media); got != "rec/" {
		t.Errorf("media = %q", got)
	}
}

func TestRunOnceDropsFinishedTempWhenSourceRemains(t *testing.T) {
	// Служба упала после проверки, но до удаления исходника: временная папка
	// удаляется, запись преобразуется заново — без дубликата rec_2.
	e := newEnv(t, fakeFFmpeg(t))
	writeFile(t, filepath.Join(e.media, tmpPrefix+"rec", playlistName), finishedPlaylist)
	writeFile(t, filepath.Join(e.recordings, "rec.ts"), "raw")

	if n := e.conv.RunOnce(context.Background()); n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	if got := list(t, e.media); got != "rec/" {
		t.Errorf("media = %q, want a single video", got)
	}
	if got := list(t, e.recordings); got != "" {
		t.Errorf("recordings = %q, want empty", got)
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{limit: 5}
	_, _ = b.Write([]byte("hello "))
	_, _ = b.Write([]byte("world"))
	if b.String() != "world" {
		t.Errorf("tail = %q", b.String())
	}
}

// TestConvertWithRealFFmpeg проверяет нарезку настоящим ffmpeg на записях того
// же вида, что пишет рекордер: MPEG-TS и фрагментированный MP4.
func TestConvertWithRealFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	e := newEnv(t, ffmpeg)
	e.conv.SegmentSec = 2
	gen := func(name string, extra ...string) {
		args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=25",
			"-f", "lavfi", "-i", "sine", "-t", "7", "-c:v", "libx264", "-g", "25", "-c:a", "aac"}
		args = append(append(args, extra...), filepath.Join(e.recordings, name))
		if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Skipf("can't generate sample (no libx264/aac?): %v: %s", err, out)
		}
	}
	gen("live_part1.ts", "-f", "mpegts")
	gen("live_part2.mp4", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4")

	if n := e.conv.RunOnce(context.Background()); n != 2 {
		t.Fatalf("published = %d, want 2 (recordings: %s)", n, list(t, e.recordings))
	}

	check := func(video, segExt string, wantInit bool) {
		dir := filepath.Join(e.media, video)
		pl, err := os.ReadFile(filepath.Join(dir, "playlist.m3u8"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(pl)
		if !strings.Contains(text, "#EXT-X-ENDLIST") || !strings.Contains(text, "#EXT-X-PLAYLIST-TYPE:VOD") {
			t.Errorf("%s: playlist not a finished VOD:\n%s", video, text)
		}
		if got := strings.Contains(text, `#EXT-X-MAP:URI="init.mp4"`); got != wantInit {
			t.Errorf("%s: init map present = %v, want %v", video, got, wantInit)
		}
		segments := 0
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, "/") || filepath.Ext(line) != segExt {
				t.Errorf("%s: unexpected segment URI %q", video, line)
			}
			if _, err := os.Stat(filepath.Join(dir, line)); err != nil {
				t.Errorf("%s: segment missing: %v", video, err)
			}
			segments++
		}
		if segments < 2 {
			t.Errorf("%s: %d segments, want several", video, segments)
		}
		if wantInit {
			if _, err := os.Stat(filepath.Join(dir, "init.mp4")); err != nil {
				t.Errorf("%s: init.mp4 missing: %v", video, err)
			}
		}
	}
	check("live_part1", ".ts", false)
	check("live_part2", ".m4s", true)

	if got := list(t, e.recordings); got != "" {
		t.Errorf("recordings = %q, sources must be removed", got)
	}
}
