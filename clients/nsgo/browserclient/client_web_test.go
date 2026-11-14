//go:build js && wasm

package browserclient

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

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
	if c.dataWS != nil || !strings.HasPrefix(c.Path(), "P2P") {
		t.Fatal("free connection did not select direct-only path", c.Path())
	}
	if _, err := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"}); err != nil {
		t.Fatal(err)
	}
}
