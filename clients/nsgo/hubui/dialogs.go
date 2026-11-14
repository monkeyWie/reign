package hubui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/liyue201/goqr"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	qrcode "github.com/skip2/go-qrcode"
)

func (s *State) dialogView(cx *core.Scope, kind string) core.Node {
	if kind == "connecting" {
		mode := core.NewMemo(cx, func() string {
			s.read()
			if s.connectAttempt == nil {
				return ""
			}
			return fmt.Sprintf("%p|%t", s.connectAttempt, s.connectAttempt.active)
		})
		return core.Dynamic(func(cx *core.Scope) core.Node { _ = mode.Get(); return s.connectAttemptView(cx) })
	}
	if kind == "" {
		return nil
	}
	close := func() { s.dialog.Set("") }
	title := ""
	children := []core.Node{}
	footer := []core.Node{button(cx, "dialog-cancel", "取消", nil, "outline", close)}
	field := func(id, placeholder string, value *core.Signal[string], masked bool) core.Node {
		return ui.Input{TestID: id, Placeholder: placeholder, Label: placeholder, Value: value.Get, OnChange: value.Set, Masked: masked, Style: core.Style{MinHeight: core.Pt(40), FontSize: 13}}
	}
	d := ui.Dialog{TestID: "dialog-" + strings.Split(kind, ":")[0], Open: func() bool { return true }, OnOpenChange: func(v bool, _ base.Reason) {
		if !v {
			close()
		}
	}, Style: core.Style{Width: core.Pt(448), MaxWidth: core.Pct(96)}}
	switch {
	case kind == "connection-status":
		id := s.connectionDetailTarget
		title = "连接状态"
		children = append(children, dynamicText(cx, func() string {
			s.read()
			for _, d := range s.devices {
				if str(d, "id") == id {
					return deviceName(d) + " · " + deviceStatus(d)
				}
			}
			return "服务连接"
		}, 13, false), dynamicText(cx, func() string {
			s.read()
			if s.connection(id).reconnecting {
				return "正在自动恢复连接，草稿和已缓存的消息会保留。"
			}
			if s.connectionProblem(id) {
				return "连接暂时不可用或同步异常。恢复后会自动同步最新消息。"
			}
			return "连接正常，消息会持续同步。"
		}, 12, true), core.Show{When: func() bool { s.read(); return s.connectionDetails(id) != "" }, Then: func(cx *core.Scope) core.Node {
			raw := dynamicText(cx, func() string { s.read(); return s.connectionDetails(id) }, 11, true)
			raw.TestID = "connection-error-raw"
			raw.Selectable = core.SelectableOn
			return core.Scroll{Style: core.Style{MaxHeight: core.Pt(180)}, Children: []core.Node{raw}}
		}})
		footer = []core.Node{button(cx, "connection-done", "关闭", nil, "outline", close)}
		if id != "" {
			footer = append(footer, button(cx, "connection-retry", "重新连接", lucide.RefreshCw, "primary", func() { s.reconnectDevice(id) }))
		}
	case kind == "account":
		title = "账号与设备同步"
		prefix := "client.account."
		origin := serviceOrigin()
		if s.api.CanHost() && str(s.host, "serviceUrl") != "" {
			origin = str(s.host, "serviceUrl")
		}
		service := core.NewSignal(cx, origin)
		params := Object{"service": service.Get(), "name": s.clientName}
		if !s.api.CanHost() {
			children = append(children, field("account-service", "服务地址", service, false))
		}
		if s.api.CanHost() {
			prefix = "host.account."
		}
		children = append(children, label(cx, "登录后可同步设备。首次连接仍需主机确认或访问密码；自建开源服务无需登录。", 13, true))
		if s.accountLogin != nil {
			children = append(children, label(cx, "授权码："+str(s.accountLogin, "code"), 17, false), s.copyButton(cx, "copy-login-code", "复制授权码", "outline", func() string { return str(s.accountLogin, "code") }), button(cx, "open-login-page", "在浏览器中打开登录页面", nil, "outline", func() { cx.OpenURL(str(s.accountLogin, "url"), nil) }))
		}
		if s.accountStatus != nil {
			children = append(children, label(cx, choose(flag(s.accountStatus, "linked"), "账号已绑定 · "+str(s.accountStatus, "plan"), "尚未绑定账号"), 13, false))
		}
		for _, entry := range s.accountDevices {
			d := entry
			children = append(children, button(cx, "account-device-"+str(d, "id"), str(d, "name")+" · "+str(d, "connectionId"), nil, "outline", func() {
				close()
				s.connect(Object{"service": service.Get(), "code": str(d, "connectionId"), "public": str(d, "public"), "remember": true, "name": s.clientName})
			}))
		}
		footer = append(footer, button(cx, "account-start", "登录与绑定", nil, "primary", func() {
			params["service"] = service.Get()
			s.call(prefix+"start", params, func(v any) { s.accountLogin = object(v); cx.OpenURL(str(s.accountLogin, "url"), nil) })
		}), button(cx, "account-refresh", "刷新账号设备", nil, "outline", func() {
			if s.accountLogin != nil {
				p := Object{"service": serviceOrigin(), "name": s.clientName, "id": str(s.accountLogin, "id")}
				s.call(prefix+"poll", p, nil)
			}
			params["service"] = service.Get()
			s.call(prefix+"status", params, func(v any) { s.accountStatus = object(v) })
			s.call(prefix+"devices", params, func(v any) { s.accountDevices = objects(object(v)["devices"]) })
		}))
	case kind == "session-access-help":
		help := sessionAccessHelp(s.error, s.failure)
		if help.Title == "" {
			return nil
		}
		title = "接入 Codex 后继续会话"
		for i, step := range help.Steps {
			children = append(children, label(cx, fmt.Sprintf("%d. %s", i+1, step), 13, false))
		}
		children = append(children, label(cx, "关闭这份指引不会执行重启，也不会自动重发消息。", 12, true))
		footer = []core.Node{button(cx, "session-help-done", "知道了", nil, "primary", close)}
	case kind == "connect":
		title = "连接设备"
		service := core.NewSignal(cx, serviceOrigin())
		code := core.NewSignal(cx, "")
		password := core.NewSignal(cx, "")
		remember := core.NewSignal(cx, true)
		relay := core.NewSignal(cx, false)
		if s.scanned != nil {
			service.Set(str(s.scanned, "service"))
			initialCode := str(s.scanned, "connectionId")
			if initialCode == "" {
				initialCode = str(s.scanned, "code")
			}
			code.Set(initialCode)
		}
		if !isWeb() {
			children = append(children, field("connection-service", "服务地址（HTTP / HTTPS）", service, false))
		}
		children = append(children, field("connection-code", "设备 ID，例如 123 456", code, false), field("connection-password", "访问密码（留空则请求主机确认）", password, true), ui.Checkbox{TestID: "remember-device", Text: "记住此设备授权", Checked: remember.Get, OnChange: remember.Set}, ui.Checkbox{TestID: "relay-only", Text: "仅用中继（用于诊断）", Checked: relay.Get, OnChange: relay.Set}, label(cx, "当前设备会保存配对身份与会话缓存，请仅在可信设备上使用。", 12, true))
		footer = append(footer, button(cx, "connect-submit", "连接", nil, "primary", func() {
			p := Object{"service": strings.TrimRight(strings.TrimSpace(service.Get()), "/"), "code": strings.NewReplacer(" ", "", "-", "").Replace(code.Get()), "password": password.Get(), "remember": remember.Get(), "relayOnly": relay.Get(), "name": s.clientName}
			if s.scanned != nil {
				p["public"] = s.scanned["public"]
			}
			s.scanned = nil
			close()
			s.connect(p)
		}))
	case kind == "settings" || kind == "password":
		url := str(s.host, "serviceUrl")
		if url == "" {
			url = serviceOrigin()
		}
		service := core.NewSignal(cx, url)
		name := core.NewSignal(cx, str(s.host, "name"))
		password := core.NewSignal(cx, "")
		if kind == "password" {
			title = "设置访问密码"
			children = append(children, field("access-password", "8–72 字节的访问密码", password, true))
		} else {
			title = "本机服务设置"
			children = append(children, button(cx, "account-settings", "账号与设备同步", nil, "outline", func() { s.dialog.Set("account") }))
			children = append(children, field("server-url", "服务地址", service, false), field("device-name", "设备名称", name, false), label(cx, "优先尝试 P2P 直连；允许中继时自动回退到所选服务的 WSS 中继。", 12, true))
		}
		footer = append(footer, button(cx, "dialog-save", "保存", nil, "primary", func() {
			close()
			if kind == "password" {
				s.call("host.password", Object{"password": password.Get()}, nil)
			} else {
				s.call("host.configure", Object{"serviceUrl": strings.TrimSpace(service.Get()), "name": strings.TrimSpace(name.Get())}, nil)
			}
		}))
	case kind == "qr":
		title = "扫描连接这台电脑"
		code := pairingCode(object(s.host["qr"]))
		png, e := qrcode.Encode(code, qrcode.Medium, 250)
		if e != nil {
			children = append(children, label(cx, e.Error(), 12, true))
		} else {
			children = append(children, core.Image{Source: core.ImageSource{Data: png}, ResizeMode: core.Contain, Style: core.Style{Width: core.Pt(250), Height: core.Pt(250), AlignSelf: core.AlignCenter, Background: core.RGB(0xffffff)}})
		}
		children = append(children, label(cx, "扫码后仍需主机确认或密码授权。", 12, true), s.copyButton(cx, "copy-pairing", "复制连接信息", "outline", func() string { return code }))
		footer = []core.Node{button(cx, "dialog-done", "完成", nil, "primary", close)}
	case strings.HasPrefix(kind, "authorize:"):
		title = "允许远程连接？"
		id := strings.TrimPrefix(kind, "authorize:")
		var pending Object
		for _, p := range objects(s.host["pending"]) {
			if str(p, "id") == id {
				pending = p
				break
			}
		}
		if pending == nil {
			return nil
		}
		d.NoCloseOnBackdropPress = true
		d.NoCloseOnEscape = true
		children = append(children, label(cx, str(pending, "name")+" 请求访问这台电脑上的 Agent。", 13, false), label(cx, "设备指纹："+str(pending, "fingerprint"), 11, true), label(cx, choose(flag(pending, "remember"), "允许后记住此控制设备，可在授权列表撤销。", "临时授权 8 小时，期间可再次连接。"), 12, true))
		footer = []core.Node{button(cx, "deny-host-request", "拒绝", nil, "outline", func() { close(); s.call("host.respond", Object{"id": id, "allow": false}, nil) }), button(cx, "approve-host-request", "允许连接", nil, "primary", func() { close(); s.call("host.respond", Object{"id": id, "allow": true}, nil) })}
	case strings.HasPrefix(kind, "activate:"):
		provider := strings.TrimPrefix(kind, "activate:")
		var p Object
		for _, v := range s.localProviders {
			if str(v, "id") == provider {
				p = v
				break
			}
		}
		title = "接入 " + str(p, "name")
		children = append(children, label(cx, "接入需要正常退出并重新打开官方 App。请先处理当前任务；不会强制结束进程。", 13, false))
		expected := p["instances"]
		footer = append(footer, button(cx, "confirm-activate", "确认接入", nil, "primary", func() {
			close()
			s.busy = true
			s.async(func(ctx context.Context) (any, error) {
				return s.api.Call(ctx, "host.activate", Object{"provider": provider}, "", Object{"expected": expected, "confirmRestart": true})
			}, func(any) { s.poll() })
		}))
	case kind == "create":
		title = "新建会话"
		cwd := core.NewSignal(cx, s.createDirectory)
		provider := core.NewSignal(cx, "")
		options := []ui.Option[string]{}
		for _, p := range s.providers {
			if providerReady(p) {
				options = append(options, ui.Option[string]{Value: str(p, "id"), Label: str(p, "name")})
			}
		}
		if len(options) > 0 {
			provider.Set(options[0].Value)
		}
		children = append(children, ui.Select[string]{TestID: "new-session-provider", Label: "Agent", Options: options, Value: provider.Get, OnChange: provider.Set}, field("new-session-cwd", "工作目录（被控电脑上的绝对路径）", cwd, false))
		footer = append(footer, button(cx, "create-session-confirm", "创建", nil, "primary", func() {
			target, p, directory := s.target, provider.Get(), cwd.Get()
			close()
			s.busy = true
			s.async(func(ctx context.Context) (any, error) {
				return rpc(ctx, s.api, target, p, "", "create", Object{"options": Object{"cwd": directory}})
			}, func(v any) {
				if s.target == target {
					s.selectSession(object(v))
					s.poll()
				}
			})
		}))
	case kind == "directory":
		title = "项目"
		count := 0
		for _, g := range directories(s.sessions) {
			if g.Path == s.directoryInfo {
				title = g.Title
				count = len(g.Sessions)
			}
		}
		children = append(children, label(cx, fmt.Sprintf("%d 次聊天", count), 13, true), core.Text{Content: s.directoryInfo, Selectable: core.SelectableOn, Style: core.Style{FontSize: 13}})
		footer = append(footer, s.copyButton(cx, "copy-directory", "复制路径", "outline", func() string { return s.directoryInfo }))
	case kind == "clear-cache":
		title = "清除这台设备的附件缓存？"
		children = append(children, label(cx, "仅清除控制端已下载的附件，下次查看时会从被控端重新读取。", 13, true))
		target := s.target
		footer = append(footer, button(cx, "confirm-clear-cache", "清除缓存", nil, "primary", func() {
			close()
			s.async(func(ctx context.Context) (any, error) {
				return s.api.Call(ctx, "client.assetCache.clear", Object{}, target, nil)
			}, func(any) { s.images = map[string][]byte{}; s.notice = "附件缓存已清除" })
		}))
	case kind == "forget":
		title = "移除设备？"
		children = append(children, label(cx, "删除本机保存的配对身份与该设备的消息、附件缓存。", 13, false))
		id := s.target
		footer = append(footer, button(cx, "confirm-forget", "移除", nil, "primary", func() { close(); s.call("client.forget", Object{"id": id}, func(any) { s.selectDevice("") }) }))
	case kind == "scan":
		return s.scanView(cx, close)
	case kind == "preview":
		return s.imagePreview(cx, close)
	default:
		return nil
	}
	d.Title = title
	d.Children = children
	d.Footer = footer
	return d
}
func (s *State) imagePreview(cx *core.Scope, close func()) core.Node {
	index := core.NewMemo(cx, func() int { return s.previewIndex.Get() })
	move := func(delta int) {
		if len(s.preview) > 0 {
			s.previewIndex.Set((index.Get() + delta + len(s.preview)) % len(s.preview))
		}
	}
	cx.OnKey("left", func() { move(-1) })
	cx.OnKey("right", func() { move(1) })
	toolbar := row(button(cx, "preview-close", "", lucide.X, "ghost", close), spacer(), dynamicText(cx, func() string { return fmt.Sprintf("%d / %d", index.Get()+1, len(s.preview)) }, 12, true), button(cx, "preview-download", "下载", lucide.Download, "ghost", func() {
		if len(s.preview) > 0 {
			s.download(cx, s.preview[index.Get()])
		}
	}))
	content := core.Dynamic(func(cx *core.Scope) core.Node {
		if len(s.preview) == 0 {
			return nil
		}
		a := s.preview[index.Get()]
		s.loadImage(a, false)
		key := s.key() + "|" + str(a, "id") + "|false"
		img := core.Image{TestID: "preview-image", SourceFunc: func() core.ImageSource { s.read(); return core.ImageSource{Data: s.images[key]} }, ResizeMode: core.Contain, Style: fill.Merge(core.Style{AlignSelf: core.AlignStretch})}
		return core.View{Style: fill.Merge(core.Style{Direction: core.Row, AlignItems: core.AlignCenter}), Children: []core.Node{button(cx, "preview-previous", "", lucide.ChevronLeft, "ghost", func() { move(-1) }), img, button(cx, "preview-next", "", lucide.ChevronRight, "ghost", func() { move(1) })}}
	})
	return base.Dialog{TestID: "image-preview", Label: "图片预览", Open: func() bool { return true }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			close()
		}
	}, BackdropStyle: core.Style{Background: core.RGB(0x000000).Faded(0.7)}, PopupStyleFunc: func() core.Style {
		window := cx.Env().Window()
		p := palette(cx)
		return core.Style{Width: core.Pt(min(1000, max(0, window.Width-24))), Height: core.Pt(window.Height * 0.86), Padding: core.All(16), Background: p.background, BorderColor: p.border, BorderWidth: 1, CornerRadius: 16, Overflow: core.Hidden}
	}, Children: []core.Node{toolbar, content}}
}

func (s *State) scanPhoto(cx *core.Scope, done func(string)) {
	accept := func(files []core.PickedFile) {
		if len(files) == 0 {
			done("")
			return
		}
		data := files[0].Data
		go func() {
			img, _, e := image.Decode(bytes.NewReader(data))
			if e != nil {
				cx.Post(func() { done("无法读取二维码图片") })
				return
			}
			codes, e := goqr.Recognize(img)
			cx.Post(func() {
				if e != nil || len(codes) == 0 {
					done("没有识别到设备二维码")
					return
				}
				for _, c := range codes {
					if s.acceptQR(string(c.Payload)) {
						return
					}
				}
				done("不是 ReignHub 设备二维码")
			})
		}()
	}
	cx.PickImages(core.PickOptions{Limit: 1}, accept)
}
func (s *State) acceptQR(code string) bool {
	if !strings.HasPrefix(code, "agenthub2:") {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "agenthub2:"))
	if e != nil {
		return false
	}
	v := Object{}
	if json.Unmarshal(b, &v) != nil || str(v, "service") == "" || str(v, "connectionId") == "" || str(v, "public") == "" {
		return false
	}
	s.scanned = v
	s.dialog.Set("connect")
	return true
}
