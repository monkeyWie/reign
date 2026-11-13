package httpapi

import (
	"context"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticationAndOrigin(t *testing.T) {
	s := httptest.NewServer(Handler("secret", &control.Dispatcher{Providers: map[string]provider.Provider{}}))
	defer s.Close()
	c := &Client{URL: s.URL, Key: "wrong"}
	if err := c.Call(context.Background(), control.Request{Method: "providers"}, nil); err == nil {
		t.Fatal("unauthorized accepted")
	}
	c.Key = "secret"
	if err := c.Call(context.Background(), control.Request{Method: "providers"}, nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", s.URL+"/rpc", strings.NewReader(`{"method":"providers"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "https://attacker.example")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("browser origin accepted")
	}
}
