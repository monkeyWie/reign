package access

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestGrantSignatureExpiryAndScope(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	g := Grant{Audience: Audience, Namespace: "tenant-a", HostID: "host-a", DeviceID: "client-a", Role: "client", Scopes: []string{"connect"}, IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ID: "unique"}
	token, err := Sign(priv, g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(pub, token, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Allows("tenant-a", "host-a", "client-a", "connect") || got.Allows("tenant-b", "host-a", "client-a", "connect") || got.Allows("tenant-a", "host-b", "client-a", "connect") || got.Allows("tenant-a", "host-a", "client-a", "relay") {
		t.Fatal("grant escaped its scope")
	}
	if _, err = Verify(pub, token, now.Add(time.Minute)); err == nil {
		t.Fatal("expired grant admitted")
	}
	parts := strings.Split(token, ".")
	parts[0] = "e30"
	if _, err = Verify(pub, strings.Join(parts, "."), now); err == nil {
		t.Fatal("tampering admitted")
	}
	wrong, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = Verify(wrong, token, now); err == nil {
		t.Fatal("untrusted issuer admitted")
	}
}
