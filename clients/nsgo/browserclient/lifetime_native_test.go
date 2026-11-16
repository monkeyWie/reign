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

type delayedPeer struct {
	client   *Client
	progress bool
}

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
	if request == "" {
		return nil
	}
	go func() {
		for range 9 {
			select {
			case <-p.client.ctx.Done():
				return
			case <-time.After(20 * time.Second):
			}
			if p.progress {
				p.client.mu.Lock()
				pending := p.client.pending[request]
				if pending != nil {
					pending.idle.Touch()
				}
				p.client.mu.Unlock()
			}
		}
		raw, _ := json.Marshal(Object{"id": request, "result": "large snapshot"})
		p.client.response(raw)
	}()
	return nil
}

func TestBusinessReadContinuesWhileItsOwnFragmentsProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		crypto, _ := newCryptor(strings.Repeat("ab", 32))
		c := &Client{ctx: ctx, cancel: cancel, crypto: crypto, pending: map[string]*pendingCall{}, cache: map[string]cached{}, chunks: map[string]*assembly{}}
		c.pc = &delayedPeer{client: c, progress: true}
		defer c.Close()
		value, err := c.Call(ctx, Object{"method": "transcript"})
		if err != nil || value != "large snapshot" {
			t.Fatalf("slow business read failed: %v, %v", value, err)
		}
	})
}

func TestSilentBusinessReadTimesOutWithoutClosingConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		crypto, _ := newCryptor(strings.Repeat("ab", 32))
		c := &Client{ctx: ctx, cancel: cancel, crypto: crypto, pending: map[string]*pendingCall{}, cache: map[string]cached{}, chunks: map[string]*assembly{}}
		c.pc = &delayedPeer{client: c}
		defer c.Close()
		started := time.Now()
		_, err := c.Call(ctx, Object{"method": "transcript"})
		if err == nil || !strings.Contains(err.Error(), "30 秒") || time.Since(started) != 30*time.Second {
			t.Fatal("silent read was not bounded", err, time.Since(started))
		}
		if c.ctx.Err() != nil {
			t.Fatal("a stalled read closed the shared connection")
		}
	})
}
