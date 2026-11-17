// Package codexipc implements the local Codex App owner/follower transport.
// It only connects to an existing local router; it never starts or alters App.
package codexipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const MaxFrame = 32 << 20

type Message struct {
	Type       string          `json:"type"`
	RequestID  string          `json:"requestId,omitempty"`
	Method     string          `json:"method,omitempty"`
	Version    int             `json:"version"`
	Source     string          `json:"sourceClientId,omitempty"`
	Target     string          `json:"targetClientId,omitempty"`
	Targets    []string        `json:"targetClientIds,omitempty"`
	HostID     string          `json:"hostId,omitempty"`
	TimeoutMS  int64           `json:"timeoutMs,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	ResultType string          `json:"resultType,omitempty"`
	HandledBy  string          `json:"handledByClientId,omitempty"`
	Error      string          `json:"error,omitempty"`
	Request    *Message        `json:"request,omitempty"`
	Response   any             `json:"response,omitempty"`
}

type RemoteError string

func (e RemoteError) Error() string { return string(e) }

// UnknownOutcome means the request may already have been executed. Callers must
// reconcile state, not automatically replay it or switch to another executor.
type UnknownOutcome struct {
	Method string
	Err    error
}

func (e *UnknownOutcome) Error() string {
	return fmt.Sprintf("%s 结果待确认，可能已执行；不会自动重发: %v", e.Method, e.Err)
}
func (e *UnknownOutcome) Unwrap() error { return e.Err }

type Handler interface {
	CanHandle(Message) bool
	Handle(context.Context, Message) (any, error)
	Broadcast(Message)
}

type Client struct {
	conn      net.Conn
	handler   Handler
	mu        sync.Mutex
	id        string
	pending   map[string]chan Message
	writes    chan struct{}
	next      atomic.Uint64
	done      chan struct{}
	closeOnce sync.Once
	events    chan Message
	workers   chan struct{}
	life      context.Context
	cancel    context.CancelFunc
}

func Dial(ctx context.Context, endpoint string, handler Handler) (*Client, error) {
	conn, err := dialLocal(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	c, err := Connect(ctx, conn, handler)
	if err != nil {
		_ = conn.Close()
	}
	return c, err
}

// Connect also supports isolated in-memory protocol fixtures.
func Connect(ctx context.Context, conn net.Conn, handler Handler) (*Client, error) {
	life, cancel := context.WithCancel(context.Background())
	c := &Client{writes: make(chan struct{}, 1), conn: conn, handler: handler, pending: map[string]chan Message{}, done: make(chan struct{}), events: make(chan Message, 64), workers: make(chan struct{}, 32), life: life, cancel: cancel}
	go c.readLoop()
	go c.eventLoop()
	res, err := c.Request(ctx, "initialize", 0, map[string]string{"clientType": "reignhub"}, "")
	if err == nil {
		var v struct {
			ClientID string `json:"clientId"`
		}
		err = json.Unmarshal(res.Result, &v)
		if err == nil && v.ClientID == "" {
			err = errors.New("IPC initialize returned no client ID")
		}
		c.mu.Lock()
		c.id = v.ClientID
		c.mu.Unlock()
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Client) ID() string            { c.mu.Lock(); defer c.mu.Unlock(); return c.id }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() { c.cancel(); _ = c.conn.Close(); close(c.done) })
	return nil
}

func (c *Client) Request(ctx context.Context, method string, version int, params any, target string) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	id := fmt.Sprint(c.next.Add(1))
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[id] = ch
	source := c.id
	c.mu.Unlock()
	if source == "" {
		source = "initializing-client"
	}
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	deadline, _ := ctx.Deadline()
	m := Message{Type: "request", RequestID: id, Method: method, Version: version, Params: raw, Source: source, Target: target, TimeoutMS: time.Until(deadline).Milliseconds()}
	if err = c.write(ctx, m); err != nil {
		return Message{}, &UnknownOutcome{method, err}
	}
	select {
	case res := <-ch:
		if res.ResultType != "success" {
			e := RemoteError(res.Error)
			if strings.HasSuffix(res.Error, "-timeout") {
				return res, &UnknownOutcome{method, e}
			}
			switch res.Error {
			case "timeout", "request-timeout", "client-disconnected", "server-closed", "connection-closed", "request-outcome-unknown", "context deadline exceeded":
				return res, &UnknownOutcome{method, e}
			}
			return res, e
		}
		return res, nil
	case <-ctx.Done():
		return Message{}, &UnknownOutcome{method, ctx.Err()}
	case <-c.done:
		return Message{}, &UnknownOutcome{method, errors.New("IPC disconnected")}
	}
}
func (c *Client) Broadcast(ctx context.Context, method string, version int, params any, targets []string) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(ctx, Message{Type: "broadcast", Method: method, Version: version, Params: raw, Source: c.ID(), Targets: targets})
}
func (c *Client) write(ctx context.Context, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > MaxFrame {
		return errors.New("IPC frame exceeds limit")
	}
	select {
	case c.writes <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return net.ErrClosed
	}
	defer func() { <-c.writes }()
	if err = ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	defer c.conn.SetWriteDeadline(time.Time{})
	frame := make([]byte, len(data)+4)
	binary.LittleEndian.PutUint32(frame, uint32(len(data)))
	copy(frame[4:], data)
	for len(frame) > 0 {
		n, e := c.conn.Write(frame)
		if e != nil {
			c.Close()
			return e
		}
		if n == 0 {
			c.Close()
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}
func ReadFrame(r io.Reader) (Message, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Message{}, err
	}
	n := binary.LittleEndian.Uint32(header[:])
	if n == 0 || n > MaxFrame {
		return Message{}, errors.New("invalid IPC frame size")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return Message{}, err
	}
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}
func (c *Client) readLoop() {
	defer c.Close()
	for {
		m, err := ReadFrame(c.conn)
		if err != nil {
			return
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[m.RequestID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		case "broadcast":
			if len(m.Targets) > 0 {
				found := false
				for _, id := range m.Targets {
					found = found || id == c.ID()
				}
				if !found {
					continue
				}
			}
			// Never block responses behind a large state stream. Overflow invalidates
			// this connection; the bridge will refollow and obtain a fresh snapshot.
			select {
			case c.events <- m:
			default:
				return
			}
		case "client-discovery-request", "request":
			select {
			case c.workers <- struct{}{}:
				go func() { defer func() { <-c.workers }(); c.handle(m) }()
			default:
				if m.Type == "request" {
					return
				} // bounded work; reconnect rather than hang
			}
		}
	}
}
func (c *Client) eventLoop() {
	for {
		select {
		case m := <-c.events:
			if c.handler != nil {
				c.handler.Broadcast(m)
			}
		case <-c.done:
			return
		}
	}
}
func (c *Client) handle(m Message) {
	budget := 30 * time.Second
	if m.TimeoutMS > 0 && m.TimeoutMS < budget.Milliseconds() {
		budget = time.Duration(m.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(c.life, budget)
	defer cancel()
	if m.Type == "client-discovery-request" {
		yes := m.Request != nil && c.handler != nil && c.handler.CanHandle(*m.Request)
		_ = c.write(ctx, Message{Type: "client-discovery-response", RequestID: m.RequestID, Response: map[string]bool{"canHandle": yes}})
		return
	}
	reply := Message{Type: "response", RequestID: m.RequestID, Method: m.Method, HandledBy: c.ID(), ResultType: "error", Error: "no-client-found"}
	if c.handler != nil && c.handler.CanHandle(m) {
		result, err := c.handler.Handle(ctx, m)
		if err != nil {
			reply.Error = err.Error()
			var unknown *UnknownOutcome
			if errors.As(err, &unknown) {
				reply.Error = "request-outcome-unknown"
			}
		} else {
			reply.Result, err = json.Marshal(result)
			if err != nil {
				reply.Error = err.Error()
			} else {
				reply.ResultType = "success"
				reply.Error = ""
			}
		}
	}
	_ = c.write(ctx, reply)
}
