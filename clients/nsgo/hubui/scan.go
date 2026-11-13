package hubui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func (s *State) scan(_ *core.Scope) { s.scanned = nil; s.dialog.Set("scan") }
func (s *State) scanView(cx *core.Scope, close func()) core.Node {
	errorText := core.NewSignal(cx, "")
	active := core.NewSignal(cx, true)
	ref := &core.Ref{}
	cx.OnKey("escape", close)
	return base.Dialog{TestID: "dialog-scan", Label: "扫描设备二维码", Open: func() bool { return true }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			close()
		}
	}, PopupStyleFunc: func() core.Style {
		w := cx.Env().Window()
		p := palette(cx)
		return core.Style{Width: core.Pt(min(520, w.Width)), Height: core.Pt(w.Height), Background: p.background, PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvSafeArea}, Gap: 12, Padding: core.All(16)}
	}, Children: []core.Node{
		row(button(cx, "scan-close", "", lucide.ArrowLeft, "ghost", close), label(cx, "扫描设备二维码", 18, false)),
		label(cx, "将电脑上的 RAS 设备二维码放入画面中，将自动识别。", 13, true),
		core.View{Style: fill.Merge(core.Style{CornerRadius: 16, Overflow: core.Hidden, Background: core.RGB(0x000000)}), Children: []core.Node{
			core.Native{TestID: "live-qr-camera", Component: "ras.qr-camera", Ref: ref, PropsFunc: func() map[string]any { return map[string]any{"active": active.Get()} }, Style: fill, OnEvent: func(event string, value any) {
				if !active.GetUntracked() {
					return
				}
				switch event {
				case "code":
					if code, ok := value.(string); ok && s.acceptQR(code) {
						return
					}
					errorText.Set("不是有效的 RAS 设备二维码，请扫描电脑上的设备二维码。")
					ref.Command("retry", nil)
				case "error":
					if message, ok := value.(string); ok {
						errorText.Set(message)
					}
				}
			}},
		}},
		core.Show{When: func() bool { return errorText.Get() != "" }, Then: func(cx *core.Scope) core.Node { return dynamicText(cx, errorText.Get, 13, true) }},
		row(button(cx, "scan-gallery", "从相册识别", lucide.Image, "outline", func() {
			active.Set(false)
			s.scanPhoto(cx, func(message string) { errorText.Set(message); active.Set(true); ref.Command("retry", nil) })
		}), spacer(), button(cx, "scan-manual", "输入设备 ID", lucide.Keyboard, "ghost", func() { s.scanned = nil; s.dialog.Set("connect") })),
	}}
}
