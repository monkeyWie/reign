package codexipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestWindowsRestrictedPipeNeedsOnlyClientRights(t *testing.T) {
	path := fmt.Sprintf(`\\.\pipe\reignhub-restricted-fixture-%d-%d`, os.Getpid(), time.Now().UnixNano())
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.WRITE_DAC, windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
	}()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;0x00100083;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetKernelObjectSecurity(h, windows.DACL_SECURITY_INFORMATION, sd); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := winio.DialPipeContext(ctx, path)
	if old != nil {
		old.Close()
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("fixture must reject generic client rights: %v", err)
	}
	client, err := dialLocal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	// The client is already connected, so ConnectNamedPipe must complete without
	// waiting. The existing server handle retains its original access rights.
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	overlap := windows.Overlapped{HEvent: event}
	if err := windows.ConnectNamedPipe(h, &overlap); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		t.Fatal(err)
	}
	server, err := winio.NewOpenFile(h)
	if err != nil {
		t.Fatal(err)
	}
	h = windows.InvalidHandle
	defer server.Close()
	done := make(chan error, 1)
	go func() { _, err := io.CopyN(server, server, 4); done <- err }()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(client, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("pipe exchange: %q %v", buf, err)
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

func TestWindowsDeniedPipeMetadataAndRecovery(t *testing.T) {
	path := fmt.Sprintf(`\\.\pipe\reignhub-denied-fixture-%d-%d`, os.Getpid(), time.Now().UnixNano())
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.WRITE_DAC,
		windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
	}()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	setFixtureACL := func(sddl string) {
		t.Helper()
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.SetKernelObjectSecurity(h, windows.DACL_SECURITY_INFORMATION, sd); err != nil {
			t.Fatal(err)
		}
	}
	// Explicitly deny data read/write even for elevated test runners. Change only
	// this isolated fixture's DACL using its already-open WRITE_DAC server handle.
	setFixtureACL("D:P(D;;0x00000003;;;WD)(A;;FA;;;" + user.User.Sid.String() + ")")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	denied, err := dialLocal(ctx, path)
	if denied != nil {
		denied.Close()
	}
	var original *os.PathError
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.As(err, &original) {
		t.Fatalf("expected original access denial: %v", err)
	}
	var guidance interface{ ConnectionGuidance() string }
	if !errors.As(err, &guidance) || guidance.ConnectionGuidance() == "" {
		t.Fatalf("actual denied dial lost recovery guidance: %v", err)
	}
	// The diagnostic has connected and closed a metadata-only client. A
	// single-instance server must disconnect that finished client before accepting
	// another one, just as a listener's accept loop does. Closing the client handle
	// alone does not reset the server's pipe instance to its listening state.
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	overlap := windows.Overlapped{HEvent: event}
	connecting := false
	defer func() {
		if connecting {
			windows.CancelIoEx(h, &overlap)
			var n uint32
			windows.GetOverlappedResult(h, &overlap, &n, true)
		}
	}()
	finishConnect := func() {
		t.Helper()
		if connecting {
			var n uint32
			if err := windows.GetOverlappedResult(h, &overlap, &n, false); err != nil {
				t.Fatalf("fixture client did not connect: %v", err)
			}
			connecting = false
		}
	}
	rearm := func() {
		t.Helper()
		finishConnect()
		if err := windows.DisconnectNamedPipe(h); err != nil {
			t.Fatalf("rearm fixture pipe: %v", err)
		}
		if err := windows.ResetEvent(event); err != nil {
			t.Fatal(err)
		}
		overlap = windows.Overlapped{HEvent: event}
		if err := windows.ConnectNamedPipe(h, &overlap); errors.Is(err, windows.ERROR_IO_PENDING) {
			connecting = true
		} else if err != nil {
			t.Fatalf("listen on fixture pipe: %v", err)
		}
	}
	rearm()
	pid, err := pipeServerProcessID(ctx, path)
	if err != nil || pid != uint32(os.Getpid()) {
		t.Fatalf("metadata lookup on denied pipe: PID %d, error %v", pid, err)
	}
	processes, err := snapshotPipeProcesses(ctx)
	if err != nil || processes[pid].name == "" {
		t.Fatalf("snapshot missing fixture server PID %d: %v", pid, err)
	}
	rearm()
	// A fresh dial must recover on the same single-instance fixture, with no
	// cached denial or diagnostic handle holding the instance open.
	setFixtureACL("D:P(A;;0x00100083;;;" + user.User.Sid.String() + ")")
	client, err := dialLocal(ctx, path)
	if err != nil {
		t.Fatalf("fresh dial after removing fixture denial: %v", err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	finishConnect()
	server, err := winio.NewOpenFile(h)
	if err != nil {
		t.Fatal(err)
	}
	h = windows.InvalidHandle
	defer server.Close()
	done := make(chan error, 1)
	go func() { _, err := io.CopyN(server, server, 4); done <- err }()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(client, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("recovered pipe exchange: %q %v", buf, err)
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
