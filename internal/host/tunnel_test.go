package host

import (
	"context"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/device"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestManagedAndSelfHostedTunnelConfig(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		status             int
		wantError, managed bool
	}{
		{"self-hosted", "", 404, false, false},
		{"managed", `{"url":"https://device.example","token":"connector-secret","listen":"127.0.0.1:26332"}`, 200, false, true},
		{"rejected", "", 403, true, false},
		{"invalid-listener", `{"url":"https://device.example","token":"secret","listen":"0.0.0.0:26332"}`, 200, true, false},
		{"invalid-origin", `{"url":"http://device.example","token":"secret","listen":"127.0.0.1:26332"}`, 200, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cloud/tunnel" || r.Header.Get("Authorization") != "Bearer device-token" {
					t.Error("incorrect provisioning credentials")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer service.Close()
			listen, c, err := tunnelConfig(context.Background(), config.Config{DataDir: t.TempDir()}, device.Identity{Service: service.URL, Token: "device-token"})
			if (err != nil) != tc.wantError {
				t.Fatal("unexpected error", err)
			}
			if tc.managed {
				if listen != "127.0.0.1:26332" || c.PublicURL != "https://device.example" {
					t.Fatal("bad configuration")
				}
				b, e := os.ReadFile(c.TokenFile)
				if e != nil || string(b) != "connector-secret" {
					t.Fatal("connector token missing", e)
				}
			}
		})
	}
}
