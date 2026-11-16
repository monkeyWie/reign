package hubui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type downloadFixture struct {
	*demoAPI
	data                []byte
	gate, chunks        chan struct{}
	started             chan int64
	active, peak, reads atomic.Int32
	corrupt             bool
}

func (f *downloadFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method != "client.call" || str(r, "method") != "attachment.read" {
		return f.demoAPI.Call(ctx, method, p, target, r)
	}
	offset := number(object(r["params"]), "offset")
	f.reads.Add(1)
	active := f.active.Add(1)
	defer f.active.Add(-1)
	for old := f.peak.Load(); active > old && !f.peak.CompareAndSwap(old, active); old = f.peak.Load() {
	}
	if f.started != nil {
		f.started <- offset
	}
	gate := f.gate
	if offset > 0 && f.chunks != nil {
		gate = f.chunks
	}
	if gate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-gate:
		}
	}
	end := min(offset+(48<<10), int64(len(f.data)))
	b := append([]byte(nil), f.data[offset:end]...)
	if f.corrupt && offset > 0 {
		b[0] ^= 1
	}
	return Object{"offset": offset, "size": len(f.data), "sha256": hashBytes(f.data), "data": base64.StdEncoding.EncodeToString(b), "eof": end == int64(len(f.data))}, nil
}

func (f *downloadFixture) attachment() Object {
	return Object{"id": "apk", "name": "ReignHub.apk", "mime": "application/zip", "size": len(f.data), "state": "ready", "sha256": hashBytes(f.data)}
}

func TestAttachmentDownloadUsesFourReadsAndVerifiesBytes(t *testing.T) {
	f := &downloadFixture{demoAPI: newDemoAPI(), data: bytes.Repeat([]byte("APK data"), 50000), chunks: make(chan struct{}), started: make(chan int64, 32)}
	s := &State{api: f}
	result := make(chan error, 1)
	var received atomic.Int64
	go func() {
		b, err := s.attachmentBytesProgress(context.Background(), "fixture", "codex-app", "session", f.attachment(), false, func(n, total int64) { received.Store(n) })
		if err == nil && !bytes.Equal(b, f.data) {
			err = errors.New("download changed bytes")
		}
		result <- err
	}()
	for range 5 {
		select {
		case <-f.started:
		case <-time.After(time.Second):
			t.Fatal("parallel reads did not start")
		}
	}
	if received.Load() != 48<<10 || f.peak.Load() != 4 {
		t.Fatal("wrong progress or read concurrency", received.Load(), f.peak.Load())
	}
	close(f.chunks)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if received.Load() != int64(len(f.data)) || f.peak.Load() > 4 {
		t.Fatal("invalid completion progress/concurrency")
	}
	f.corrupt = true
	if _, err := s.attachmentBytes(context.Background(), "fixture", "codex-app", "session", f.attachment(), false); err == nil {
		t.Fatal("accepted corrupted download")
	}
}

func TestAttachmentDownloadShowsDialogBeforeReadingAndCancels(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("save request inspection uses the headless service driver")
	}
	f := &downloadFixture{demoAPI: newDemoAPI(), data: []byte("APK"), gate: make(chan struct{}), started: make(chan int64, 4)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.target, s.selected = "fixture", Object{"id": "session", "providerId": "codex-app"}
		return core.View{Style: fill, Children: []core.Node{s.fileTile(cx, f.attachment()), core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	})
	app.Press("id:download-apk")
	app.WaitFor("id:attachment-download", time.Second)
	if len(app.Saves) != 0 || s.notice != "" {
		t.Fatal("save or toast appeared before download")
	}
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	app.Press("id:attachment-download-cancel")
	app.WaitUntil(time.Second, func() bool { return f.active.Load() == 0 })
	close(f.gate)
	app.Settle()
	if app.Exists("id:attachment-download") || len(app.Saves) != 0 {
		t.Fatal("cancelled download opened save picker")
	}
	app.Press("id:download-apk")
	app.WaitFor("id:attachment-download-save", time.Second) // Test picker returns cancellation.
	if len(app.Saves) != 1 || app.Saves[0].Name != "ReignHub.apk" || app.Saves[0].MIME != "application/vnd.android.package-archive" || !bytes.Equal(app.Saves[0].Data, f.data) {
		t.Fatal("incorrect Android save request", app.Saves)
	}
	reads := f.reads.Load()
	app.Press("id:attachment-download-save")
	app.WaitUntil(time.Second, func() bool { return len(app.Saves) == 2 })
	if f.reads.Load() != reads {
		t.Fatal("cancelling the save picker discarded verified bytes")
	}
}

func TestAttachmentDownloadProgressView(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, newDemoAPI())
		s.applyColors()
		s.downloadTask = &attachmentDownload{name: "reignhub-0.1.0-dev.126-android-arm64.apk", status: "downloading", received: 155648, total: 409600, cancel: func() {}}
		s.dialog.Set("attachment-download")
		return core.View{Style: fill, Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	}, nsgotest.Size(390, 844))
	app.Find("text:正在下载 38%")
	app.Find("id:attachment-download-cancel")
	app.Screenshot("attachment-download-progress")
}
