package codexipc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func TestWindowsNamedPipeInitializationAndRequest(t *testing.T) {
	// Never bind or connect to the real Codex router in a test.
	path := fmt.Sprintf(`\\.\pipe\reignhub-ipc-fixture-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := winio.ListenPipe(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan error, 1)
	release := make(chan struct{})
	defer close(release)
	go func() {
		c, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		for range 2 {
			request, err := ReadFrame(c)
			if err != nil {
				done <- err
				return
			}
			response := Message{Type: "response", RequestID: request.RequestID, ResultType: "success", Result: request.Params}
			if request.Method == "initialize" {
				response.Result = json.RawMessage(`{"clientId":"fixture"}`)
			}
			if err = sendFixture(c, response); err != nil {
				done <- err
				return
			}
		}
		done <- nil
		<-release
	}()
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	c, err := Dial(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	response, err := c.Request(ctx, "fixture-echo", 1, "hello", "")
	if err != nil || string(response.Result) != `"hello"` {
		t.Fatalf("named pipe protocol exchange failed: %s %v", response.Result, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
