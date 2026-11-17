package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/rpc"
)

var runtimeFixtureSetup = func() error { return nil }

func TestMain(m *testing.M) {
	if os.Getenv("REIGN_TEST_STDIO_CODEX") == "1" {
		if err := runtimeFixtureSetup(); err != nil {
			os.Exit(3)
		}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var msg rpc.Message
			_ = json.Unmarshal(scanner.Bytes(), &msg)
			if msg.Method == "initialized" {
				continue
			}
			result := json.RawMessage(`{}`)
			if msg.Method == "echo" {
				result = msg.Params
			}
			raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": result})
			fmt.Println(string(raw))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestPrivateRuntimeClosesAndReleasesProcess(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "REIGN_TEST_STDIO_CODEX=1")
	c, err := rpc.StartProcess(ctx, cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	var echo map[string]string
	if err = c.Call(ctx, "echo", map[string]string{"text": "private"}, &echo); err != nil || echo["text"] != "private" {
		t.Fatal(echo, err)
	}
	_ = c.Close()
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("private runtime did not stop")
	}
	// A closed runtime must never remain usable or be reused as a shared backend.
	if err = c.Call(ctx, "echo", nil, nil); err == nil {
		t.Fatal("closed executor still usable")
	}
}
