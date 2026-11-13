package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/monkeyWie/reign/internal/assets"
	"github.com/monkeyWie/reign/internal/codex"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/events"
	"github.com/monkeyWie/reign/internal/host"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/store"
	"github.com/monkeyWie/reign/internal/syncstate"
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
	cli := codex.NewCLI(c, events.New(), attachments)
	if c.ManagedAgents["codex-app"] {
		if err := backend.Start(ctx); err != nil {
			log.Printf("Codex provider: %v", err)
		}
	}
	r, err := startEmbedded(ctx, c, map[string]provider.Provider{"codex-app": backend, "codex-cli": cli}, attachments)
	if err != nil {
		_ = backend.Close()
		_ = cli.Close()
		return nil, err
	}
	r.closers = []func() error{backend.Close, cli.Close}
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
		return fmt.Errorf("Agent Hub 已退出")
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	response := r.Dispatcher.Handle(callCtx, req)
	if response.Error != "" {
		return errors.New(response.Error)
	}
	if out != nil {
		return json.Unmarshal(response.Result, out)
	}
	return nil
}

func (r *Embedded) Path() string { return "FFI" }

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
