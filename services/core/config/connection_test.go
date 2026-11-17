package config

import (
	"encoding/json"
	"testing"
)

func TestConnection(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		proxy   string // ожидаемый ProxyURL
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
			if err == nil && c.ProxyURL() != tc.proxy {
				t.Fatalf("ProxyURL = %q, want %q", c.ProxyURL(), tc.proxy)
			}
		})
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
