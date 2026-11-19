package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/monkeyWie/reignhub/internal/agents"
	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/codex"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/host"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/internal/syncstate"
)

// Embedded runs the host in the caller's process. Calls go straight to the
// dispatcher; there is no helper executable or local HTTP control listener.
type Embedded struct {
	Dispatcher *control.Dispatcher
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	db         *store.DB
	closers    []func() error
	calls      sync.RWMutex
	closed     bool
	closeOnce  sync.Once
	closeErr   error
}

func StartEmbedded(ctx context.Context, c config.Config) (*Embedded, error) {
	key, err := config.AccessKey(c.DataDir)
	if err != nil {
		return nil, err
	}
	attachments := assets.New(filepath.Join(c.DataDir, "attachments"))
	backend := codex.New(c, key, events.New(), attachments)
	providers := map[string]provider.Provider{"codex-app": backend}
	closers := []func() error{backend.Close}
	for id, def := range agents.Catalog(c.Agents) {
		if err := agents.Validate(id, def); err != nil {
			log.Printf("Agent %s: %v", id, err)
			continue
		}
		p := agents.New(ctx, id, def, c.DataDir, attachments)
		providers[id] = p
		closers = append(closers, p.Close)
	}
	r, err := startEmbedded(ctx, c, providers, attachments)
	if err != nil {
		for _, close := range closers {
			_ = close()
		}
		return nil, err
	}
	r.closers = closers
	return r, nil
}

func startEmbedded(ctx context.Context, c config.Config, providers map[string]provider.Provider, stores ...*assets.Store) (*Embedded, error) {
	db, err := store.Open(filepath.Join(c.DataDir, "host.db"))
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	attachments := assets.New(filepath.Join(c.DataDir, "attachments"))
	if len(stores) > 0 {
		attachments = stores[0]
	}
	d := &control.Dispatcher{Providers: providers, Execute: db.Execute, Assets: attachments}
	index := &syncstate.Index{DB: db, Epoch: device.Random()[:16]}
	d.Sync = func(ctx context.Context, p provider.Provider, req control.Request) (any, error) {
		return index.Sync(ctx, p, req.Provider, req.Epoch, req.After)
	}
	manager := host.New(c, d, ICE)
	d.Local = manager.Local
	d.Network = manager.Network
	r := &Embedded{Dispatcher: d, ctx: life, cancel: cancel, done: make(chan struct{}), db: db}
	go func() { defer close(r.done); manager.Run(life) }()
	return r, nil
}

func (r *Embedded) Call(ctx context.Context, req control.Request, out any) error {
	r.calls.RLock()
	defer r.calls.RUnlock()
	if r.closed || r.ctx.Err() != nil {
		return fmt.Errorf("ReignHub 已退出")
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	if req.ID == "" {
		req.ID = "local/" + device.Random()
	}
	response := r.Dispatcher.HandleRequest(callCtx, req)
	if response.Error != "" {
		return &control.ResponseError{Code: response.Code, Message: response.Error}
	}
	if out != nil {
		return json.Unmarshal(response.Result, out)
	}
	return nil
}

func (r *Embedded) Path() string { return "In-process" }

func (r *Embedded) Close() error {
	r.closeOnce.Do(func() {
		r.cancel()
		r.calls.Lock()
		defer r.calls.Unlock()
		r.closed = true
		<-r.done
		for _, close := range r.closers {
			r.closeErr = errors.Join(r.closeErr, close())
		}
		r.closeErr = errors.Join(r.closeErr, r.db.Close())
	})
	return r.closeErr
}
