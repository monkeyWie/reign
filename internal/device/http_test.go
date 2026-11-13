package device

import "testing"

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
