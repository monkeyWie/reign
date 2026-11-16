package hubui

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/monkeyWie/reignhub/pkg/attachmentmime"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

type attachmentDownload struct {
	name, mime, previous, status, error string
	received, total                     int64
	data                                []byte
	cancel                              context.CancelFunc
}

func (s *State) download(cx *core.Scope, a Object) {
	// A recycled message row can unmount while a download is running.
	// Its callbacks belong to the application, not to the row's lifetime.
	cx = s.cx
	if s.downloadTask != nil {
		s.downloadTask.cancel()
	}
	target, p, id := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	ctx, cancel := context.WithCancel(s.ctx)
	task := &attachmentDownload{name: str(a, "name"), mime: attachmentmime.ForFile(str(a, "name"), str(a, "mime")), previous: s.dialog.GetUntracked(), status: "downloading", total: number(a, "size"), cancel: cancel}
	if task.previous == "attachment-download" {
		task.previous = ""
	}
	s.downloadTask = task
	s.notice = ""
	// Mount immediately, before the cache or the first remote request responds.
	s.dialog.Set("attachment-download")
	s.changed()
	var received, total atomic.Int64
	var posted atomic.Bool
	report := func(n, size int64) {
		// Concurrent readers can publish out of order. Keep displayed bytes monotonic.
		for old := received.Load(); n > old && !received.CompareAndSwap(old, n); old = received.Load() {
		}
		total.Store(size)
		if !posted.CompareAndSwap(false, true) {
			return
		}
		cx.Post(func() {
			posted.Store(false)
			if s.downloadTask == task && ctx.Err() == nil && task.status == "downloading" {
				task.received, task.total = received.Load(), total.Load()
				s.changed()
			}
		})
	}
	go func() {
		b, err := s.attachmentBytesProgress(ctx, target, p, id, a, false, report)
		cx.Post(func() {
			if s.downloadTask != task || ctx.Err() != nil {
				return
			}
			if err != nil {
				task.status, task.error = "error", err.Error()
				s.changed()
				return
			}
			task.data, task.received, task.total = b, int64(len(b)), int64(len(b))
			s.saveDownload(cx, task)
		})
	}()
}

func (s *State) cancelDownload(task *attachmentDownload) {
	if s.downloadTask != task {
		return
	}
	task.cancel()
	s.downloadTask = nil
	s.dialog.Set(task.previous)
	s.changed()
}

func (s *State) saveDownload(cx *core.Scope, task *attachmentDownload) {
	task.status = "saving"
	s.changed()
	cx.SaveFile(core.SaveOptions{Name: task.name, MIME: task.mime, Data: task.data}, func(saved bool, _ string, err error) {
		if s.downloadTask != task {
			return
		}
		if err != nil {
			task.status, task.error = "error", err.Error()
		} else if saved {
			s.cancelDownload(task)
			s.notice = "文件已保存"
		} else {
			// The system picker was cancelled. Retain verified bytes so saving
			// again does not redownload the attachment.
			task.status = "ready"
		}
		s.changed()
	})
}

func (s *State) downloadView(cx *core.Scope) core.Node {
	task := s.downloadTask
	if task == nil {
		return nil
	}
	return ui.Dialog{TestID: "attachment-download", Title: "下载附件", Open: func() bool { return true }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			s.cancelDownload(task)
		}
	}, Style: core.Style{Width: core.Pt(420), MaxWidth: core.Pct(94)}, Children: []core.Node{
		label(cx, task.name, 14, false),
		ui.Progress{TestID: "attachment-download-progress", Label: "附件下载进度", Value: func() float32 { s.read(); return float32(task.received) * 100 / float32(max(1, task.total)) }, Indeterminate: func() bool { s.read(); return task.total <= 0 && task.status == "downloading" }},
		dynamicText(cx, func() string {
			s.read()
			switch task.status {
			case "error":
				return "下载未完成：" + task.error
			case "saving":
				return "下载完成，请选择保存位置"
			case "ready":
				return "下载完成，可以保存文件"
			}
			if task.total <= 0 {
				return "正在准备下载…"
			}
			return fmt.Sprintf("正在下载 %d%%", task.received*100/max(1, task.total))
		}, 13, true),
	}, Footer: []core.Node{
		button(cx, "attachment-download-cancel", "取消", nil, "outline", func() { s.cancelDownload(task) }),
		core.Show{When: func() bool { s.read(); return task.data != nil && task.status != "saving" }, Then: func(cx *core.Scope) core.Node {
			return button(cx, "attachment-download-save", "保存到…", nil, "primary", func() { s.saveDownload(cx, task) })
		}},
	}}
}
