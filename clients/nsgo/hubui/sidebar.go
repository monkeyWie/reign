package hubui

import (
	"encoding/base64"
	"fmt"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/base"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/i18n"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func (s *State) manageDevice(id string) {
	s.deviceMenuTarget = id
	if id == control.LocalTarget {
		s.dialog.Set("local")
		return
	}
	s.dialog.Set("device")
}
func (s *State) sidebarDevices(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Padding: core.Symmetric(10, 12), Gap: 4}, Children: []core.Node{
		row(s.brand(cx, 18, true), spacer(), labelledIconButton(cx, "connect-device", locales.For(s.cx).AddDevice(), lucide.Plus, func() { s.scanned = nil; s.dialog.Set("connect") })),
		core.Scroll{TestID: "sidebar-devices", Style: core.Style{MaxHeight: core.Pt(148), MinHeight: core.Pt(0)}, Children: []core.Node{
			core.Each(func() []Object { s.read(); return s.devices }, func(d Object) string { return str(d, "id") }, func(cx *core.Scope, d func() Object, _ func() int) core.Node {
				id := str(d(), "id")
				return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
					core.View{TestID: "device-" + id, Role: core.RoleButton, LabelFunc: func() string {
						if flag(d(), "local") {
							return locales.For(s.cx).LocalDevice()
						}
						return deviceName(d())
					}, OnPress: func(core.PressEvent) { s.manageDevice(id) }, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Symmetric(8, 8), CornerRadius: 8}, StyleFunc: func() core.Style { return pressStyle(cx) }, Children: []core.Node{
						glyph(cx, choose(flag(d(), "local"), lucide.Laptop, lucide.Monitor), 15),
						singleLine(cx, func() string {
							if flag(d(), "local") {
								return locales.For(s.cx).LocalDevice()
							}
							return deviceName(d())
						}, 12, false), spacer(),
						dynamicText(cx, func() string {
							s.read()
							if flag(d(), "local") {
								return locales.For(s.cx).LocalExecution()
							}
							if s.connectionRecovering(id) {
								return "连接中"
							}
							return s.deviceListStatus(d())
						}, 11, true),
					}},
					core.Show{When: func() bool { return !flag(d(), "local") }, Then: func(cx *core.Scope) core.Node {
						return s.connectionIndicator(cx, func() string { return id }, "sidebar-")
					}},
				}}
			}),
		}},
	}}
}
func (s *State) directory(cx *core.Scope) core.Node {
	stamp := core.NewMemo(cx, func() uint64 { s.read(); return s.catalogueVersion })
	rows := core.NewMemoFunc(cx, func() []directoryRow { stamp.Get(); i18n.Language(cx); return core.Untracked(cx, s.directoryRows) }, nil)
	brake := &listTouchBrake{enabled: !isWeb() && cx.Env().OS() == core.OSAndroid}
	return core.View{TestID: "session-sidebar", Style: fill, StyleFunc: func() core.Style { c := palette(cx); return core.Style{Background: c.sidebar, Color: c.foreground} }, Children: []core.Node{
		s.sidebarDevices(cx),
		core.View{Style: core.Style{Padding: core.Symmetric(6, 14), Gap: 9}, Children: []core.Node{
			row(label(cx, locales.For(s.cx).Sessions(), 18, false), spacer(), s.sessionFiltersButton(cx), s.refreshSessionsButton(cx), labelledIconButton(cx, "new-session", locales.For(s.cx).NewSession(), lucide.SquarePen, func() { s.createDirectory = ""; s.dialog.Set("create") })),
		}},
		s.emptyCatalogue(cx),
		s.sessionFilterFeedback(cx),
		brake.wrap(core.List{TestID: "session-list", Style: fill, Count: func() int { return len(rows.Get()) }, Key: func(i int) any { return rows.Get()[i].ID }, EstimatedSize: 62, Row: func(cx *core.Scope, r core.ListRow) core.Node {
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
				key.Get()
				data := core.Untracked(cx, item)
				if data.Kind == "empty" {
					return core.View{TestID: data.ID, Style: core.Style{Padding: core.Symmetric(8, 14)}, Children: []core.Node{label(cx, data.Title, 12, true)}}
				}
				if data.Header {
					return s.treeHeader(cx, item, brake)
				}
				return s.sessionRow(cx, item, brake)
			})
		}}),
		core.View{Style: core.Style{Padding: core.Symmetric(8, 10)}, Children: []core.Node{row(
			button(cx, "server-settings", "设置", lucide.Settings, "ghost", func() { s.openSettings("general") }),
			labelledIconButton(cx, "account-menu", "账号与设备同步", lucide.User, func() { s.dialog.Set("account") }), spacer(),
			core.Show{When: func() bool { return isMobile(cx) }, Then: func(cx *core.Scope) core.Node {
				return labelledIconButton(cx, "scan-qr", "扫描二维码", lucide.ScanLine, func() { s.scan(cx) })
			}},
			labelledIconButton(cx, "theme-toggle", "切换主题", lucide.SunMoon, s.toggleTheme),
		)}},
	}}
}
func (s *State) treeHeader(cx *core.Scope, item func() directoryRow, brake *listTouchBrake) core.Node {
	// Construction reads must not subscribe the keyed row boundary to metadata.
	data := core.Untracked(cx, item)
	key := data.Group
	if data.Kind == "section" {
		key = data.ID
	}
	if data.Kind == "section" {
		return core.View{TestID: data.ID, Role: core.RoleButton, LabelFunc: func() string { return item().Title }, Expanded: func() bool { s.read(); return !s.collapsed[key] },
			OnPress: func(core.PressEvent) {
				brake.action(func() { s.collapsed[key] = !s.collapsed[key]; s.saveSidebarPreferences() })()
			},
			Style:     core.Style{Group: "directory-section", Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Padding: core.Symmetric(9, 14), Margin: core.Edges{Top: 8}, CornerRadius: 7, MinHeight: core.Pt(38)},
			StyleFunc: func() core.Style { return pressStyle(cx) }, Children: []core.Node{
				singleLine(cx, func() string { return item().Title }, 13, true),
				core.Icon{TestID: data.ID + "-disclosure", SVGFunc: func() *core.SVG {
					s.read()
					if s.collapsed[key] {
						return lucide.ChevronRight()
					}
					return lucide.ChevronDown()
				},
					Style: core.Style{Width: core.Pt(13), Height: core.Pt(13)}, StyleFunc: func() core.Style {
						s.read()
						if s.collapsed[key] {
							return core.Style{Opacity: core.Alpha(1)}
						}
						return contextualAffordance(cx, "directory-section", true)
					}},
			}}
	}
	menuOpen, tipOpen := core.NewSignal(cx, false), core.NewSignal(cx, false)
	tip := tooltipCard(cx, s.groupInfo(cx, item))
	tip.Open = func() bool { return tipOpen.Get() && !menuOpen.Get() }
	tip.OnOpenChange = func(open bool, _ base.Reason) { tipOpen.Set(open && !menuOpen.GetUntracked()) }
	trigger := core.View{TestID: data.ID, Role: core.RoleButton,
		Expanded:  func() bool { s.read(); return !s.collapsed[key] },
		LabelFunc: func() string { return item().Title + " · " + locales.For(cx).FilterMatches(fmt.Sprint(item().Count)) },
		OnPress: func(core.PressEvent) {
			brake.action(func() { s.collapsed[key] = !s.collapsed[key]; s.saveSidebarPreferences() })()
		}, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 7, Padding: core.Symmetric(7, 5), MinHeight: core.Pt(34), MinWidth: core.Pt(0), CornerRadius: 7},
		StyleFunc: func() core.Style { return pressStyle(cx) }, Tooltip: tip, Children: []core.Node{
			core.Icon{TestID: data.ID + "-icon", SVGFunc: func() *core.SVG {
				s.read()
				switch data.Kind {
				case "device":
					return lucide.Monitor()
				case "agent":
					return agentIcon(data.Provider)
				default:
					if s.collapsed[key] {
						return lucide.FolderClosed()
					}
					return lucide.FolderOpen()
				}
			}, Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), FlexShrink: 0}, StyleFunc: func() core.Style {
				s.read()
				return core.Style{Color: choose(s.collapsed[key], palette(cx).mutedText, palette(cx).foreground)}
			}},
			singleLine(cx, func() string { return item().Title }, 12, true),
		}}
	return core.View{Style: core.Style{Padding: core.Edges{Top: 2, Left: core.Points(8 + data.Depth*14), Right: 8}}, Children: []core.Node{
		ui.ContextMenu{TestID: "menu-" + data.ID, Open: menuOpen.Get, OnOpenChange: func(open bool, _ base.Reason) {
			menuOpen.Set(open)
			if open {
				tipOpen.Set(false)
			}
		}, Trigger: trigger, Children: []core.Node{s.groupInfo(cx, item)}},
	}}

}
func (s *State) sessionRow(cx *core.Scope, item func() directoryRow, brake *listTouchBrake) core.Node {
	// Construction reads must not subscribe the keyed row boundary to metadata.
	data := core.Untracked(cx, item)
	ss := data.Session
	id := sessionRowID(data.Section, ss)
	pin := core.NewMemo(cx, func() bool { s.read(); return s.pinned[sessionIdentity(item().Session)] != 0 })
	selected := func() bool {
		s.read()
		return s.target == str(item().Session, "deviceId") && str(s.selected, "providerId") == str(item().Session, "providerId") && str(s.selected, "id") == str(item().Session, "id")
	}
	menuOpen := core.NewSignal(cx, false)
	actionsHover := core.NewSignal(cx, false)
	tipOpen := core.NewSignal(cx, false)
	tip := s.sessionTooltip(cx, item)
	tip.Open = func() bool { return tipOpen.Get() && !menuOpen.Get() && !actionsHover.Get() }
	tip.OnOpenChange = func(open bool, _ base.Reason) { tipOpen.Set(open && !menuOpen.GetUntracked()) }
	return ui.ContextMenu{TestID: "menu-" + id, Open: menuOpen.Get, OnOpenChange: func(open bool, _ base.Reason) {
		menuOpen.Set(open)
		if open {
			tipOpen.Set(false)
		}
	}, Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
		pinned := pin.Get()
		return ui.MenuItem{TestID: "session-pin-action", Text: choose(pinned, locales.For(cx).UnpinSession(), locales.For(cx).PinSession()), Icon: choose(pinned, lucide.PinOff, lucide.Pin), OnSelect: func() { s.togglePinned(item().Session) }}
	}), ui.MenuItem{TestID: "session-remove-action", Text: locales.For(cx).RemoveSession(), Icon: lucide.Archive, Variant: ui.MenuItemDanger, OnSelect: func() { s.requestSessionRemoval(item().Session) }}}, Trigger: core.View{Style: core.Style{Group: "session-row", Direction: core.Row, AlignItems: core.AlignCenter, Margin: core.Symmetric(2, 8), CornerRadius: 9, MinWidth: core.Pt(0)}, StyleFunc: func() core.Style {
		st := core.Style{Padding: core.Edges{Left: core.Points(item().Depth * 14)}, Hover: &core.Style{Background: palette(cx).muted}, FocusWithin: &core.Style{Background: palette(cx).muted}}
		if selected() {
			st.Background = palette(cx).selected
		}
		return st
	}, Children: []core.Node{
		core.View{TestID: id, Role: core.RoleButton, LabelFunc: func() string {
			v := item()
			return v.Title + " · " + s.environmentName(str(v.Session, "deviceId")) + " · " + s.providerName(str(v.Session, "deviceId"), str(v.Session, "providerId")) + " · " + s.sessionStatusLabel(v.Session)
		}, OnPress: func(core.PressEvent) { brake.action(func() { s.selectSession(item().Session) })() }, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Padding: core.Edges{Top: 9, Bottom: 9, Left: 7, Right: 70}, Gap: 5, CornerRadius: 9}, StyleFunc: func() core.Style { return pressStyle(cx) }, Tooltip: tip, Children: []core.Node{
			singleLine(cx, func() string {
				v := item().Title
				if v == "" {
					return locales.For(s.cx).UnnamedSession()
				}
				return v
			}, 13, false),
			row(glyph(cx, sessionIcon(ss), 12), singleLine(cx, func() string {
				s.read()
				v := item().Session
				return s.environmentName(str(v, "deviceId")) + " · " + s.providerName(str(v, "deviceId"), str(v, "providerId"))
			}, 11, true)),
		}},
		s.sessionStatusIndicator(cx, id, item),
		core.Show{When: func() bool { return cx.Env().Desktop() }, Then: func(cx *core.Scope) core.Node {
			return core.View{TestID: "actions-" + id, Style: core.Style{Position: core.Absolute, Top: core.Pt(4), Right: core.Pt(4), Direction: core.Row, AlignItems: core.AlignCenter, CornerRadius: 7},
				OnMouseEnter: func(core.MouseEvent) { actionsHover.Set(true) }, OnMouseLeave: func(core.MouseEvent) { actionsHover.Set(false) },
				StyleFunc: func() core.Style {
					return contextualAffordance(cx, "session-row", false)
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						pinned := pin.Get()
						return contextualIconButton(cx, "pin-"+id, choose(pinned, locales.For(cx).UnpinSession(), locales.For(cx).PinSession()), choose(pinned, lucide.PinOff, lucide.Pin), func() { s.togglePinned(item().Session) })
					}),
					contextualIconButton(cx, "remove-"+id, locales.For(cx).RemoveSession(), lucide.Archive, func() { s.requestSessionRemoval(item().Session) }),
				}}
		}},
	}}}

}

func sessionRowID(section string, session Object) string {
	return "session-" + section + "-" + base64.RawURLEncoding.EncodeToString([]byte(sessionIdentity(session)))
}
