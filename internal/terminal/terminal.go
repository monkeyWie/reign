// Package terminal owns conversation terminals independently of views and peers.
// Platform engines are supplied by the desktop application composition.
package terminal

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/device"
)

const (
	MaxTerminals = 16
	maxRetired   = 4096
	MaxInput     = 32 << 10
	MaxFrame     = 4 << 20
	// NSGo's input writer is asynchronous. A lifetime budget also bounds its
	// outstanding input if a child permanently stops reading its PTY.
	MaxSessionInput = 16 << 20
)

type Size struct {
	Cols       int `json:"cols"`
	Rows       int `json:"rows"`
	CellWidth  int `json:"cellWidth,omitempty"`
	CellHeight int `json:"cellHeight,omitempty"`
}

func (s Size) Valid() bool {
	return s.Cols >= 2 && s.Cols <= 400 && s.Rows >= 1 && s.Rows <= 160 && s.CellWidth >= 0 && s.CellWidth <= 128 && s.CellHeight >= 0 && s.CellHeight <= 256
}

type Scope struct{ Owner, Provider, Session string }
type Params struct {
	Profile    string `json:"profile,omitempty"`
	ID         string `json:"id"`
	Attachment string `json:"attachment,omitempty"`
	After      int64  `json:"after,omitempty"`
	InputSeq   int64  `json:"inputSeq,omitempty"`
	Data       []byte `json:"data,omitempty"`
	Size       Size   `json:"size"`
}
type Snapshot struct {
	ID         string `json:"id"`
	Attachment string `json:"attachment"`
	Sequence   int64  `json:"sequence"`
	Data       []byte `json:"data,omitempty"`
	Size       Size   `json:"size"`
	Exited     bool   `json:"exited"`
	ExitCode   int    `json:"exitCode"`
}

type Engine interface {
	Resize(Size)
	Frame(reset bool) []byte
	Send([]byte)
	Done() <-chan struct{}
	ExitCode() int
	Close() error
}
type Factory func(ctx context.Context, directory string, size Size, shellPath string) (Engine, error)

var ErrMissing = errors.New("终端已结束或设备已重启，请重新打开终端")
var ErrDetached = errors.New("终端连接已被替换，请重新连接")

type entry struct {
	scope      Scope
	ready      chan struct{}
	err        error
	engine     Engine
	mu         sync.Mutex
	reading    string
	closed     bool
	attachment string
	size       Size
	sequence   int64
	inputSeq   int64
	inputBytes int
	last       Snapshot
}
type Manager struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	factory Factory
	entries map[string]*entry
	retired map[string]struct{}
	closed  bool
}

func New(ctx context.Context, factory Factory) *Manager {
	life, cancel := context.WithCancel(ctx)
	return &Manager{ctx: life, cancel: cancel, factory: factory, entries: map[string]*entry{}, retired: map[string]struct{}{}}
}
func validID(id string) bool { b, err := hex.DecodeString(id); return err == nil && len(b) == 32 }

// Open reserves an ID before starting the process. Repeating it can only find
// the same scoped process, including after a lost response; it never spawns twice.
func (m *Manager) Open(ctx context.Context, scope Scope, p Params, directory func(context.Context) (string, error)) (Snapshot, error) {
	if !validID(p.ID) || !p.Size.Valid() || scope.Owner == "" || scope.Provider == "" || scope.Session == "" {
		return Snapshot{}, errors.New("无效终端参数")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Snapshot{}, ErrMissing
	}
	if _, retired := m.retired[p.ID]; retired {
		m.mu.Unlock()
		return Snapshot{}, ErrMissing
	}
	e := m.entries[p.ID]
	if e != nil {
		m.mu.Unlock()
		return m.Attach(ctx, scope, p)
	}
	if len(m.retired)+len(m.entries) >= maxRetired {
		m.mu.Unlock()
		return Snapshot{}, errors.New("本次启动的终端数量已达到上限，请重启 ReignHub")
	}
	if len(m.entries) >= MaxTerminals {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("最多同时保留 %d 个终端，请先结束不再使用的终端", MaxTerminals)
	}
	e = &entry{scope: scope, ready: make(chan struct{}), size: p.Size}
	m.entries[p.ID] = e
	m.mu.Unlock()
	// A request cancellation must not kill a successfully created shell.
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	dir, err := directory(setup)
	if err == nil {
		var path string
		path, err = profilePath(setup, p.Profile)
		if err == nil {
			e.engine, err = m.factory(setup, dir, p.Size, path)
		}
	}
	stop()
	cancel()
	e.err = err
	close(e.ready)
	if err != nil {
		m.mu.Lock()
		if m.entries[p.ID] == e {
			delete(m.entries, p.ID)
		}
		m.mu.Unlock()
		return Snapshot{}, err
	}
	if m.ctx.Err() != nil {
		_ = e.engine.Close()
		return Snapshot{}, ErrMissing
	}
	return m.Attach(ctx, scope, p)
}
func (m *Manager) get(ctx context.Context, scope Scope, id string) (*entry, error) {
	m.mu.Lock()
	e := m.entries[id]
	closed := m.closed
	m.mu.Unlock()
	if closed || e == nil || e.scope != scope {
		return nil, ErrMissing
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, ErrMissing
	case <-e.ready:
	}
	if e.err != nil {
		return nil, e.err
	}
	return e, nil
}
func (m *Manager) Attach(ctx context.Context, scope Scope, p Params) (Snapshot, error) {
	e, err := m.get(ctx, scope, p.ID)
	if err != nil {
		return Snapshot{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Snapshot{}, ErrMissing
	}
	e.attachment = device.Random()
	e.inputSeq = 0
	e.sequence++
	e.last = e.snapshot(p.ID, e.engine.Frame(true))
	if len(e.last.Data) > MaxFrame {
		return Snapshot{}, errors.New("终端画面超过传输上限，请结束后重新打开")
	}
	return e.last, nil
}
func (e *entry) snapshot(id string, data []byte) Snapshot {
	exited := false
	select {
	case <-e.engine.Done():
		exited = true
	default:
	}
	return Snapshot{ID: id, Attachment: e.attachment, Sequence: e.sequence, Data: data, Size: e.size, Exited: exited, ExitCode: e.engine.ExitCode()}
}
func (e *entry) check(p Params) error {
	if e.closed {
		return ErrMissing
	}
	if p.Attachment == "" || e.attachment != p.Attachment {
		return ErrDetached
	}
	return nil
}

// Read retains the last frame until the receiver acknowledges its sequence.
// A cancelled read therefore cannot consume output that the receiver never saw.
func (m *Manager) Read(ctx context.Context, scope Scope, p Params) (Snapshot, error) {
	e, err := m.get(ctx, scope, p.ID)
	if err != nil {
		return Snapshot{}, err
	}
	e.mu.Lock()
	if err = e.check(p); err != nil {
		e.mu.Unlock()
		return Snapshot{}, err
	}
	if e.reading == p.Attachment {
		e.mu.Unlock()
		return Snapshot{}, errors.New("终端已有读取请求")
	}
	e.reading = p.Attachment
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.reading == p.Attachment {
			e.reading = ""
		}
		e.mu.Unlock()
	}()
	tick := time.NewTicker(time.Second / 30)
	defer tick.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		e.mu.Lock()
		if err = e.check(p); err != nil {
			e.mu.Unlock()
			return Snapshot{}, err
		}
		if p.After < e.sequence {
			out := e.last
			e.mu.Unlock()
			return out, nil
		}
		if p.After != e.sequence {
			e.mu.Unlock()
			return Snapshot{}, errors.New("终端画面序号不连续，请重新连接")
		}
		data := e.engine.Frame(false)
		out := e.snapshot(p.ID, data)
		if len(data) > MaxFrame {
			e.mu.Unlock()
			return Snapshot{}, errors.New("终端画面超过传输上限，请重新连接")
		}
		if len(data) > 0 || out.Exited != e.last.Exited || out.Size != e.last.Size {
			e.sequence++
			out.Sequence = e.sequence
			e.last = out
			e.mu.Unlock()
			return out, nil
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-m.ctx.Done():
			return Snapshot{}, ErrMissing
		case <-deadline.C:
			return out, nil
		case <-tick.C:
		}
	}
}
func (m *Manager) Input(ctx context.Context, scope Scope, p Params) error {
	if len(p.Data) == 0 || len(p.Data) > MaxInput {
		return errors.New("终端输入超过单次上限")
	}
	e, err := m.get(ctx, scope, p.ID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check(p); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Reject duplicates rather than acknowledge different data at the same index.
	if p.InputSeq != e.inputSeq+1 {
		return errors.New("终端输入序号不连续，请重新连接；未重发输入")
	}
	select {
	case <-e.engine.Done():
		return ErrMissing
	default:
	}
	if e.inputBytes+len(p.Data) > MaxSessionInput {
		return errors.New("终端输入已达到上限，请结束后重新打开")
	}
	e.inputSeq = p.InputSeq
	e.inputBytes += len(p.Data)
	e.engine.Send(p.Data)
	return nil
}
func (m *Manager) Resize(ctx context.Context, scope Scope, p Params) error {
	if !p.Size.Valid() {
		return errors.New("无效终端尺寸")
	}
	e, err := m.get(ctx, scope, p.ID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check(p); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.size = p.Size
	e.engine.Resize(p.Size)
	return nil
}
func (m *Manager) Detach(ctx context.Context, scope Scope, p Params) error {
	e, err := m.get(ctx, scope, p.ID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if p.Attachment == e.attachment {
		e.attachment = ""
	}
	return nil
}
func (m *Manager) Terminate(ctx context.Context, scope Scope, p Params) error {
	if !validID(p.ID) {
		return errors.New("无效终端参数")
	}
	m.mu.Lock()
	e := m.entries[p.ID]
	if e == nil {
		// A close may overtake an open whose response was lost. Retire even a
		// missing ID so that the delayed request cannot start a new shell.
		if _, known := m.retired[p.ID]; !known && len(m.retired)+len(m.entries) >= maxRetired {
			m.mu.Unlock()
			return errors.New("本次启动的终端数量已达到上限，请重启 ReignHub")
		}
		m.retired[p.ID] = struct{}{}
		m.mu.Unlock()
		return nil
	} // Idempotent explicit termination.
	m.mu.Unlock()
	if e.scope != scope {
		return ErrMissing
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.ready:
	}
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	var err error
	if e.engine != nil {
		err = e.engine.Close()
	}
	m.mu.Lock()
	if m.entries[p.ID] == e {
		delete(m.entries, p.ID)
		m.retired[p.ID] = struct{}{}
	}
	m.mu.Unlock()
	return err
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	entries := m.entries
	m.entries = map[string]*entry{}
	m.mu.Unlock()
	// Close all shells concurrently; the application's quit budget is shared,
	// not multiplied by the number of terminals or a slow process setup.
	done := make(chan struct{})
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		var wg sync.WaitGroup
		for _, e := range entries {
			wg.Go(func() {
				select {
				case <-e.ready:
				case <-shutdown.Done():
					return
				}
				e.mu.Lock()
				e.closed = true
				e.mu.Unlock()
				if e.engine != nil {
					_ = e.engine.Close()
					select {
					case <-e.engine.Done():
					case <-shutdown.Done():
					}
				}
			})
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-shutdown.Done():
		return context.DeadlineExceeded
	}
}
