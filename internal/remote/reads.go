package remote

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type hostRead struct {
	stop  context.CancelFunc
	watch string
}
type hostReads struct {
	mu      sync.Mutex
	pending map[string]*hostRead
	watches map[string]string
}

func (r *hostReads) add(req control.Request, stop context.CancelFunc) (*hostRead, bool) {
	if !control.CancellableRead(req.Method) {
		return nil, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = map[string]*hostRead{}
		r.watches = map[string]string{}
	}
	if r.pending[req.ID] != nil {
		return nil, false
	}
	v := &hostRead{stop: stop}
	if req.Method == "watch" {
		v.watch = req.Provider + "\x00" + req.Session
		oldID := r.watches[v.watch]
		if old := r.pending[oldID]; old != nil {
			old.stop()
			delete(r.pending, oldID)
		}
	}
	if len(r.pending) >= 32 {
		return nil, false
	}
	if v.watch != "" {
		r.watches[v.watch] = req.ID
	}
	r.pending[req.ID] = v
	return v, true
}
func (r *hostReads) remove(id string, v *hostRead) {
	if v == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[id] == v {
		delete(r.pending, id)
	}
	if v.watch != "" && r.watches[v.watch] == id {
		delete(r.watches, v.watch)
	}
}
func (r *hostReads) cancel(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.pending[id]; v != nil {
		v.stop()
	}
}

func (w *wire) discardResponse(request string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, a := range w.chunks {
		if a.request == request {
			for _, part := range a.parts {
				w.chunkBytes -= len(part)
			}
			delete(w.chunks, id)
		}
	}
}

func (c *Client) cancelRead(r control.Request) {
	if !control.CancellableRead(r.Method) || c.wire.ctx.Err() != nil {
		return
	}
	go func() {
		ctx, stop := context.WithTimeout(c.wire.ctx, 3*time.Second)
		defer stop()
		cancelID := relay.ID()
		raw, _ := json.Marshal(control.Request{ID: cancelID, Method: control.CancelRead, Cancel: r.ID, Provider: r.Provider, Session: r.Session})
		_ = c.wire.sendContext(ctx, cancelID, raw)
	}()
}
