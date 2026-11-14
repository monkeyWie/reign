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

type connectAttempt struct {
	params               Object
	cancel               context.CancelFunc
	step                 connectivity.Progress
	started, stepStarted time.Time
	active               bool
	err                  string
}

func (s *State) connect(p Object) {
	if a := s.connectAttempt; a != nil && a.active {
		s.dialog.Set("connecting")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 190*time.Second)
	a := &connectAttempt{params: copyObject(p), cancel: cancel, active: true, started: time.Now(), stepStarted: time.Now(), step: connectivity.Progress{Stage: "service", Message: "正在连接服务并验证本机身份", Timeout: 15 * time.Second}}
	s.connectAttempt = a
	s.error, s.notice = "", ""
	s.dialog.Set("connecting")
	s.changed()
	ctx = connectivity.WithProgress(ctx, func(p connectivity.Progress) {
		s.cx.Post(func() {
			if s.connectAttempt != a || !a.active {
				return
			}
			a.step, a.stepStarted = p, time.Now()
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
			if err != nil {
				a.err = err.Error()
				s.changed()
				return
			}
			s.connectAttempt = nil
			s.dialog.Set("")
			s.devices = objects(v)
			for _, d := range s.devices {
				if str(d, "connectionId") == strings.NewReplacer(" ", "", "-", "").Replace(str(p, "code")) && str(d, "service") == str(p, "service") {
					s.selectDevice(str(d, "id"))
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

func (s *State) connectAttemptView(cx *core.Scope) core.Node {
	a := s.connectAttempt
	if a == nil {
		return nil
	}
	title := "正在连接设备"
	if !a.active {
		title = "连接失败"
	}
	d := ui.Dialog{TestID: "dialog-connecting", Title: title, Open: func() bool { return true }, NoCloseOnBackdropPress: true, OnOpenChange: func(v bool, _ base.Reason) {
		if !v {
			s.closeConnectAttempt()
		}
	}, Style: core.Style{Width: core.Pt(448), MaxWidth: core.Pct(96)}}
	d.Children = []core.Node{label(cx, "设备 "+str(a.params, "code"), 14, false), label(cx, str(a.params, "service"), 12, true)}
	if a.active {
		message := dynamicText(cx, func() string { s.read(); return a.step.Message }, 13, false)
		message.TestID = "connect-stage"
		message.Style.FlexShrink = 1
		message.Style.MinWidth = core.Pt(0)
		elapsedText := dynamicText(cx, func() string {
			s.read()
			elapsed := int(time.Since(a.started).Seconds())
			remaining := max(0, int((a.step.Timeout - time.Since(a.stepStarted)).Seconds()))
			return fmt.Sprintf("已用 %d 秒 · 此步骤最多再等待 %d 秒", elapsed, remaining)
		}, 12, true)
		elapsedText.TestID = "connect-elapsed"
		d.Children = append(d.Children, row(ui.Spinner{TestID: "connection-loading", Label: "正在连接", Variant: ui.SpinnerSpokes, Size: 20}, message), elapsedText)
		d.Footer = []core.Node{button(cx, "connect-cancel", "取消连接", nil, "outline", s.closeConnectAttempt)}
	} else {
		summary, hint := connectionFailure(a)
		expanded := core.NewSignal(cx, false)
		d.Children = append(d.Children, label(cx, summary, 15, false), label(cx, hint, 13, true), ui.Collapsible{TestID: "connect-error-details", Text: "错误详情", Open: expanded.Get, OnOpenChange: expanded.Set, Content: core.Scroll{Style: core.Style{MaxHeight: core.Pt(120)}, Children: []core.Node{core.Text{TestID: "connect-error-raw", Content: a.err, Selectable: core.SelectableOn, Style: core.Style{FontSize: 11}, StyleFunc: func() core.Style { return core.Style{Color: palette(cx).mutedText} }}}}})
		d.Footer = []core.Node{button(cx, "connect-edit", "修改连接信息", nil, "outline", func() {
			s.scanned = copyObject(a.params)
			s.closeConnectAttempt()
			s.dialog.Set("connect")
		}), button(cx, "connect-retry", "重试连接", lucide.RefreshCw, "primary", func() { s.connect(a.params) })}
	}
	return d
}
