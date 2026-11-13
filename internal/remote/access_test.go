package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/pkg/access"
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
