package device

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceURLCleartextPolicy(t *testing.T) {
	previous := AllowLANHTTP
	t.Cleanup(func() { AllowLANHTTP = previous })
	for _, enabled := range []string{"false", "true"} {
		AllowLANHTTP = enabled
		for _, tc := range []struct {
			url string
			ok  bool
		}{
			{"http://127.0.0.1:7443", true},
			{"http://localhost:7443", true},
			{"http://[::1]:7443", true},
			{"http://10.0.4.191:7443", true},
			{"http://172.16.0.1:7443", true},
			{"http://192.168.1.2:7443", true},
			{"http://172.32.0.1:7443", true},
			{"http://100.64.0.1:7443", true},
			{"http://169.254.1.1:7443", true},
			{"http://0.0.0.0:7443", true},
			{"http://example.com:7443", true},
			{"http://8.8.8.8:7443", true},
			{"https://example.com", true},
			{"https://user:pass@example.com", false},
			{"https://example.com/path", false},
			{"ftp://example.com", false},
			{"http://user:password@example.com", false},
			{"http://example.com/path", false},
		} {
			_, err := ServiceURL(tc.url)
			if (err == nil) != tc.ok {
				t.Errorf("LAN=%s URL=%s: err=%v, want accepted=%v", enabled, tc.url, err, tc.ok)
			}
		}
	}
}

func TestHostNetworkRequiresTheServiceTrustIdentity(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	valid := base64.RawURLEncoding.EncodeToString(pub)
	for _, tc := range []struct {
		name, key, service string
		ok                 bool
	}{
		{"valid", valid, "service", true},
		{"missing-key", "", "service", false},
		{"short-key", "YWJj", "service", false},
		{"invalid-key", "not!base64", "service", false},
		{"missing-service", valid, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"serviceId": tc.service, "grantPublicKey": tc.key})
			}))
			defer server.Close()
			policy, err := HostNetwork(context.Background(), server.URL)
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v error=%v", tc.ok, err)
			}
			if tc.ok && (policy.GrantPublicKey != valid || policy.ServiceID != "service") {
				t.Fatal("identity changed")
			}
		})
	}
}
