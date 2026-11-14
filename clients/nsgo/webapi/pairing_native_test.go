//go:build !js

package webapi

import (
	"github.com/monkeyWie/reignhub/internal/device"
	"testing"
)

func TestPairingEnvelopesWithExistingHost(t *testing.T) {
	client, _ := device.NewIdentity("browser")
	host, _ := device.NewIdentity("host")
	identity := Object{"private": client.Private}
	context := "reignhub/pair/request/fixture"
	packet, e := seal(identity, host.Public, context, Object{"password": "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	var v Object
	if e = device.Open(host.Private, client.Public, context, packet, &v); e != nil || v["password"] != "fixture" {
		t.Fatal("host cannot read browser envelope", e)
	}
	packet, e = device.Seal(host.Private, client.Public, "reply", Object{"host": "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	v, e = openEnvelope(identity, host.Public, "reply", packet)
	if e != nil || v["host"] != "fixture" {
		t.Fatal(e)
	}
	if _, e = openEnvelope(identity, host.Public, "other-request", packet); e == nil {
		t.Fatal("accepted wrong request context")
	}
}
