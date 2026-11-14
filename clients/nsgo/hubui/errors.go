package hubui

import (
	"context"
	"errors"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func operationErrorText(raw string, failure *sessionOperationError) (title, description string) {
	help := sessionAccessHelp(raw, failure)
	if help.Title != "" {
		return help.Title, help.Description
	}
	title, description = "操作未完成", raw
	if strings.Contains(strings.ToLower(raw), "deadline exceeded") || failure != nil && errors.Is(failure, context.DeadlineExceeded) {
		description = "请求未及时返回，请确认操作结果后再决定是否重试。"
	} else if transportError(errors.New(raw)) {
		description = "暂时无法完成操作，请检查设备连接后重试。"
	} else if r := []rune(description); len(r) > 120 {
		description = string(r[:120]) + "…"
	}
	return
}

// Keep the failed operation's context: the user can switch sessions while RPC runs.
type sessionOperationError struct {
	cause                                             error
	target, device, provider, session, source, method string
}

func (e *sessionOperationError) Error() string { return e.cause.Error() }
func (e *sessionOperationError) Unwrap() error { return e.cause }

type accessHelp struct {
	Title, Description string
	Steps              []string
}

func sessionAccessHelp(raw string, failure *sessionOperationError) accessHelp {
	if failure == nil || failure.Error() != raw || failure.method != "send" && failure.method != "steer" || !strings.HasPrefix(failure.provider, "codex-") || !strings.Contains(strings.ToLower(raw), "already has an active writer") {
		return accessHelp{}
	}
	app := failure.provider == "codex-app" || failure.source == "app"
	h := accessHelp{Title: "此会话暂时无法发送消息", Description: "该会话由另一个 Codex 进程管理。能查看会话历史，不代表 ReignHub 已接入它的运行后端。输入内容已保留。"}
	where := "被控电脑"
	if failure.device != "" {
		where += "「" + failure.device + "」"
	}
	if app {
		h.Steps = []string{
			"在" + where + "上打开 ReignHub，进入「本机」。",
			"先等待 Codex 当前任务结束，再在「Codex App」卡片点击「启用」，按提示正常退出并重新打开 Codex App。",
			"确认 Codex App 显示「已就绪」后，返回此会话重试发送。",
		}
	} else {
		h.Steps = []string{
			"在" + where + "上确认这个会话当前由哪个 Codex 应用或终端管理。",
			"如果来自 Codex App：等待当前任务结束，在 ReignHub「本机」的「Codex App」卡片点击「启用」，按提示重新接入。",
			"如果来自 Codex CLI：需让 CLI 会话使用 ReignHub 已接入的共享后台；独立进程占用的会话不能直接接管。",
			"接入完成后返回原会话重试；请勿仅因这个提示强制结束正在运行的任务。",
		}
	}
	return h
}

func (s *State) errorAtComposer() bool {
	s.read()
	f := s.failure
	return s.error != "" && f != nil && f.Error() == s.error && s.selected != nil && f.target == s.target && f.provider == str(s.selected, "providerId") && f.session == str(s.selected, "id") && (s.width.Get() >= wideBreakpoint || s.narrowChat)
}

func (s *State) errorOverlay(cx *core.Scope, composer bool) core.Node {
	style := core.Style{Position: core.Absolute, Left: core.Pt(18), Right: core.Pt(18), Bottom: core.Pt(20), AlignItems: core.AlignEnd, ZIndex: 50}
	if composer {
		style.Bottom = core.Pct(100)
		style.Margin.Bottom = 8
		style.AlignItems = core.AlignCenter
	}
	return core.View{TestID: "error-overlay", Style: style, Children: []core.Node{s.errorCard(cx)}}
}

func (s *State) errorCard(cx *core.Scope) core.Node {
	message := core.NewMemo(cx, func() string { s.read(); return s.error })
	return core.Dynamic(func(cx *core.Scope) core.Node {
		raw := message.Get()
		help := sessionAccessHelp(raw, s.failure)
		title, description := operationErrorText(raw, s.failure)
		expanded := core.NewSignal(cx, false)
		heading := label(cx, title, 13, false)
		heading.Style.FontWeight = core.FontWeightSemibold
		actions := []core.Node{}
		if help.Title != "" {
			actions = append(actions, button(cx, "session-access-help", "查看接入步骤", nil, "outline", func() { s.dialog.Set("session-access-help") }))
		}
		if description != raw {
			actions = append(actions, button(cx, "session-error-details", "错误详情", nil, "ghost", func() { expanded.Set(!expanded.GetUntracked()) }))
		}
		return core.View{TestID: "error-banner", Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(520), Padding: core.All(14), Gap: 8, CornerRadius: 12, BorderWidth: 1, Shadows: []core.BoxShadow{{Offset: core.Point{Y: 6}, Blur: 24, Color: core.RGBA(0x000000, .16)}}}, StyleFunc: func() core.Style {
			p := palette(cx)
			return core.Style{Background: p.background, BorderColor: core.RGBA(0xf59e0b, .45), Color: p.foreground}
		}, Children: []core.Node{
			row(glyph(cx, lucide.CircleAlert, 18), core.View{Style: fill, Children: []core.Node{heading}}, button(cx, "dismiss-error", "", lucide.X, "ghost", func() { s.error = ""; s.failure = nil; s.changed() })),
			label(cx, description, 12, true),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: actions},
			core.Show{When: expanded.Get, Then: func(cx *core.Scope) core.Node {
				t := label(cx, raw, 11, true)
				t.TestID = "session-error-raw"
				t.Selectable = core.SelectableOn
				return core.Scroll{Style: core.Style{MaxHeight: core.Pt(120)}, Children: []core.Node{t}}
			}},
		}}
	})
}
