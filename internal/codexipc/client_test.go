package codexipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
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
