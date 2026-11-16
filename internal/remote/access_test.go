package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/pkg/access"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignedLeaseEnforcedOnDirectRPC(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Unix()
	g := access.Grant{Audience: access.Audience, Namespace: "service", HostID: "host", DeviceID: "controller", Role: "client", Scopes: []string{"connect"}, Products: []string{"codex"}, IssuedAt: now, ExpiresAt: now + 180, ID: "lease"}
	ticket, _ := access.Sign(priv, g)
	w := &wire{opts: Options{GrantRequired: true, ServiceID: "service", GrantPublicKey: base64.RawURLEncoding.EncodeToString(pub)}, grant: ticket}
	if err := w.authorize("host", "controller", control.Request{Method: "send", Provider: "codex-app"}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []struct{ host, device, provider string }{{"host", "controller", "claude"}, {"another-host", "controller", "codex"}, {"host", "attacker", "codex"}} {
		if w.authorize(req.host, req.device, control.Request{Method: "send", Provider: req.provider}) == nil {
			t.Fatal("quota/identity bypass", req)
		}
	}
	g.Products = []string{}
	w.grant, _ = access.Sign(priv, g)
	if w.authorize("host", "controller", control.Request{Method: "send", Provider: "codex-cli"}) == nil {
		t.Fatal("empty product grant allowed execution")
	}
	w.grant = ticket + "broken"
	if w.authorize("host", "controller", control.Request{Method: "send", Provider: "codex"}) == nil {
		t.Fatal("forged grant accepted")
	}
	w.opts.GrantPublicKey = ""
	if w.authorize("host", "controller", control.Request{Method: "send"}) == nil {
		t.Fatal("missing trust key failed open")
	}
}

func TestExpiredConnectionResponseClosesClientWithoutReplaying(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "coded", true: "older-host"}[legacy], func(t *testing.T) {
			token := strings.Repeat("r", 32)
			server := httptest.NewServer(relay.New(token))
			defer server.Close()
			pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			d := &control.Dispatcher{Execute: func(req control.Request, _ func() control.Response) control.Response {
				calls.Add(1)
				response := authorizationFailure(req.ID, errConnectionAuthorization)
				if legacy {
					response.Code = ""
					response.Error = "远控授权已过期，请重新连接或登录"
				}
				return response
			}}
			monitor := NewHostStatus(pair.Relay)
			go RunHostObserved(ctx, pair, Options{RelayOnly: true}, d, monitor)
			waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
			c, err := Connect(ctx, pair, Options{RelayOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			err = c.Call(ctx, control.Request{Method: "send", Provider: "fixture", Session: "fixture"}, nil)
			if err == nil || strings.Contains(err.Error(), "登录") || !strings.Contains(err.Error(), "续期") {
				t.Fatal("misleading expiry error", err)
			}
			waitFor(t, func() bool { return c.Path() == "Disconnected" })
			if calls.Load() != 1 {
				t.Fatal("command replayed", calls.Load())
			}
		})
	}
}
