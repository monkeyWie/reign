package device

import (
	"encoding/base64"
	"testing"
)

func TestPairingEnvelopeAuthenticatesPeerAndRequest(t *testing.T) {
	a, _ := NewIdentity("controller")
	b, _ := NewIdentity("host")
	c, _ := NewIdentity("outsider")
	packet, e := Seal(a.Private, b.Public, "request/1", map[string]string{"password": "private password"})
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]string
	if e = Open(b.Private, a.Public, "request/1", packet, &out); e != nil || out["password"] != "private password" {
		t.Fatal(e)
	}
	for _, params := range [][3]string{{b.Private, a.Public, "request/2"}, {b.Private, c.Public, "request/1"}, {c.Private, a.Public, "request/1"}} {
		if e = Open(params[0], params[1], params[2], packet, &out); e == nil {
			t.Fatal("wrong context/peer accepted")
		}
	}
	raw, _ := base64.StdEncoding.DecodeString(packet)
	raw[len(raw)-1] ^= 1
	if e = Open(b.Private, a.Public, "request/1", base64.StdEncoding.EncodeToString(raw), &out); e == nil {
		t.Fatal("tampered envelope accepted")
	}
}
