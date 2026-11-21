package terminal

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Call is bound to one authenticated environment, Provider and conversation.
type Call func(context.Context, string, Params) (Snapshot, error)

var ErrInputUnconfirmed = errors.New("终端输入未能确认，未自动重发；重新连接后请先检查执行结果")

// Conn adapts the shared endpoint contract to NSGo's remote terminal API.
// It owns an attachment, not the host process. Reconnection creates a new Conn.
type Conn struct {
	ctx          context.Context
	cancel       context.CancelFunc
	call         Call
	mu           sync.Mutex
	snapshot     Snapshot
	buffer       []byte
	inputSeq     int64
	inputPending bool
	failed       bool
	inputMu      sync.Mutex
	sizes        chan Size
	ready        chan struct{}
	resize       func(Size)
	state        func(error, bool)
	once         sync.Once
}

// OpenConn starts the shell under the workspace lifetime. Its first receiver
// uses the view lifetime, which may end while the host is still starting it.
func OpenConn(view, workspace context.Context, call Call, id, profile string) (*Conn, error) {
	method := "terminal.open"
	if profile != "" {
		method = "terminal.openProfile"
	}
	return connect(view, workspace, call, method, Params{ID: id, Profile: profile, Size: Size{Cols: 80, Rows: 24}})
}

func AttachConn(ctx context.Context, call Call, id string) (*Conn, error) {
	return connect(ctx, ctx, call, "terminal.attach", Params{ID: id})
}

func connect(view, request context.Context, call Call, method string, params Params) (*Conn, error) {
	setup, stop := context.WithTimeout(request, 12*time.Second)
	defer stop()
	snapshot, err := call(setup, method, params)
	if err != nil {
		return nil, err
	}
	if snapshot.ID != params.ID || snapshot.Attachment == "" || !snapshot.Size.Valid() || len(snapshot.Data) > MaxFrame {
		return nil, errors.New("无效终端响应")
	}
	life, cancel := context.WithCancel(view)
	return &Conn{ctx: life, cancel: cancel, call: call, snapshot: snapshot, buffer: snapshot.Data, sizes: make(chan Size, 1), ready: make(chan struct{})}, nil
}

// Bind is called once, before exposing the receiver, after terminal.New returns.
func (c *Conn) Bind(resize func(Size), state func(error, bool)) {
	c.resize, c.state = resize, state
	close(c.ready)
	go c.resizeLoop()
}
func (c *Conn) params() Params {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Params{ID: c.snapshot.ID, Attachment: c.snapshot.Attachment, After: c.snapshot.Sequence}
}
func (c *Conn) Read(b []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case <-c.ready:
	}
	for {
		if err := c.ctx.Err(); err != nil {
			return 0, err
		}
		if len(c.buffer) > 0 {
			c.mu.Lock()
			size := c.snapshot.Size
			c.mu.Unlock()
			c.resize(size)
			n := copy(b, c.buffer)
			c.buffer = c.buffer[n:]
			return n, nil
		}
		c.mu.Lock()
		last := c.snapshot
		c.mu.Unlock()
		if last.Exited {
			c.state(nil, true)
			<-c.ctx.Done() // Keep the final host screen; do not append "disconnected".
			return 0, c.ctx.Err()
		}
		out, err := c.call(c.ctx, "terminal.read", c.params())
		if err != nil {
			c.fail(err)
			return 0, err
		}
		if out.ID != last.ID || out.Attachment != last.Attachment || out.Sequence < last.Sequence || out.Sequence > last.Sequence+1 || !out.Size.Valid() || len(out.Data) > MaxFrame {
			err = errors.New("终端画面不连续，请重新连接")
			c.fail(err)
			return 0, err
		}
		c.mu.Lock()
		c.snapshot = out
		c.mu.Unlock()
		if out.Sequence > last.Sequence {
			c.buffer = out.Data
			c.resize(out.Size)
		}
	}
}
func (c *Conn) Write(b []byte) (int, error) {
	c.inputMu.Lock()
	defer c.inputMu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	exited := c.snapshot.Exited
	c.mu.Unlock()
	if exited {
		return 0, io.EOF
	}
	// Refuse oversized pastes before sending any part of them.
	if len(b) > 1<<20 {
		err := errors.New("粘贴内容过大，终端已暂停输入")
		c.fail(err)
		return 0, err
	}
	n := 0
	for len(b) > 0 {
		size := min(len(b), MaxInput)
		p := c.params()
		c.inputSeq++
		p.InputSeq = c.inputSeq
		p.Data = b[:size]
		ctx, stop := context.WithTimeout(c.ctx, 8*time.Second)
		c.mu.Lock()
		if c.failed || c.ctx.Err() != nil {
			err := c.ctx.Err()
			if err == nil {
				err = ErrInputUnconfirmed
			}
			c.mu.Unlock()
			stop()
			return n, err
		}
		c.inputPending = true
		c.mu.Unlock()
		_, err := c.call(ctx, "terminal.input", p)
		stop()
		if err != nil {
			// No retry: the host may already have accepted these bytes.
			c.fail(ErrInputUnconfirmed)
			return n, err
		}
		c.mu.Lock()
		c.inputPending = false
		c.mu.Unlock()
		n += size
		b = b[size:]
	}
	return n, nil
}
func (c *Conn) Resize(cols, rows int) error { return c.ResizePixels(cols, rows, 0, 0) }
func (c *Conn) ResizePixels(cols, rows, width, height int) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	size := Size{Cols: max(2, min(cols, 400)), Rows: max(1, min(rows, 160)), CellWidth: max(0, min(width, 128)), CellHeight: max(0, min(height, 256))}
	// View resize can run on the UI thread; only keep the newest desired size.
	select {
	case <-c.sizes:
	default:
	}
	select {
	case c.sizes <- size:
	default:
	}
	return nil
}
func (c *Conn) resizeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case size := <-c.sizes:
			p := c.params()
			p.Size = size
			ctx, stop := context.WithTimeout(c.ctx, 8*time.Second)
			_, err := c.call(ctx, "terminal.resize", p)
			stop()
			if err != nil {
				c.fail(err)
				return
			}
		}
	}
}
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.failed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	c.failed = true
	if c.inputPending {
		err = ErrInputUnconfirmed
	}
	c.mu.Unlock()
	// Publish the cause before waking the reconnect owner, exactly once.
	c.state(err, false)
	c.cancel()
}
func (c *Conn) Done() <-chan struct{} { return c.ctx.Done() }
func (c *Conn) Close() error {
	c.once.Do(func() {
		c.cancel()
		p := c.params()
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			_, _ = c.call(ctx, "terminal.detach", p)
		}()
	})
	return nil
}
