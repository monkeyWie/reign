//go:build !js

package browserclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type delayedPeer struct{ client *Client }

func (*delayedPeer) Offer() error        { return nil }
func (*delayedPeer) Signal(Object) error { return nil }
func (*delayedPeer) Direct() bool        { return true }
func (*delayedPeer) Close()              {}
func (p *delayedPeer) Send([]byte) error {
	p.client.mu.Lock()
	var request string
	for id := range p.client.pending {
		request = id
	}
	p.client.mu.Unlock()
	go func() {
		time.Sleep(3 * time.Minute)
		raw, _ := json.Marshal(Object{"id": request, "result": "large snapshot"})
		p.client.response(raw)
	}()
	return nil
}

func TestBusinessReadHasNoTotalTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		crypto, _ := newCryptor(strings.Repeat("ab", 32))
		c := &Client{ctx: ctx, cancel: cancel, crypto: crypto, pending: map[string]*pendingCall{}, cache: map[string]cached{}, chunks: map[string]*assembly{}}
		c.pc = &delayedPeer{client: c}
		defer c.Close()
		value, err := c.Call(ctx, Object{"method": "transcript"})
		if err != nil || value != "large snapshot" {
			t.Fatalf("slow business read failed: %v, %v", value, err)
		}
	})
}
