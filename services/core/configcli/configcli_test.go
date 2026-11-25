package configcli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreconfig "cbr/core/config"
	"cbr/core/netdial"
)

// testConfig — конфиг с параметрами всех видов.
type testConfig struct {
	URL   string                `json:"url"`
	Token string                `json:"token"`
	ID    int64                 `json:"id"`
	Note  string                `json:"note"`
	CDN   coreconfig.Connection `json:"cdn"`
}

func (c *testConfig) Normalize() { c.CDN.Normalize() }

func (c *testConfig) Validate() error {
	if c.URL == "" {
		return errors.New("url is required")
	}
	return c.CDN.Validate("cdn")
}

func newCommand(t *testing.T) (*Command, *testConfig) {
	t.Helper()
	cfg := &testConfig{}
	return &Command{
		Service: "svc",
		Path:    filepath.Join(t.TempDir(), "config", "svc.json"),
		Config:  cfg,
		Params: []Param{
			URL("url", "url", "base URL", true, &cfg.URL),
			Secret("token", "token", "<token>", "secret token", false, &cfg.Token),
			Int64("id", "id", "<id>", "numeric id", false, &cfg.ID),
			String("note", "note", "<text>", "free text", false, &cfg.Note),
			Connection("cdn", "cdn", "connection to the CDN", &cfg.CDN),
		},
		Examples: []string{"config --url https://example.com"},
	}, cfg
}

// run выполняет команду на свежем конфиге (как новый запуск бинарника) и читает файл.
func run(t *testing.T, path, stdin string, args ...string) (code int, out, errOut, file string) {
	t.Helper()
	cmd, _ := newCommand(t)
	cmd.Path = path
	var stdout, stderr bytes.Buffer
	code = cmd.Run(args, strings.NewReader(stdin), &stdout, &stderr)
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return code, stdout.String(), stderr.String(), string(raw)
}

func TestFlagsWriteAndMerge(t *testing.T) {
	cmd, _ := newCommand(t)
	path := cmd.Path

	code, out, errOut, file := run(t, path, "", "--url", "https://example.com/a?x=1&y=2", "--token", "s3cret", "--id", "42", "--cdn", "http://proxy:1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(file, `"url": "https://example.com/a?x=1&y=2"`) || !strings.Contains(file, `"proxy": "http://proxy:1"`) {
		t.Fatalf("file = %s", file)
	}
	if strings.Contains(out, "s3cret") {
		t.Errorf("secret is printed: %s", out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, err = %v", info.Mode().Perm(), err)
	}

	// Второй запуск с одним флагом: остальное остаётся как в файле, вопросов нет.
	code, out, errOut, file = run(t, path, "", "--note", "hello")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{`"note": "hello"`, `"token": "s3cret"`, `"id": 42`, `"connection": "proxy"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file has no %s: %s", want, file)
		}
	}
	if strings.Contains(out, "]: ") {
		t.Errorf("flags mode must not ask: %s", out)
	}

	// Пустое значение флага очищает необязательный параметр.
	if _, _, _, file = run(t, path, "", "--note", ""); strings.Contains(file, "hello") {
		t.Errorf("note is not cleared: %s", file)
	}
}

func TestFlagsErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		code int
	}{
		"unknown flag":       {[]string{"--bogus", "1"}, 2},
		"positional":         {[]string{"extra"}, 2},
		"bad value":          {[]string{"--url", "https://example.com", "--id", "abc"}, 2},
		"empty required":     {[]string{"--url", ""}, 2},
		"proxy without addr": {[]string{"--url", "https://example.com", "--cdn", "proxy"}, 2},
		"invalid config":     {[]string{"--note", "no url given"}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := newCommand(t)
			code, _, errOut, file := run(t, cmd.Path, "", tc.args...)
			if code != tc.code || errOut == "" {
				t.Errorf("exit %d, want %d; stderr %q", code, tc.code, errOut)
			}
			if file != "" {
				t.Errorf("file must not be written: %s", file)
			}
		})
	}
}

func TestAsk(t *testing.T) {
	cmd, _ := newCommand(t)
	path := cmd.Path

	// url: пусто (обязателен — переспрос), не URL (переспрос), верный; token; id: не число, число;
	// note; cdn: адрес.
	answers := "\nnope\nhttps://example.com\ns3cr3t\nabc\n7\nfirst note\nhttp://proxy:1\n"
	code, out, errOut, file := run(t, path, answers)
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut, out)
	}
	if strings.Count(out, "❌") != 3 {
		t.Errorf("want 3 rejected answers: %s", out)
	}
	for _, want := range []string{`"url": "https://example.com"`, `"token": "s3cr3t"`, `"id": 7`, `"note": "first note"`, `"proxy": "http://proxy:1"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file has no %s: %s", want, file)
		}
	}

	// Enter оставляет значение, "-" очищает необязательный, direct не стирает адрес прокси.
	code, out, errOut, file = run(t, path, "\n\n\n-\ndirect\n")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "[https://example.com]") || !strings.Contains(out, "[set, hidden]") || strings.Contains(out, "s3cr3t") {
		t.Errorf("prompts must show current values and hide the secret: %s", out)
	}
	for _, want := range []string{`"token": "s3cr3t"`, `"id": 7`, `"note": ""`, `"connection": "direct"`, `"proxy": "http://proxy:1"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file has no %s: %s", want, file)
		}
	}

	// Ввод оборвался — файл не тронут.
	before := file
	code, _, errOut, file = run(t, path, "https://other.example.com\n")
	if code != 1 || errOut == "" || file != before {
		t.Errorf("exit %d, stderr %q, file changed: %v", code, errOut, file != before)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		cmd, _ := newCommand(t)
		code, out, _, file := run(t, cmd.Path, "", args...)
		if code != 0 || file != "" {
			t.Fatalf("%v: exit %d, file %q", args, code, file)
		}
		for _, p := range cmd.Params {
			if !strings.Contains(out, "--"+p.Flag) || !strings.Contains(out, p.Usage) {
				t.Errorf("%v: help has no --%s: %s", args, p.Flag, out)
			}
		}
		if !strings.Contains(out, cmd.Path) || !strings.Contains(out, "svc config --url https://example.com") {
			t.Errorf("%v: help has no path or example: %s", args, out)
		}
	}
}

// TestConnectionParam: разбор значения — coreconfig.Connection.SetValue; здесь —
// что интерфейс должен существовать и что неудачный ответ секцию не меняет.
func TestConnectionParam(t *testing.T) {
	c := coreconfig.Connection{Connection: coreconfig.ConnectionProxy, Proxy: "http://a:1"}
	p := Connection("cdn", "cdn", "", &c)
	for _, bad := range []string{"nosuchif0", "http://", "interface"} {
		if err := p.Set(bad); err == nil {
			t.Fatalf("Set(%q): expected error", bad)
		}
		if p.Get() != "http://a:1" {
			t.Fatalf("Set(%q) changed the section: %+v", bad, c)
		}
	}
	loopback := netdial.LoopbackInterface()
	if loopback == "" {
		t.Skip("no loopback interface")
	}
	if err := p.Set(loopback); err != nil {
		t.Fatal(err)
	}
	if p.Get() != loopback || c.InterfaceName() != loopback || c.Proxy != "http://a:1" {
		t.Fatalf("section = %+v", c)
	}
}

func TestMigrateNotes(t *testing.T) {
	cmd, cfg := newCommand(t)
	cmd.Migrate = func() []string { cfg.Note = "migrated"; return []string{"old key moved"} }
	var stdout, stderr bytes.Buffer
	if code := cmd.Run([]string{"--url", "https://example.com"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(cmd.Path)
	if err != nil || !strings.Contains(string(raw), `"note": "migrated"`) || !strings.Contains(stdout.String(), "old key moved") {
		t.Fatalf("file = %s, out = %s, err = %v", raw, stdout.String(), err)
	}
}
