package netdial

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	coreconfig "cbr/core/config"
)

func client(t *testing.T, c coreconfig.Connection) (*http.Client, error) {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	if err := Configure(transport, &net.Dialer{}, c); err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport}, nil
}

// TestConfigureInterface: сокет, привязанный к loopback-интерфейсу, достаёт локальный сервер.
func TestConfigureInterface(t *testing.T) {
	loopback := LoopbackInterface()
	if loopback == "" || !bindSupported {
		t.Skip("no loopback interface or binding is not supported on this OS")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	conn := coreconfig.Connection{Connection: coreconfig.ConnectionInterface, Interface: loopback}
	c, err := client(t, conn)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("request through %s: %v", loopback, err)
	}
	_ = resp.Body.Close()
	if kind, target := Describe(conn); kind != coreconfig.ConnectionInterface || target != loopback {
		t.Errorf("Describe = %q, %q", kind, target)
	}
}

// TestConfigureMissingInterface: нет интерфейса — ошибка сразу, а не маршрут по умолчанию.
func TestConfigureMissingInterface(t *testing.T) {
	_, err := client(t, coreconfig.Connection{Connection: coreconfig.ConnectionInterface, Interface: "nosuchif0"})
	if err == nil {
		t.Fatal("expected error for a missing interface")
	}
}

// TestConfigureProxyAndDirect: прокси получает запрос только при connection "proxy";
// сохранённые, но не выбранные адрес и интерфейс не действуют.
func TestConfigureProxyAndDirect(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxied.Add(1) }))
	defer proxy.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	for _, tc := range []struct {
		conn coreconfig.Connection
		want int32
	}{
		{coreconfig.Connection{Connection: coreconfig.ConnectionDirect, Proxy: proxy.URL, Interface: "nosuchif0"}, 0},
		{coreconfig.Connection{Connection: coreconfig.ConnectionProxy, Proxy: proxy.URL, Interface: "nosuchif0"}, 1},
	} {
		proxied.Store(0)
		c, err := client(t, tc.conn)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if proxied.Load() != tc.want {
			t.Errorf("%+v: proxied = %d, want %d", tc.conn, proxied.Load(), tc.want)
		}
	}
}
