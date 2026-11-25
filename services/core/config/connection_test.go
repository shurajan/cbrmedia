package config

import (
	"encoding/json"
	"testing"
)

func TestConnection(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		proxy   string // ожидаемый ProxyURL
		iface   string // ожидаемый InterfaceName
		wantErr bool
	}{
		"no section":            {body: `{}`},
		"direct":                {body: `{"connection":"direct"}`},
		"direct keeps address":  {body: `{"connection":" Direct ","proxy":"http://a:1"}`},
		"proxy":                 {body: `{"connection":" Proxy ","proxy":" http://a:1 "}`, proxy: "http://a:1"},
		"address without mode":  {body: `{"proxy":"http://a:1"}`},
		"proxy without address": {body: `{"connection":"proxy"}`, wantErr: true},
		"unknown connection":    {body: `{"connection":"socks"}`, wantErr: true},
		"bad address":           {body: `{"connection":"proxy","proxy":"nope"}`, wantErr: true},
		"bad address, direct":   {body: `{"connection":"direct","proxy":"nope"}`, wantErr: true},
		"interface":             {body: `{"connection":" Interface ","interface":" wg0 "}`, iface: "wg0"},
		"interface, proxy kept": {body: `{"connection":"interface","interface":"eth0","proxy":"http://a:1"}`, iface: "eth0"},
		"proxy, interface kept": {body: `{"connection":"proxy","interface":"eth0","proxy":"http://a:1"}`, proxy: "http://a:1"},
		"interface w/o name":    {body: `{"connection":"interface"}`, wantErr: true},
		"bad interface name":    {body: `{"connection":"interface","interface":"wg 0"}`, wantErr: true},
		"long interface name":   {body: `{"connection":"direct","interface":"abcdefghijklmnop"}`, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			var c Connection
			if err := json.Unmarshal([]byte(tc.body), &c); err != nil {
				t.Fatal(err)
			}
			c.Normalize()
			err := c.Validate("cdn")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (c.ProxyURL() != tc.proxy || c.InterfaceName() != tc.iface) {
				t.Fatalf("ProxyURL = %q, InterfaceName = %q, want %q, %q", c.ProxyURL(), c.InterfaceName(), tc.proxy, tc.iface)
			}
		})
	}
}

// TestSetValue: подключение одним значением — шаги выполняются подряд на одной секции.
func TestSetValue(t *testing.T) {
	var c Connection
	c.Normalize()
	for i, s := range []struct {
		set     string
		wantErr bool
		value   string
		want    Connection
	}{
		{set: "proxy", wantErr: true, value: "direct", want: Connection{Connection: "direct"}},
		{set: "interface", wantErr: true, value: "direct", want: Connection{Connection: "direct"}},
		{set: "wg 0", wantErr: true, value: "direct", want: Connection{Connection: "direct"}},
		{set: "http://", wantErr: true, value: "direct", want: Connection{Connection: "direct"}},
		{set: "http://a:1", value: "http://a:1", want: Connection{Connection: "proxy", Proxy: "http://a:1"}},
		{set: " wg0 ", value: "wg0", want: Connection{Connection: "interface", Proxy: "http://a:1", Interface: "wg0"}},
		{set: "Direct", value: "direct", want: Connection{Connection: "direct", Proxy: "http://a:1", Interface: "wg0"}},
		{set: "proxy", value: "http://a:1", want: Connection{Connection: "proxy", Proxy: "http://a:1", Interface: "wg0"}},
		{set: "interface", value: "wg0", want: Connection{Connection: "interface", Proxy: "http://a:1", Interface: "wg0"}},
		{set: "", value: "direct", want: Connection{Connection: "direct"}},
	} {
		if err := c.SetValue(s.set); (err != nil) != s.wantErr {
			t.Fatalf("step %d: SetValue(%q) = %v", i, s.set, err)
		}
		if c != s.want || c.Value() != s.value {
			t.Fatalf("step %d: after SetValue(%q): %+v, Value = %q", i, s.set, c, c.Value())
		}
		if err := c.Validate("cdn"); err != nil {
			t.Fatalf("step %d: Validate: %v", i, err)
		}
	}
}

func TestLegacyProxyError(t *testing.T) {
	if err := LegacyProxyError(" ", "cdn"); err != nil {
		t.Errorf("empty legacy key: %v", err)
	}
	if err := LegacyProxyError("http://a:1", "cdn"); err == nil {
		t.Error("expected error for non-empty legacy key")
	}
}

func TestMigrateLegacyProxy(t *testing.T) {
	for name, tc := range map[string]struct {
		legacy string
		conn   Connection
		want   Connection
		note   bool
	}{
		"empty legacy":       {legacy: " ", conn: Connection{Connection: "direct"}, want: Connection{Connection: "direct"}},
		"moved":              {legacy: " http://a:1 ", conn: Connection{Connection: "direct"}, want: Connection{Connection: "proxy", Proxy: "http://a:1"}, note: true},
		"section configured": {legacy: "http://a:1", conn: Connection{Connection: "direct", Proxy: "http://b:2"}, want: Connection{Connection: "direct", Proxy: "http://b:2"}, note: true},
	} {
		t.Run(name, func(t *testing.T) {
			legacy, conn := tc.legacy, tc.conn
			note := MigrateLegacyProxy(&legacy, &conn, "cdn")
			if legacy != "" || conn != tc.want || (note != "") != tc.note {
				t.Fatalf("legacy = %q, conn = %+v, note = %q", legacy, conn, note)
			}
		})
	}
}
