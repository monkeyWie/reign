package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// StartProcess owns a private stdio app-server. Closing it ends that executor
// and releases its thread locks; Codex App never uses this transport directly.
func StartProcess(ctx context.Context, cmd *exec.Cmd, observer Observer) (*Client, error) {
	out, childOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	childIn, in, err := os.Pipe()
	if err != nil {
		_ = out.Close()
		_ = childOut.Close()
		return nil, err
	}
	cmd.Stdin = childIn
	cmd.Stdout = childOut
	err = cmd.Start()
	_ = childIn.Close()
	_ = childOut.Close()
	if err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	exited := make(chan struct{})
	life, cancel := context.WithCancel(context.Background())
	c := &Client{pending: make(map[string]chan Message), ctx: life, cancel: cancel, done: make(chan struct{}), observer: observer}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64<<10), 32<<20)
	c.read = func() ([]byte, error) {
		if scanner.Scan() {
			return scanner.Bytes(), nil
		}
		_ = out.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	writes := make(chan struct{}, 1)
	c.writeRaw = func(ctx context.Context, data []byte) error {
		select {
		case writes <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctx.Done():
			return io.ErrClosedPipe
		}
		defer func() { <-writes }()
		if err := ctx.Err(); err != nil {
			return err
		}
		// Bound local writes too: an unresponsive child must not hold every RPC.
		stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
		defer stop()
		timer := time.AfterFunc(10*time.Second, func() { _ = cmd.Process.Kill() })
		defer timer.Stop()
		_, err := in.Write(append(data, '\n'))
		return err
	}
	var closeOnce sync.Once
	c.closeRaw = func() error {
		closeOnce.Do(func() {
			// EOF gives the official server a chance to clean up its own tools. Kill
			// only this owned child if it does not exit promptly; never kill Codex App.
			_ = in.Close()
			go func() {
				select {
				case <-exited:
				case <-time.After(time.Second):
					_ = cmd.Process.Kill()
				}
				_ = out.Close()
			}()
		})
		return nil
	}
	go func() { _ = cmd.Wait(); close(exited); _ = in.Close() }()
	go c.readLoop()
	var initialized json.RawMessage
	err = c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "reignhub", "title": "ReignHub", "version": "0.1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}, &initialized)
	if err == nil {
		err = c.Notify(ctx, "initialized", map[string]any{})
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
