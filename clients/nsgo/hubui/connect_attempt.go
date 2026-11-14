package hubui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type connectionStep struct {
	connectivity.Progress
	at time.Time
}

type connectAttempt struct {
	steps                          []connectionStep
	target                         string
	complete                       bool
	params                         Object
	cancel                         context.CancelFunc
	step                           connectivity.Progress
	started, stepStarted, finished time.Time
	active                         bool
	err                            string
}

func (s *State) connect(p Object) {
	if a := s.connectAttempt; a != nil && a.active {
		s.dialog.Set("connecting")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 190*time.Second)
	a := &connectAttempt{params: copyObject(p), cancel: cancel, active: true, started: time.Now(), stepStarted: time.Now(), step: connectivity.Progress{Stage: "service", Message: "正在连接服务并验证本机身份", Timeout: 15 * time.Second}}
	a.record(a.step)
	s.connectAttempt = a
	s.error, s.notice = "", ""
	s.dialog.Set("connecting")
	s.changed()
	ctx = connectivity.WithProgress(ctx, func(p connectivity.Progress) {
		s.cx.Post(func() {
			if s.ctx.Err() != nil || (s.connectAttempt != a && (a.target == "" || s.connection(a.target).attempt != a)) || !a.active && !a.complete {
				return
			}
			a.record(p)
			s.changed()
		})
	})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.cx.Post(func() {
					if s.connectAttempt == a && a.active {
						s.changed()
					}
				})
			}
		}
	}()
	go func() {
		v, err := s.api.Call(ctx, "client.connect", a.params, "", nil)
		cancel()
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.connectAttempt != a || !a.active {
				return
			}
			a.active = false
			a.finished = time.Now()
			if err != nil {
				a.err = err.Error()
				s.changed()
				return
			}
			a.complete = true
			s.dialog.Set("")
			s.devices = objects(v)
			for _, d := range s.devices {
				if str(d, "connectionId") == strings.NewReplacer(" ", "", "-", "").Replace(str(p, "code")) && str(d, "service") == str(p, "service") {
					a.target = str(d, "id")
					s.connection(a.target).attempt = a
					s.selectDevice(a.target)
					break
				}
			}
			s.changed()
			s.poll()
		})
	}()
}

func (s *State) closeConnectAttempt() {
	if a := s.connectAttempt; a != nil {
		a.active = false
		a.cancel()
	}
	s.connectAttempt = nil
	s.dialog.Set("")
	s.changed()
}

func connectionFailure(a *connectAttempt) (string, string) {
	raw := strings.ToLower(a.err)
	switch {
	case strings.Contains(raw, "登录并绑定") || strings.Contains(raw, "account_required"):
		return "此服务要求登录", "请先在被控电脑上的 ReignHub 中登录并绑定账号，或改用允许免登录的自建服务。"
	case strings.Contains(raw, "离线") || strings.Contains(raw, "host_offline"):
		return "被控电脑不在线", "请打开电脑上的 ReignHub，确认电脑联网，且两端使用同一个服务地址。"
	case strings.Contains(raw, "找不到") || strings.Contains(raw, "device_not_found"):
		return "找不到这台设备", "请检查设备 ID，并确认手机与电脑使用相同的服务地址。"
	case strings.Contains(raw, "主机拒绝") || strings.Contains(raw, "授权被拒绝") || strings.Contains(raw, "密码错误"):
		return "连接未获授权", "请检查访问密码，或在电脑上的 ReignHub 中允许连接。"
	case strings.Contains(raw, "中继权限") || strings.Contains(raw, "不包含中继") || strings.Contains(raw, "直连失败"):
		return "直连未能建立", "当前网络未能建立直连，且此服务未授予中继权限。可换用同一局域网，或使用支持中继的服务后重试。"
	case a.step.Stage == "approval":
		return "等待授权未完成", "请检查电脑上的 ReignHub 是否收到了连接申请，并点击允许；也可以使用电脑设置的访问密码。"
	case a.step.Stage == "service" || a.step.Stage == "resolve" || a.step.Stage == "request" || a.step.Stage == "signal":
		return "无法连接服务", "请检查网络和服务地址。如果浏览器也无法打开这个地址，请先恢复网络连接。"
	case a.step.Stage == "direct":
		return "连接验证未完成", "请检查电脑是否仍在线，或切换网络后重试。"
	default:
		return "设备连接未完成", "请确认电脑上的 ReignHub 保持运行，再重新连接。"
	}
}

func (a *connectAttempt) record(p connectivity.Progress) {
	if p.ID == "" {
		p.ID = p.Stage
	}
	if p.State == "" {
		p.State = "running"
	}
	if p.Timeout > 0 || p.Stage == "connected" {
		for i := range a.steps {
			if a.steps[i].State == "running" && a.steps[i].Timeout > 0 {
				a.steps[i].State = "success"
			}
		}
		if p.Timeout > 0 {
			a.step, a.stepStarted = p, time.Now()
		}
	}
	for i := range a.steps {
		if a.steps[i].ID == p.ID {
			a.steps[i].Progress = p
			return
		}
	}
	a.steps = append(a.steps, connectionStep{Progress: p, at: time.Now()})
	if len(a.steps) > 80 {
		a.steps = a.steps[len(a.steps)-80:]
	}
}

func (s *State) connectAttemptView(cx *core.Scope) core.Node {
	return s.connectionPanel(cx, "", s.connectAttempt, true)
}

// Pairing, connection status and every retry share this one compact panel.
func (s *State) connectionPanel(cx *core.Scope, id string, a *connectAttempt, initial bool) core.Node {
	close := func() { s.dialog.Set("") }
	if initial {
		close = s.closeConnectAttempt
	}
	d := ui.Dialog{TestID: choose(initial, "dialog-connecting", "dialog-connection-status"), Title: "连接", Open: func() bool { return true }, NoCloseOnBackdropPress: true,
		OnOpenChange: func(v bool, _ base.Reason) {
			if !v {
				close()
			}
		}, Style: core.Style{Width: core.Pt(448), MaxWidth: core.Pct(96)}}
	target := dynamicText(cx, func() string {
		s.read()
		for _, device := range s.devices {
			if str(device, "id") == id {
				return deviceName(device)
			}
		}
		if a != nil {
			return "设备 " + str(a.params, "code")
		}
		return "服务连接"
	}, 14, false)
	target.Style.FontWeight = core.FontWeightSemibold
	d.Children = []core.Node{target}
	if id != "" {
		d.Children = append(d.Children, dynamicText(cx, func() string {
			s.read()
			for _, device := range s.devices {
				if str(device, "id") == id {
					return deviceStatus(device)
				}
			}
			return ""
		}, 11, true))
	}
	if a != nil {
		d.Children = append(d.Children, singleLine(cx, func() string { return str(a.params, "service") }, 11, true))
	}

	header := []core.Node{}
	if a != nil && a.active {
		header = append(header, ui.Spinner{TestID: "connection-loading", Label: "正在连接", Variant: ui.SpinnerSpokes, Size: 16})
	}
	header = append(header, dynamicText(cx, func() string {
		s.read()
		if a != nil && a.active {
			return "正在连接"
		}
		if a != nil && a.err != "" {
			summary, _ := connectionFailure(a)
			return summary
		}
		if s.connectionProblem(id) {
			return "连接暂时不可用"
		}
		return "连接正常，消息会持续同步。"
	}, 13, false), spacer())
	if a != nil {
		elapsed := dynamicText(cx, func() string { s.read(); return fmt.Sprintf("已用 %d 秒", int(a.elapsed().Seconds())) }, 11, true)
		elapsed.TestID = "connect-elapsed"
		header = append(header, elapsed)
	}
	d.Children = append(d.Children, row(header...))
	if a != nil {
		ref := &core.Ref{}
		follow := core.NewSignal(cx, true)
		d.Children = append(d.Children, core.Scroll{Ref: ref, OnScroll: func(e core.ScrollEvent) { follow.Set(e.ContentSize.Height-e.Offset.Y-e.Size.Height < 36) }, TestID: "connection-timeline", Style: core.Style{MaxHeight: core.Pt(260)}, Children: []core.Node{core.View{OnLayout: func(core.Bounds) {
			if follow.Get() {
				ref.ScrollToEnd(false)
			}
		}, Children: []core.Node{core.Each(func() []connectionStep { s.read(); return append([]connectionStep{}, a.steps...) }, func(p connectionStep) string { return p.ID }, func(cx *core.Scope, p func() connectionStep, _ func() int) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				v := p()
				icon := lucide.Circle
				if v.State == "success" {
					icon = lucide.Check
				} else if v.State == "warning" {
					icon = lucide.Minus
				}
				title := label(cx, v.Message, 12, false)
				title.TestID = "connect-stage"
				children := []core.Node{row(glyph(cx, icon, 12), core.View{Style: fill, Children: []core.Node{title}}, label(cx, fmt.Sprintf("%.1fs", v.at.Sub(a.started).Seconds()), 10, true))}
				if v.Detail != "" {
					detail := label(cx, v.Detail, 11, true)
					detail.Selectable = core.SelectableOn
					children = append(children, core.View{Style: core.Style{Padding: core.Edges{Left: 20}}, Children: []core.Node{detail}})
				}
				return core.View{Style: core.Style{Padding: core.Symmetric(6, 0), Gap: 3}, Children: children}
			})
		})}}}})
	}
	raw := s.connectionDetails(id)
	if a != nil && a.err != "" {
		raw = a.err
	}
	if raw != "" {
		text := label(cx, raw, 11, true)
		text.TestID = "connection-error-raw"
		text.Selectable = core.SelectableOn
		d.Children = append(d.Children, core.Scroll{Style: core.Style{MaxHeight: core.Pt(90)}, Children: []core.Node{text}})
	}
	if a != nil && !a.active && a.err != "" {
		_, hint := connectionFailure(a)
		d.Children = append(d.Children, label(cx, hint, 12, true))
	}
	active := a != nil && a.active
	if active {
		d.Footer = []core.Node{button(cx, "connect-cancel", "取消", nil, "outline", func() { a.active = false; a.cancel(); close(); s.changed() })}
	} else {
		d.Footer = []core.Node{button(cx, "connection-done", "关闭", nil, "outline", close)}
		if initial && a != nil && !a.complete {
			d.Footer = append(d.Footer, button(cx, "connect-edit", "修改", nil, "outline", func() { s.scanned = copyObject(a.params); s.closeConnectAttempt(); s.dialog.Set("connect") }), button(cx, "connect-retry", "重试", lucide.RefreshCw, "primary", func() { s.connect(a.params) }))
		} else if id != "" {
			d.Footer = append(d.Footer, button(cx, "connection-retry", "重试", lucide.RefreshCw, "primary", func() { s.reconnectDevice(id) }))
		}
	}
	return d
}

func (a *connectAttempt) elapsed() time.Duration {
	if !a.finished.IsZero() {
		return a.finished.Sub(a.started)
	}
	return time.Since(a.started)
}
