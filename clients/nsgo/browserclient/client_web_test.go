//go:build js && wasm

package browserclient

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func browserConnectLatency(t *testing.T) {
	url := os.Getenv("AGENTHUB_NSGO_TEST_FIXTURE")
	if url == "" {
		t.Skip("requires isolated browser fixture")
	}
	r, err := http.Get(url + "/fixture-slow")
	if err != nil {
		t.Fatal(err)
	}
	var p Pairing
	err = json.NewDecoder(r.Body).Decode(&p)
	r.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	setup, stop := context.WithTimeout(context.Background(), 4*time.Second)
	c, err := Connect(setup, p)
	stop()
	if err != nil {
		t.Fatal("optional services blocked direct connection:", err)
	}
	defer c.Close()
	if c.Path() != "P2P · WebRTC" || c.relayAvailable() {
		t.Fatal("stalled relay was required before establishing P2P", c.Path())
	}
	deadline := time.After(8 * time.Second)
	for !c.relayAvailable() {
		select {
		case <-deadline:
			t.Fatal("ending the pairing RPC canceled the background relay")
		case <-time.After(20 * time.Millisecond):
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Call(ctx, Object{"method": "network"}); err != nil {
		t.Fatal("verified data path unavailable after background relay attached:", err)
	}
}

func browserInterop(t *testing.T) {
	url := os.Getenv("AGENTHUB_NSGO_TEST_FIXTURE")
	if url == "" {
		t.Skip("run testsupport/fixture and pass AGENTHUB_NSGO_TEST_FIXTURE")
	}
	r, e := http.Get(url + "/fixture")
	if e != nil {
		t.Fatal(e)
	}
	var p Pairing
	e = json.NewDecoder(r.Body).Decode(&p)
	r.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, relayOnly := range []bool{true, false} {
		name := "P2P"
		if relayOnly {
			name = "Relay"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			p.RelayOnly = relayOnly
			c, e := Connect(ctx, p)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			if !relayOnly {
				for !strings.HasPrefix(c.Path(), "P2P") {
					select {
					case <-ctx.Done():
						t.Fatal("browser never established P2P", c.Path())
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			for range 2 {
				v, e := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"})
				if e != nil {
					t.Fatal(e)
				}
				rows := v.(map[string]any)["sessions"].([]any)
				if len(rows) != 1 || len(rows[0].(map[string]any)["preview"].(string)) < 100000 {
					t.Fatal("lost chunked response")
				}
			}
		})
	}
}

func browserDirectInterop(t *testing.T) {
	url := os.Getenv("AGENTHUB_NSGO_TEST_FIXTURE")
	if url == "" {
		t.Skip("requires isolated browser fixture")
	}
	r, err := http.Get(url + "/fixture-direct")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var p Pairing
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c, err := Connect(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.dataWS.Load() != nil || !strings.HasPrefix(c.Path(), "P2P") {
		t.Fatal("free connection did not select direct-only path", c.Path())
	}
	// Simulate browser connection-state events without changing the underlying
	// local fixture sockets. Reconnection must restore the same open DataChannel.
	pconn := c.pc.(*jsPeer)
	original := c.id
	js.Global().Get("Object").Call("defineProperty", pconn.pc, "connectionState", map[string]any{"value": "disconnected", "configurable": true})
	pconn.pc.Call("dispatchEvent", js.Global().Get("Event").New("connectionstatechange"))
	time.Sleep(1200 * time.Millisecond)
	if c.ctx.Err() != nil {
		t.Fatal("temporary ICE interruption destroyed peer")
	}
	js.Global().Get("Object").Call("defineProperty", pconn.pc, "connectionState", map[string]any{"value": "connected", "configurable": true})
	pconn.pc.Call("dispatchEvent", js.Global().Get("Event").New("connectionstatechange"))
	js.Global().Get("Reflect").Call("deleteProperty", pconn.pc, "connectionState")
	if err := c.Resume(ctx); err != nil || c.id != original || !strings.HasPrefix(c.Path(), "P2P") {
		t.Fatalf("same-peer recovery failed: %s, %v", c.Path(), err)
	}
	if _, err := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"}); err != nil {
		t.Fatal(err)
	}
	// Losing the coordination socket must not close an already authenticated
	// DataChannel, including when the host receives the relay's peer-bye frame.
	if err := closeSocket(c.ws); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !c.signalLost.Load() {
		if time.Now().After(deadline) {
			t.Fatal("signal closure not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if !strings.HasPrefix(c.Path(), "P2P") {
		t.Fatal("signal closure destroyed browser P2P", c.Path())
	}
	if _, err := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"}); err != nil {
		t.Fatal("direct read after signaling loss", err)
	}
}
