package hubui

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

type tones struct {
	background, foreground, muted, mutedText, border  core.Color
	canvas, sidebar, elevated, selected, accent, code core.Color
}

func palette(cx *core.Scope) tones {
	p := cx.Theme().Light
	if cx.Dark() {
		p = cx.Theme().Dark
	}
	role := func(name string, fallback core.Color) core.Color {
		if c, ok := p.Colors[name]; ok {
			return c
		}
		return fallback
	}
	return tones{background: p.Background, foreground: p.Text, muted: p.Surface,
		mutedText: p.TextMuted, border: p.Border, canvas: role("canvas", p.Surface),
		sidebar: role("sidebar", p.Surface), elevated: role("elevated", p.Background),
		selected: role("selected", p.SurfacePressed), accent: p.Accent, code: role("codeSurface", p.Surface)}
}

var fill = core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)}

func row(nodes ...core.Node) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: nodes}
}
func leftButton(node core.Node) core.Node {
	v := node.(core.View)
	v.Style.JustifyContent = core.JustifyStart
	return v
}
func spacer() core.Node { return core.View{Style: core.Style{FlexGrow: 1}} }
func label(cx *core.Scope, value string, size core.Points, muted bool) core.Text {
	return core.Text{Content: value, Style: core.Style{FontSize: size, LineHeight: size * 1.5}, StyleFunc: func() core.Style {
		c := palette(cx).foreground
		if muted {
			c = palette(cx).mutedText
		}
		return core.Style{Color: c}
	}}
}
func singleLine(cx *core.Scope, value func() string, size core.Points, muted bool) core.Text {
	t := dynamicText(cx, value, size, muted)
	t.MaxLines = 1
	t.Style.FlexShrink = 1
	t.Style.MinWidth = core.Pt(0)
	return t
}
func dynamicText(cx *core.Scope, value func() string, size core.Points, muted bool) core.Text {
	t := label(cx, "", size, muted)
	t.ContentFunc = value
	return t
}
func glyph(cx *core.Scope, icon func() *core.SVG, size core.Points) core.Node {
	return core.Icon{SVG: icon(), Style: core.Style{Width: core.Pt(size), Height: core.Pt(size), FontSize: size, FlexShrink: 0}, StyleFunc: func() core.Style { return core.Style{Color: palette(cx).foreground} }}
}

func button(cx *core.Scope, id, title string, icon func() *core.SVG, variant string, onPress func()) core.Node {
	nodes := []core.Node{}
	if icon != nil {
		nodes = append(nodes, core.Icon{SVG: icon(), Style: core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0}})
	}
	if title != "" {
		t := core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 13, LineHeight: 18, FlexShrink: 1, MinWidth: core.Pt(0)}}
		nodes = append(nodes, t)
	}
	return core.View{TestID: id, Role: core.RoleButton, Label: title, OnPress: func(core.PressEvent) {
		if onPress != nil {
			onPress()
		}
	}, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 8, Padding: core.Symmetric(8, 12), MinHeight: core.Pt(36), CornerRadius: 10, FlexShrink: 0}, StyleFunc: func() core.Style {
		c := palette(cx)
		st := core.Style{Color: c.foreground, Hover: &core.Style{Background: c.muted}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: c.mutedText}}
		if variant == "outline" {
			st.BorderWidth = 1
			st.BorderColor = c.border
			st.Background = c.background
		}
		if variant == "primary" {
			st.Background = c.accent
			st.Color = cx.Theme().Light.AccentText
			if cx.Dark() {
				st.Color = cx.Theme().Dark.AccentText
			}
			st.Hover = &core.Style{Opacity: core.Alpha(.88)}
		}
		return st
	}, Children: nodes}
}
func divider(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Width: core.Pt(1), Height: core.Pct(100), FlexShrink: 0}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).border} }}
}

func (s *State) copyButton(cx *core.Scope, id, title, variant string, value func() string) core.Node {
	copied := core.NewMemo(cx, func() bool {
		s.read()
		return s.copiedValue != "" && s.copiedValue == value()
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		text, icon := title, lucide.Copy
		if copied.Get() {
			icon = lucide.Check
			if title != "" {
				text = "已复制"
			}
		}
		return button(cx, id, text, icon, variant, func() { s.copy(value()) })
	})
}
func heading(cx *core.Scope, title, subtitle string) core.Node {
	t := label(cx, title, 24, false)
	t.Style.FontWeight = core.FontWeightSemibold
	return core.View{Style: core.Style{Gap: 6}, Children: []core.Node{t, label(cx, subtitle, 13, true)}}
}
func card(cx *core.Scope, nodes ...core.Node) core.Node {
	return core.View{Style: core.Style{Padding: core.All(24), Gap: 14, CornerRadius: 16, BorderWidth: 1}, StyleFunc: func() core.Style {
		return core.Style{Background: palette(cx).elevated, BorderColor: palette(cx).border}
	}, Children: nodes}
}

func Root(cx *core.Scope) core.Node {
	api := platformAPI(cx)
	if demoRequested() {
		api = newDemoAPI()
	}
	return RootWithAPI(cx, api, true)
}
func RootWithAPI(cx *core.Scope, api API, start bool) core.Node {
	s := newState(cx, api)
	s.loadAppearance()
	s.installWorkspaceNavigation()
	s.observeNativeBackground()
	if start {
		s.initialize()
	} else {
		s.ready = true
	}
	page := core.NewMemo(cx, func() string {
		s.read()
		width := s.width.Get()
		mode := "wide"
		if width < compactBreakpoint {
			mode = "phone"
		} else if width < wideBreakpoint {
			mode = "tablet"
		}
		if mode == "phone" {
			return mode
		}
		return fmt.Sprintf("%s|%s|%s|%t", mode, s.target, str(s.selected, "id"), s.narrowChat)
	})
	return s.windowFrame(cx, core.View{TestID: "agenthub", Style: fill.Merge(core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvSafeArea}}), StyleFunc: func() core.Style {
		return core.Style{Background: palette(cx).canvas, Color: palette(cx).foreground, FontSize: 13}
	}, OnLayout: func(b core.Bounds) { s.width.Set(b.Size.Width) }, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			_ = page.Get()
			width := s.width.GetUntracked()
			if width < compactBreakpoint {
				return s.phonePages(cx)
			}
			nodes := []core.Node{s.navigation(cx, false)}
			if width >= compactBreakpoint {
				nodes = append(nodes, s.resize(cx, true))
			}
			var body core.Node
			if s.target == "" {
				if api.CanHost() {
					body = s.local(cx)
				} else {
					body = s.welcome(cx)
				}
			} else if width >= wideBreakpoint {
				body = core.View{Style: fill.Merge(core.Style{Direction: core.Row}), Children: []core.Node{core.View{StyleFunc: func() core.Style {
					return core.Style{Width: core.Pt(s.sessionWidth.Get()), MinHeight: core.Pt(0), MinWidth: core.Pt(0)}
				}, Children: []core.Node{s.directory(cx)}}, s.resize(cx, false), s.conversation(cx, false)}}
			} else if s.narrowChat {
				body = s.conversation(cx, true)
			} else {
				body = s.directory(cx)
			}
			nodes = append(nodes, core.View{Style: fill, Children: []core.Node{body}})
			return core.View{Style: fill.Merge(core.Style{Direction: core.Row}), Children: nodes}
		}),
		core.Show{When: func() bool { s.read(); return s.error != "" && !s.errorAtComposer() }, Then: func(cx *core.Scope) core.Node {
			return s.errorOverlay(cx, false)
		}},
		core.Show{When: func() bool { s.read(); return s.notice != "" }, Then: func(cx *core.Scope) core.Node {
			return core.View{TestID: "notice", Style: core.Style{Position: core.Absolute, Bottom: core.Pt(16), Right: core.Pt(20), Padding: core.Symmetric(10, 16), CornerRadius: 8, BorderWidth: 1, ZIndex: 10}, StyleFunc: func() core.Style {
				c := palette(cx)
				return core.Style{Background: c.background, BorderColor: c.border}
			}, Children: []core.Node{dynamicText(cx, func() string { s.read(); return s.notice }, 12, false)}}
		}},
		core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) }),
	}})
}

func (s *State) resize(cx *core.Scope, devices bool) core.Node {
	w := s.sessionWidth
	if devices {
		w = s.deviceWidth
	}
	var initial core.Points
	return core.View{TestID: fmt.Sprint("resize-", devices), Style: core.Style{Width: core.Pt(5), FlexShrink: 0, AlignItems: core.AlignCenter}, Class: "cursor-col-resize", OnPan: func(e core.PanEvent) {
		if e.Phase == core.GestureBegan {
			initial = w.Get()
		}
		lo, hi := core.Points(220), core.Points(400)
		if devices {
			lo, hi = 175, 300
		}
		w.Set(min(hi, max(lo, initial+e.Translation.X)))
	}, Children: []core.Node{core.View{Style: core.Style{Width: core.Pt(1), Height: core.Pct(100)}, StyleFunc: func() core.Style { return core.Style{Hover: &core.Style{Background: palette(cx).border}} }}}}
}
func (s *State) navigation(cx *core.Scope, compact bool) core.Node {
	openConnect := func() { s.dialog.Set("connect") }
	nodes := []core.Node{}
	if compact {
		nodes = append(nodes, button(cx, "home", "", lucide.Command, "ghost", func() { s.selectDevice("") }), button(cx, "connect-device", "", lucide.Plus, "ghost", openConnect))
	} else {
		nodes = append(nodes, core.View{Style: core.Style{Padding: core.Symmetric(20, 8)}, Children: []core.Node{s.brand(cx, 19, true)}})
		if s.api.CanHost() {
			nodes = append(nodes, button(cx, "home", "本机", lucide.Monitor, "outline", func() { s.selectDevice("") }))
		}
		nodes = append(nodes, button(cx, "connect-device", "连接设备", lucide.Plus, "primary", openConnect))
	}
	if isMobile(cx) {
		title := "扫描二维码"
		if compact {
			title = ""
		}
		nodes = append(nodes, button(cx, "scan-qr", title, lucide.ScanLine, "ghost", func() { s.scan(cx) }))
	}
	if !compact {
		nodes = append(nodes, core.View{Style: core.Style{Padding: core.Edges{Top: 16, Left: 8, Bottom: 4}}, Children: []core.Node{label(cx, "已保存设备", 12, true)}})
	}
	nodes = append(nodes, core.Scroll{Style: fill, Children: []core.Node{core.Each(func() []Object { s.read(); return s.devices }, func(d Object) string { return str(d, "id") }, func(cx *core.Scope, d func() Object, _ func() int) core.Node {
		if compact {
			return button(cx, "device-"+str(d(), "id"), "", lucide.Monitor, "ghost", func() { s.openDevice(str(d(), "id")) })
		}
		return core.View{TestID: "device-" + str(d(), "id"), Role: core.RoleButton, LabelFunc: func() string { return deviceName(d()) }, OnPress: func(core.PressEvent) { s.openDevice(str(d(), "id")) }, Style: core.Style{Margin: core.Edges{Bottom: 6}, Padding: core.All(12), Gap: 5, CornerRadius: 12}, StyleFunc: func() core.Style {
			c := palette(cx)
			bg := c.sidebar
			s.read()
			if s.target == str(d(), "id") {
				bg = c.selected
			}
			return core.Style{Background: bg, BorderColor: c.border, Hover: &core.Style{Background: c.muted}}
		}, Children: []core.Node{row(core.View{Style: fill, Children: []core.Node{dynamicText(cx, func() string { return deviceName(d()) }, 13, false)}}, s.devicePresenceIndicator(cx, d)), dynamicText(cx, func() string { return s.deviceListStatus(d()) }, 10, true)}}
	})}})
	nodes = append(nodes, core.Show{When: func() bool { s.read(); return s.connectionProblem("") }, Then: func(cx *core.Scope) core.Node { return s.connectionIndicator(cx, func() string { return "" }) }}, button(cx, "account-menu", choose(compact, "", "账号与设备同步"), lucide.User, "ghost", func() { s.dialog.Set("account") }))
	themeButton := button(cx, "theme-toggle", "", lucide.SunMoon, "ghost", s.toggleTheme).(core.View)
	themeButton.Label = "切换主题"
	themeButton.Tooltip = label(cx, "切换主题", 12, false)
	nodes = append(nodes, row(leftButton(button(cx, "server-settings", choose(compact, "", "设置"), lucide.Settings, "ghost", func() { s.openSettings("general") })), spacer(), themeButton))
	return core.View{TestID: "device-sidebar", Style: core.Style{Padding: core.All(14), Gap: 8, FlexShrink: 0}, StyleFunc: func() core.Style {
		w := s.deviceWidth.Get()
		if compact {
			w = 60
		}
		c := palette(cx)
		return core.Style{Width: core.Pt(w), Background: c.sidebar, Color: c.foreground}
	}, Children: nodes}
}
func choose[T any](condition bool, a, b T) T {
	if condition {
		return a
	}
	return b
}
func isMobile(cx *core.Scope) bool {
	return cx.Env().OS() == core.OS("ios") || cx.Env().OS() == core.OS("android")
}

func (s *State) local(cx *core.Scope) core.Node {
	copyID := s.copyButton(cx, "copy-device-id", "复制 ID", "outline", func() string { return str(s.host, "connectionId") })
	return core.Scroll{TestID: "local-dashboard", Style: fill, StyleFunc: func() core.Style { return s.workspaceSurface(cx) }, Children: []core.Node{core.View{Style: core.Style{Padding: core.All(32), Gap: 18}, Children: []core.Node{
		heading(cx, "这台电脑", "允许已授权设备访问本机 Agent，官方应用仍可照常使用。"),
		card(cx, row(glyph(cx, lucide.Monitor, 22), dynamicText(cx, func() string {
			s.read()
			name := str(s.host, "name")
			if name == "" {
				name = "本机"
			}
			return name
		}, 14, false), spacer(), dynamicText(cx, func() string { s.read(); return str(s.host, "status") }, 12, true), s.connectionIndicator(cx, func() string { return "" })), label(cx, "你的设备 ID", 12, true), core.Text{TestID: "device-code", ContentFunc: func() string {
			s.read()
			v := str(s.host, "connectionId")
			if len(v) == 6 {
				return v[:3] + " " + v[3:]
			}
			return "尚未登记"
		}, Selectable: core.SelectableOn, Style: core.Style{FontSize: 32, FontWeight: core.FontWeightSemibold, LetterSpacing: 2}}, row(copyID, button(cx, "show-qr", "设备二维码", nil, "outline", func() { s.dialog.Set("qr") }), button(cx, "configure-service", "配置服务", nil, "outline", func() { s.openSettings("connection") }))),
		card(cx, label(cx, "访问授权", 14, false), label(cx, "首次连接需本机确认；配置密码后，输入正确密码可自动授权。", 13, true), row(button(cx, "set-password", "设置访问密码", nil, "outline", func() { s.dialog.Set("password") }), core.Show{When: func() bool { s.read(); return flag(s.host, "passwordEnabled") }, Then: func(cx *core.Scope) core.Node {
			return button(cx, "clear-password", "关闭密码访问", nil, "ghost", func() { s.call("host.password", Object{"clearPassword": true}, nil) })
		}}), core.Each(func() []Object { s.read(); return objects(s.host["grants"]) }, func(g Object) string { return str(g, "client") }, func(cx *core.Scope, g func() Object, _ func() int) core.Node {
			return row(controllerIdentity(cx, g), spacer(), button(cx, "revoke-"+str(g(), "client"), "撤销授权", nil, "ghost", func() { s.call("host.revoke", Object{"client": g()["client"]}, nil) }))
		})),
		card(cx, row(glyph(cx, lucide.Bot, 22), core.View{Style: fill, Children: []core.Node{label(cx, "本机 Agent", 14, false), label(cx, "管理已安装的 Agent 与自定义接入。", 13, true)}}, button(cx, "manage-agents", "管理 Agent", lucide.ArrowRight, "outline", func() { s.openSettings("agents") }))),
	}}}}
}
func (s *State) welcome(cx *core.Scope) core.Node {
	return core.View{Style: fill.Merge(core.Style{AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(32), Gap: 20}), StyleFunc: func() core.Style { return s.workspaceSurface(cx) }, Children: []core.Node{glyph(cx, lucide.Network, 52), label(cx, "你的 Agent，随处连接", 26, false), label(cx, "输入设备 ID，连接电脑上的 Codex 与其它 Agent。", 13, true), button(cx, "welcome-connect", "连接你的电脑", nil, "primary", func() { s.dialog.Set("connect") })}}
}

type directoryRow struct {
	ID, Title, Path string
	Session         Object
	Header, More    bool
}

func (s *State) directoryRows() []directoryRow {
	s.read()
	rows := []directoryRow{}
	for _, g := range directories(s.sessions) {
		rows = append(rows, directoryRow{ID: "dir:" + g.Path, Title: g.Title, Path: g.Path, Header: true})
		if s.collapsed[s.target+"|"+g.Path] {
			continue
		}
		visible := g.Sessions
		if !s.expanded[s.target+"|"+g.Path] && len(visible) > 5 {
			visible = visible[:5]
		}
		for _, ss := range visible {
			rows = append(rows, directoryRow{ID: str(ss, "providerId") + "|" + str(ss, "id"), Title: str(ss, "title"), Path: g.Path, Session: ss})
		}
		if len(g.Sessions) > 5 {
			rows = append(rows, directoryRow{ID: "more:" + g.Path, Path: g.Path, More: true})
		}
	}
	return rows
}
func (s *State) directory(cx *core.Scope) core.Node {
	// Connection timers and streaming text do not change the session catalogue.
	// Avoid regrouping/sorting it on every unrelated revision while scrolling.
	type catalogueStamp struct {
		Target  string
		Version uint64
		Count   int
	}
	stamp := core.NewMemo(cx, func() catalogueStamp {
		s.read()
		return catalogueStamp{s.target, s.catalogueVersion, len(s.sessions)}
	})
	groups := core.NewSignal(cx, 0)
	rows := core.NewMemoFunc(cx, func() []directoryRow {
		stamp.Get()
		groups.Get()
		return core.Untracked(cx, s.directoryRows)
	}, nil)
	brake := &listTouchBrake{enabled: !isWeb() && cx.Env().OS() == core.OSAndroid}
	return core.View{TestID: "session-sidebar", Style: fill, StyleFunc: func() core.Style {
		c := palette(cx)
		return core.Style{Background: c.sidebar, Color: c.foreground}
	}, Children: []core.Node{
		core.View{Style: core.Style{Padding: core.Edges{Top: 20, Left: 18, Right: 12, Bottom: 6}}, Children: []core.Node{row(core.Show{When: func() bool { return s.width.Get() < compactBreakpoint }, Then: func(cx *core.Scope) core.Node {
			return button(cx, "back-devices", "设备", lucide.ArrowLeft, "ghost", s.backDevices)
		}}, core.View{Style: fill, Children: []core.Node{singleLine(cx, func() string { s.read(); return deviceName(s.device()) }, 14, false), singleLine(cx, func() string { s.read(); return s.deviceListStatus(s.device()) }, 11, true)}}, s.connectionIndicator(cx, func() string { return s.target }), button(cx, "refresh-sessions", "", lucide.RefreshCw, "ghost", s.poll))}},
		core.View{Style: core.Style{Padding: core.Symmetric(6, 12)}, Children: []core.Node{leftButton(button(cx, "new-session", "新聊天", lucide.SquarePen, "ghost", func() { s.createDirectory = ""; s.dialog.Set("create") }))}},
		core.View{Style: core.Style{Padding: core.Edges{Left: 22, Top: 16, Right: 16}}, Children: []core.Node{label(cx, "项目", 12, true)}},
		s.emptyCatalogue(cx),
		brake.wrap(core.List{TestID: "session-list", Style: fill, Count: func() int { return len(rows.Get()) }, Key: func(i int) any { return rows.Get()[i].ID }, EstimatedSize: 38, Row: func(cx *core.Scope, r core.ListRow) core.Node {
			item := func() directoryRow {
				v := rows.Get()
				i := r.Index()
				if i >= 0 && i < len(v) {
					return v[i]
				}
				return directoryRow{}
			}
			key := core.NewMemo(cx, func() string { return item().ID })
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = key.Get()
				data := core.Untracked(cx, item)
				groupKey := s.target + "|" + data.Path
				if data.More {
					return core.View{Style: core.Style{Padding: core.Edges{Left: 43, Right: 12}}, Children: []core.Node{leftButton(button(cx, "more-"+data.Path, choose(s.expanded[groupKey], "收起", "展开显示"), nil, "ghost", brake.action(func() {
						s.expanded[groupKey] = !s.expanded[groupKey]
						groups.Update(func(v int) int { return v + 1 })
						s.changed()
					})))}}
				}
				if data.Header {
					folder := core.Icon{SVGFunc: func() *core.SVG {
						s.read()
						if s.collapsed[groupKey] {
							return lucide.Folder()
						}
						return lucide.FolderOpen()
					}, Style: core.Style{Width: core.Pt(18), Height: core.Pt(18)}}
					return core.View{TestID: data.ID, Style: core.Style{Padding: core.Edges{Top: 18, Bottom: 3, Left: 10, Right: 10}}, Children: []core.Node{row(core.View{Role: core.RoleButton, Label: data.Title, OnPress: func(core.PressEvent) {
						brake.action(func() {
							s.collapsed[groupKey] = !s.collapsed[groupKey]
							groups.Update(func(v int) int { return v + 1 })
							s.changed()
						})()
					}, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Padding: core.Symmetric(8, 12)}, Children: []core.Node{folder, singleLine(cx, func() string { return item().Title }, 14, false)}}, button(cx, "directory-info-"+data.Path, "", lucide.Ellipsis, "ghost", brake.action(func() { s.directoryInfo = data.Path; s.dialog.Set("directory") })), button(cx, "directory-create-"+data.Path, "", lucide.SquarePen, "ghost", brake.action(func() { s.createDirectory = data.Path; s.dialog.Set("create") })))}}
				}
				return core.View{TestID: "session-" + str(data.Session, "id"), Role: core.RoleButton, LabelFunc: func() string { return item().Title }, OnPress: func(core.PressEvent) { brake.action(func() { s.selectSession(item().Session) })() }, Style: core.Style{MinHeight: core.Pt(36), Padding: core.Edges{Top: 8, Bottom: 8, Left: 34, Right: 12}, Margin: core.Symmetric(1, 10), CornerRadius: 12}, StyleFunc: func() core.Style {
					s.read()
					c := palette(cx)
					st := core.Style{Hover: &core.Style{Background: c.muted}}
					if str(s.selected, "id") == str(item().Session, "id") && str(s.selected, "providerId") == str(item().Session, "providerId") {
						st.Background = c.selected
					}
					return st
				}, Children: []core.Node{row(core.Icon{SVG: sessionIcon(data.Session)(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)}, StyleFunc: func() core.Style { return core.Style{Color: palette(cx).mutedText} }}, core.Text{ContentFunc: func() string { return item().Title }, MaxLines: 1, Style: core.Style{FontSize: 13, FlexShrink: 1, MinWidth: core.Pt(0)}})}}
			})
		}}),
		core.View{Style: core.Style{Padding: core.All(8)}, Children: []core.Node{row(button(cx, "reconnect-device", "重连", nil, "ghost", func() { s.reconnectDevice(s.target) }), button(cx, "disconnect-device", "断开", nil, "ghost", func() { s.call("client.disconnect", Object{"id": s.target}, nil) }), spacer(), button(cx, "forget-device", "", lucide.Trash, "ghost", func() { s.dialog.Set("forget") }))}},
	}}
}
func sessionIcon(s Object) func() *core.SVG {
	if strings.Contains(str(s, "providerId"), "claude") {
		return lucide.Sparkles
	}
	if str(s, "sourceKind") == "app" || str(s, "providerId") == "codex-app" {
		return lucide.Monitor
	}
	return lucide.Terminal
}
