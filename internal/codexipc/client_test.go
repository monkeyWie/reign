package codexipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func sendFixture(w io.Writer, m Message) error {
	raw, _ := json.Marshal(m)
	frame := make([]byte, 4+len(raw))
	binary.LittleEndian.PutUint32(frame, uint32(len(raw)))
	copy(frame[4:], raw)
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		frame = frame[n:]
	}
	return nil
}
func TestFragmentedConcurrentRequestsAndUnknownOutcome(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	var writes sync.Mutex
	reply := func(m Message) {
		writes.Lock()
		defer writes.Unlock()
		data, _ := json.Marshal(m)
		frame := make([]byte, 4+len(data))
		binary.LittleEndian.PutUint32(frame, uint32(len(data)))
		copy(frame[4:], data)
		for _, part := range [][]byte{frame[:1], frame[1:3], frame[3:]} {
			if _, err := b.Write(part); err != nil {
				return
			}
		}
	}
	go func() {
		for {
			m, err := ReadFrame(b)
			if err != nil {
				return
			}
			if m.Method == "never-ack" {
				continue
			}
			res := Message{Type: "response", RequestID: m.RequestID, Method: m.Method, ResultType: "success", Result: m.Params}
			if m.Method == "initialize" {
				res.Result = json.RawMessage(`{"clientId":"fixture"}`)
			}
			go reply(res)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := c.Request(ctx, "echo", 0, i, "")
			if err != nil || string(res.Result) != string(mustJSON(i)) {
				t.Errorf("request %d: %s %v", i, res.Result, err)
			}
		}(i)
	}
	wg.Wait()
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	_, err = c.Request(short, "never-ack", 0, nil, "")
	var unknown *UnknownOutcome
	if !errors.As(err, &unknown) {
		t.Fatalf("unsafe timeout classification: %v", err)
	}
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("pending leaked: %d", n)
	}
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
func TestOversizedFrameRejectedBeforeBody(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() {
		defer b.Close()
		var head [4]byte
		binary.LittleEndian.PutUint32(head[:], MaxFrame+1)
		_, _ = b.Write(head[:])
	}()
	if _, err := ReadFrame(a); err == nil {
		t.Fatal("unbounded frame accepted")
	}
}

func TestQueuedWriteHonorsCancellation(t *testing.T) {
	// Hold the writer token as if a previous peer write were stalled. A later
	// request must return at its own deadline without waiting for that writer.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &Client{conn: a, writes: make(chan struct{}, 1), done: make(chan struct{})}
	c.writes <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	completed := make(chan error, 1)
	go func() { completed <- c.write(ctx, Message{Type: "request"}) }()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request waited behind another write")
	}
}

type stalledBroadcast struct {
	Handler
	started, release chan struct{}
}

func (h stalledBroadcast) Broadcast(Message) {
	close(h.started)
	<-h.release
}

// Use a small byte budget with the real framing, reader and event worker so
// the test can distinguish a busy handler from a frame still in the queue.
func TestBroadcastBytesBoundInFlightAndPreserveResponses(t *testing.T) {
	client, router := net.Pipe()
	defer router.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h := stalledBroadcast{started: make(chan struct{}), release: make(chan struct{})}
	defer close(h.release)
	life, stop := context.WithCancel(context.Background())
	defer stop()
	c := &Client{life: life, conn: client, handler: h, pending: map[string]chan Message{}, writes: make(chan struct{}, 1), events: make(chan broadcastFrame, 64), done: make(chan struct{}), cancel: stop}
	defer c.Close()
	m := Message{Type: "broadcast", Params: mustJSON(strings.Repeat("x", 1024))}
	go c.readLoop(int64(2 * len(mustJSON(m))))
	go c.eventLoop()
	if err := sendFixture(router, m); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.started:
	case <-ctx.Done():
		t.Fatal("handler did not start")
	}
	if err := sendFixture(router, m); err != nil {
		t.Fatal(err)
	}
	go func() {
		req, err := ReadFrame(router)
		if err == nil {
			_ = sendFixture(router, Message{Type: "response", RequestID: req.RequestID, ResultType: "success"})
		}
	}()
	if _, err := c.Request(ctx, "probe", 1, nil, ""); err != nil {
		t.Fatal("busy broadcast blocked response", err)
	}
	if err := sendFixture(router, m); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("byte overflow did not disconnect")
	}
}
