package hubui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/pkg/messagecontent"
	"github.com/nsgo-dev/nsgo/base/textview"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"github.com/nsgo-dev/nsgo/ui/chat"
)

func (s *State) conversation(cx *core.Scope, narrow bool) core.Node {
	if s.selected == nil {
		return core.View{Style: fill.Merge(core.Style{AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}), Children: []core.Node{label(cx, "选择一个会话，或创建新会话", 14, true)}}
	}
	children := []core.Node{}
	floatingHeader := s.width.GetUntracked() < wideBreakpoint
	header := []core.Node{}
	if narrow {
		header = append(header, button(cx, "back-sessions", "", lucide.ArrowLeft, "ghost", s.backSessions))
	}
	header = append(header, core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, Gap: 4}, Children: []core.Node{singleLine(cx, func() string { s.read(); return str(s.selected, "title") }, 14, false), singleLine(cx, func() string { s.read(); return str(s.selected, "cwd") }, 11, true)}})
	header = append(header, s.connectionIndicator(cx, func() string { return s.target }), glyph(cx, sessionIcon(s.selected), 18), button(cx, "attachment-cache", "", lucide.HardDrive, "ghost", func() { s.dialog.Set("clear-cache") }))
	if !floatingHeader {
		children = append(children, core.View{TestID: "conversation-header", Style: core.Style{Padding: core.Symmetric(16, 20)}, StyleFunc: func() core.Style { return core.Style{BorderColor: palette(cx).border} }, Children: []core.Node{row(header...)}})
	}
	listRef := s.timeline
	atBottom := core.NewSignal(cx, true)
	viewport := core.NewSignal(cx, core.Size{})
	// Connection/approval polling must not rebuild the entire list's data and
	// navigation index. Message projections are replaced, never edited in place.
	source := core.NewMemoFunc(cx, func() []Message { s.read(); return s.messageRows }, func(a, b []Message) bool {
		return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
	})
	rows := core.NewMemoFunc(cx, func() []Message {
		messages := source.Get()
		out := make([]Message, len(messages))
		for i, m := range messages {
			out[len(out)-1-i] = m
		}
		return out
	}, nil)
	// Row zero stays at the bottom. This is a native recycled list, not a
	// scroll view with thousands of eagerly constructed Markdown documents.
	tail := func() bool {
		s.read()
		active := activeTurn(s.transcript)
		if active == nil || !s.connected() {
			return false
		}
		for _, i := range objects(active["items"]) {
			if visibleTool(i) || str(i, "role") == "assistant" && strings.TrimSpace(str(i, "text")) != "" {
				return false
			}
		}
		return true
	}
	tailCount := func() int {
		s.read()
		n := len(s.approvals)
		if tail() {
			n++
		}
		return n
	}
	list := core.List{TestID: "chat-message-list", Ref: listRef, Inverted: true, EstimatedSize: 120, Style: fill, Count: func() int { return len(rows.Get()) + tailCount() }, Key: func(i int) any {
		offset := tailCount()
		if i < offset {
			return fmt.Sprint("tail-", i)
		}
		return rows.Get()[i-offset].ID
	},
		OnLayout: func(b core.Bounds) { viewport.Set(b.Size) }, OnScroll: func(e core.ScrollEvent) { atBottom.Set(e.ContentSize.Height-e.Size.Height-e.Offset.Y < 48) },
		Row: func(cx *core.Scope, r core.ListRow) core.Node {
			key := core.NewMemo(cx, func() string {
				i := r.Index()
				offset := tailCount()
				if i < offset {
					return fmt.Sprint("tail-", i)
				}
				v := rows.Get()
				if i-offset < len(v) {
					return v[i-offset].ID
				}
				return ""
			})
			return core.Dynamic(func(cx *core.Scope) core.Node {
				id := key.Get()
				if strings.HasPrefix(id, "tail-") {
					i := core.Untracked(cx, r.Index)
					if i < len(s.approvals) {
						return s.approvalCard(cx, s.approvals[i])
					}
					return s.messageInset(cx, core.View{Style: core.Style{Padding: core.Edges{Top: 12, Bottom: 20}}, Children: []core.Node{chat.Shimmer{Text: func() string { return "Thinking…" }, Size: 13}}})
				}
				get := func() Message {
					index := r.Index() - tailCount()
					v := rows.Get()
					if index < 0 || index >= len(v) {
						return Message{}
					}
					return v[index]
				}
				return s.messageInset(cx, s.message(cx, get))
			})
		}, Footer: core.View{TestID: "history-start-inset", Style: core.Style{Padding: core.Edges{Top: choose(floatingHeader, core.Points(88), core.Points(0))}}, Children: []core.Node{core.Show{When: func() bool { s.read(); return str(s.transcript, "nextCursor") != "" }, Then: func(cx *core.Scope) core.Node {
			return button(cx, "load-older", "加载更早消息", nil, "ghost", s.older)
		}}}},
	}
	timelineChildren := []core.Node{list, s.emptyTranscript(cx), s.syncIndicator(cx), core.Show{When: func() bool { return !s.narrowLayout() }, Then: func(cx *core.Scope) core.Node { return s.messageNavigation(cx, rows, listRef, viewport, tailCount) }}, core.Show{When: func() bool { return !atBottom.Get() }, Then: func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Position: core.Absolute, Bottom: core.Pt(12), Right: core.Pt(40)}, Children: []core.Node{button(cx, "jump-to-latest", "回到最新", lucide.ArrowDown, "outline", func() { listRef.ScrollToIndexAt(0, 0, 0, false) })}}
	}}}
	if floatingHeader {
		timelineChildren = append(timelineChildren, s.compactConversationHeader(cx, narrow))
	}
	children = append(children, core.View{TestID: "timeline", Style: fill, Children: timelineChildren})
	children = append(children, s.composer(cx))
	return core.View{TestID: "conversation", Style: fill, StyleFunc: func() core.Style { return s.workspaceSurface(cx) }, Children: children}
}

// The narrow-layout toolbar floats over the recycled list. Only the beginning of the
// history has an inset, so scrolled messages pass beneath the fading backdrop.
func (s *State) compactConversationHeader(cx *core.Scope, back bool) core.Node {
	pill := func(children ...core.Node) core.Node {
		return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Height: core.Pt(52), CornerRadius: 26, FlexShrink: 0, Padding: core.Symmetric(0, 4)}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: children}
	}
	buttons := []core.Node{}
	if back {
		buttons = append(buttons, pill(button(cx, "back-sessions", "", lucide.ArrowLeft, "ghost", s.backSessions)))
	}
	buttons = append(buttons, core.View{TestID: "conversation-title-pill", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Height: core.Pt(52), CornerRadius: 26, JustifyContent: core.JustifyCenter, Padding: core.Symmetric(6, 16), Gap: 2}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
		singleLine(cx, func() string { s.read(); return str(s.selected, "title") }, 14, false),
		singleLine(cx, func() string {
			s.read()
			cwd := strings.TrimRight(strings.ReplaceAll(str(s.selected, "cwd"), "\\", "/"), "/")
			if i := strings.LastIndex(cwd, "/"); i >= 0 {
				cwd = cwd[i+1:]
			}
			return cwd
		}, 11, true),
	}}, pill(s.connectionIndicator(cx, func() string { return s.target }), button(cx, "attachment-cache", "", lucide.HardDrive, "ghost", func() { s.dialog.Set("clear-cache") })))
	return core.View{TestID: "conversation-header", PointerEvents: core.PointerEventsBoxNone, Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0), Height: core.Pt(100), ZIndex: 5}, Children: []core.Node{
		core.View{TestID: "conversation-header-fade", PointerEvents: core.PointerEventsNone, Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0)}, StyleFunc: func() core.Style {
			bg := palette(cx).background
			mask := core.LinearGradient(180, core.RGB(0xffffff), core.RGB(0xffffff), core.RGB(0xffffff).Faded(0))
			return core.Style{Gradients: []core.Gradient{core.LinearGradient(180, bg.Faded(.9), bg.Faded(.6), bg.Faded(0))}, BackdropFilters: []core.Filter{core.Blur(10)}, Mask: &mask}
		}},
		core.View{PointerEvents: core.PointerEventsBoxNone, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Edges{Top: 8, Left: 12, Right: 12}, MinWidth: core.Pt(0)}, Children: buttons},
	}}
}
func (s *State) messageInset(cx *core.Scope, content core.Node) core.Node {
	return core.View{Style: core.Style{Width: core.Pct(100), MinWidth: core.Pt(0), AlignItems: core.AlignCenter}, Children: []core.Node{core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(988)}, StyleFunc: func() core.Style {
		inset := core.Points(40)
		width := s.width.Get()
		if width >= wideBreakpoint {
			width -= s.deviceWidth.Get() + s.sessionWidth.Get() + 10
		} else if width >= compactBreakpoint {
			width -= s.deviceWidth.Get() + 5
		} else {
			inset = 20
		}
		if width >= wideBreakpoint {
			inset = 64
		}
		return core.Style{Padding: core.Symmetric(0, inset), MaxWidth: core.Pt(860 + inset*2)}
	}, Children: []core.Node{content}}}}
}
func (s *State) message(cx *core.Scope, get func() Message) core.Node {
	m := core.Untracked(cx, get)
	if m.Role == "activity" {
		return s.activity(cx, get)
	}
	hover := core.NewSignal(cx, false)
	user := m.Role == "user"
	// User bubbles use their intrinsic content width; the parent provides the
	// available width for wrapping. Assistant articles fill the message column.
	bodyWidth := core.Pct(100)
	if user {
		bodyWidth = core.Length{}
	}
	content := []core.Node{}
	content = append(content, core.Each(func() []Object {
		out := []Object{}
		for _, a := range get().Attachments {
			if str(a, "kind") == "image" && !flag(a, "inline") {
				out = append(out, a)
			}
		}
		return out
	}, func(a Object) string { return str(a, "id") }, func(cx *core.Scope, a func() Object, _ func() int) core.Node { return s.imageTile(cx, get, a) }))
	bodyText := smoothReply(cx, func() string {
		if user {
			return strings.TrimRight(displayText(get()), " \t\r\n")
		}
		return displayText(get())
	}, func() bool {
		s.read()
		return !user && s.animateText[get().ID] && !s.transcriptSyncing && s.connected()
	})
	body := ui.TextView{MarkdownFunc: bodyText, Options: textview.Options{Streaming: m.Live}, RenderImage: func(cx *core.Scope, im textview.Image) core.Node {
		for _, a := range get().Attachments {
			if str(a, "source") == im.Src {
				return s.imageTile(cx, get, func() Object { return a })
			}
		}
		return label(cx, choose(im.Alt != "", im.Alt, "图片不可用"), 13, true)
	}, Selectable: true, OpenLinks: false, OnLinkPress: func(e textview.LinkPressEvent) { s.openLink(cx, get(), e.URL) }, CodeBlockActions: []textview.CodeAction{{ID: textview.CopyAction, Label: "复制"}}, Styles: textview.Styles{textview.PartRoot: core.Style{FontSize: 15, LineHeight: 25, MinWidth: core.Pt(0), Width: bodyWidth}}, StyleOf: func(p textview.Part) core.Style {
		c := palette(cx)
		switch p {
		case textview.PartRoot, textview.PartParagraph, textview.PartHeading:
			return core.Style{Color: c.foreground}
		case textview.PartCodeBlock:
			return core.Style{Background: c.code, BorderWidth: 1, BorderColor: c.border, CornerRadius: 12, Overflow: core.Hidden}
		case textview.PartCodeBlockScroll:
			return core.Style{Background: c.code, Padding: core.All(18)}
		case textview.PartCode:
			return core.Style{Color: c.accent, Background: c.code, CornerRadius: 4}
		}
		return core.Style{}
	}}
	var bodyContent core.Node = body
	if user {
		bodyContent = core.Dynamic(func(cx *core.Scope) core.Node {
			children := []core.Node{}
			for _, part := range messagecontent.Parse(displayText(get())) {
				if len(part.Replies) > 0 {
					children = append(children, questionReplyCard(cx, part.Replies))
				} else if strings.TrimSpace(part.Text) != "" {
					text := part.Text
					view := body
					view.MarkdownFunc = func() string { return strings.TrimRight(text, " \t\r\n") }
					children = append(children, view)
				}
			}
			return core.View{Style: core.Style{Gap: 12, MinWidth: core.Pt(0)}, Children: children}
		})
	}
	bubble := core.View{TestID: "message-body-" + m.ID, Selectable: true, Style: core.Style{Width: bodyWidth, FlexShrink: 1, MinWidth: core.Pt(0), MaxWidth: core.Pt(choose(user, core.Points(680), core.Points(820)))}, StyleFunc: func() core.Style {
		if user {
			return core.Style{Background: palette(cx).muted, CornerRadius: 16, Padding: core.Symmetric(10, 16)}
		}
		return core.Style{}
	}, Children: []core.Node{core.Show{When: func() bool { return strings.TrimSpace(displayText(get())) != "" }, Then: func(*core.Scope) core.Node { return bodyContent }}, core.Each(func() []Object {
		out := []Object{}
		for _, a := range get().Attachments {
			if str(a, "kind") != "image" && !flag(a, "inline") {
				out = append(out, a)
			}
		}
		return out
	}, func(a Object) string { return str(a, "id") }, func(cx *core.Scope, a func() Object, _ func() int) core.Node { return s.fileTile(cx, a()) })}}
	var bubbleContent core.Node = bubble
	if user {
		bubbleContent = core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), MinWidth: core.Pt(0), JustifyContent: core.JustifyEnd}, Children: []core.Node{bubble}}
	}
	// Images are a compact horizontal gallery above the bubble, not path text.
	gallery := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8, JustifyContent: choose(user, core.JustifyEnd, core.JustifyStart)}, Children: content}
	content = []core.Node{core.Show{When: func() bool {
		for _, a := range get().Attachments {
			if str(a, "kind") == "image" && !flag(a, "inline") {
				return true
			}
		}
		return false
	}, Then: func(*core.Scope) core.Node {
		return core.View{Style: core.Style{Padding: core.Edges{Bottom: 12}}, Children: []core.Node{gallery}}
	}}, core.Show{When: func() bool {
		if strings.TrimSpace(displayText(get())) != "" {
			return true
		}
		for _, a := range get().Attachments {
			if str(a, "kind") != "image" && !flag(a, "inline") {
				return true
			}
		}
		return false
	}, Then: func(*core.Scope) core.Node { return bubbleContent }}}
	content = append(content, core.Show{When: func() bool { return get().Delivery != "" }, Then: func(cx *core.Scope) core.Node {
		return core.View{TestID: "delivery-" + m.ID, Tooltip: dynamicText(cx, func() string { return get().DeliveryError }, 12, false), Style: core.Style{Padding: core.Edges{Top: 6}}, Children: []core.Node{core.Show{When: func() bool { return get().Delivery == "sending" }, Then: func(*core.Scope) core.Node {
			return chat.Shimmer{Text: func() string { return "发送中…" }, Size: 11}
		}, Else: func(cx *core.Scope) core.Node {
			return dynamicText(cx, func() string { return choose(get().Delivery == "failed", "发送失败", "已发送") }, 11, true)
		}}}}
	}})
	content = append(content, core.Show{When: func() bool { return !get().Live && get().Delivery != "sending" }, Then: func(cx *core.Scope) core.Node {
		return core.View{TestID: "message-actions-" + m.ID, Style: core.Style{Height: core.Pt(32), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, StyleFunc: func() core.Style {
			visible := hover.Get() || s.narrowLayout()
			return core.Style{Opacity: core.Alpha(choose(visible, float32(1), float32(0))), PointerEvents: choose(visible, core.PointerEventsAuto, core.PointerEventsNone)}
		}, Children: []core.Node{s.copyButton(cx, "copy-message-"+m.ID, "", "ghost", func() string { return messageCopyText(get()) }), dynamicText(cx, func() string {
			ts := get().Timestamp
			if ts == 0 {
				return "时间未知"
			}
			return s.localTime(time.UnixMilli(ts)).Format("15:04")
		}, 11, true)}}
	}})
	return core.View{TestID: "message-" + m.ID, OnMouseEnter: func(core.MouseEvent) { hover.Set(true) }, OnMouseLeave: func(core.MouseEvent) { hover.Set(false) }, Style: core.Style{AlignItems: choose(user, core.AlignEnd, core.AlignStretch), Padding: core.Edges{Top: 10, Bottom: 10}, Width: core.Pct(100), MinWidth: core.Pt(0)}, Children: content}
}
func (s *State) activity(cx *core.Scope, get func() Message) core.Node {
	open := core.NewSignal(cx, false)
	tools := func() []Object { return get().Tools }
	title := func() string { return activityLabel(tools()) }
	return core.View{TestID: "tool-group", Style: core.Style{Padding: core.Edges{Top: 10, Bottom: 10}, Gap: 8}, Children: []core.Node{core.View{Role: core.RoleButton, LabelFunc: title, OnPress: func(core.PressEvent) { open.Set(!open.Get()) }, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 9, Padding: core.Symmetric(8, 0)}, Children: []core.Node{glyph(cx, lucide.Wrench, 16), core.Show{When: func() bool { return get().Live && s.connected() }, Then: func(*core.Scope) core.Node { return chat.Shimmer{Text: title, Size: 12} }, Else: func(cx *core.Scope) core.Node { return dynamicText(cx, title, 12, true) }}, core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node { return glyph(cx, lucide.ChevronUp, 12) }}}}, core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
		return core.List{Count: func() int { return len(tools()) }, EstimatedSize: 36, StyleFunc: func() core.Style {
			return core.Style{Height: core.Pt(core.Points(min(8, len(tools()))) * 44), MaxHeight: core.Pt(352)}
		}, Row: func(cx *core.Scope, r core.ListRow) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				v := tools()
				if r.Index() >= len(v) {
					return nil
				}
				t := v[r.Index()]
				acts := objects(t["activities"])
				parts := []core.Node{}
				for _, a := range acts {
					parts = append(parts, row(glyph(cx, lucide.Wrench, 13), label(cx, str(a, "title"), 12, true)))
					if d := str(a, "detail"); d != "" {
						parts = append(parts, label(cx, d, 11, true))
					}
				}
				if len(parts) == 0 {
					parts = append(parts, label(cx, choose(str(t, "status") == "completed", "操作已完成", "正在执行…"), 12, true))
				}
				return core.View{Style: core.Style{Padding: core.Symmetric(6, 0), Gap: 4}, Children: parts}
			})
		}}
	}}}}
}
func (s *State) composer(cx *core.Scope) core.Node {
	inputHeight := core.NewSignal(cx, core.Points(56))
	modelCatalog := core.NewMemo(cx, func() string {
		s.read()
		b, _ := json.Marshal(s.models)
		return string(b) + "|" + s.model + "|" + fmt.Sprint(activeTurn(s.transcript) != nil)
	})
	running := func() bool { s.read(); return s.connected() && activeTurn(s.transcript) != nil }
	disabled := func() bool { s.read(); return s.busy || !s.connected() }
	submit := core.SubmitBehaviorSubmit
	if isMobile(cx) {
		submit = core.SubmitBehaviorNewline
	}
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Padding: core.Edges{Top: 6, Left: 18, Right: 18, Bottom: 18}, PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard}}, Children: []core.Node{core.Show{When: s.errorAtComposer, Then: func(cx *core.Scope) core.Node { return s.errorOverlay(cx, true) }}, core.View{TestID: "composer-card", OnFileDrop: func(uris []string) {
		if !isMobile(cx) {
			s.drop(cx, uris)
		}
	}, Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(860), Padding: core.All(12), CornerRadius: 18, BorderWidth: 1, Gap: 10}, StyleFunc: func() core.Style {
		c := palette(cx)
		return core.Style{Background: c.elevated, BorderColor: c.border, Color: c.foreground, Shadows: []core.BoxShadow{{Offset: core.Point{Y: 5}, Blur: 20, Color: core.RGB(0x000000).Faded(choose(cx.Dark(), float32(.18), float32(.07)))}}}
	}, Children: []core.Node{
		core.Show{When: func() bool { s.read(); return len(s.uploads) > 0 }, Then: func(cx *core.Scope) core.Node { return s.uploadPreviews(cx) }},
		core.Input{TestID: "chat-input", Label: "消息", Placeholder: "发送消息…", Value: s.draft.Get, OnChange: s.draft.Set, Multiline: true, SubmitBehavior: submit, OnSubmit: func(string) { s.send(choose(running(), "steer", "send")) }, OnPaste: func(e core.PasteEvent) {
			if !isMobile(cx) {
				s.paste(cx, e)
			}
		}, OnContentSizeChange: func(size core.Size) { inputHeight.Set(min(168, max(56, size.Height+12))) }, Style: core.Style{FontSize: 14, LineHeight: 23, Padding: core.Symmetric(10, 4)}, StyleFunc: func() core.Style {
			c := palette(cx)
			return core.Style{Height: core.Pt(inputHeight.Get()), Color: c.foreground, PlaceholderColor: c.mutedText}
		}},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10}, Children: []core.Node{core.Show{When: func() bool { return s.agentCapability("imageInput") || s.agentCapability("fileInput") }, Then: func(cx *core.Scope) core.Node {
			return button(cx, "attach-files", "", lucide.Plus, "ghost", func() {
				if !disabled() {
					s.chooseFiles(cx)
				}
			})
		}}, spacer(), core.Dynamic(func(cx *core.Scope) core.Node {
			_ = modelCatalog.Get()
			options := []chat.Option{}
			found := false
			for _, m := range s.models {
				options = append(options, chat.Option{ID: str(m, "id"), Label: str(m, "name")})
				found = found || str(m, "id") == s.model
			}
			if s.model != "" && !found {
				options = append([]chat.Option{{ID: s.model, Label: s.model}}, options...)
			}
			if core.Untracked(cx, running) {
				name := s.model
				for _, m := range s.models {
					if str(m, "id") == s.model {
						name = str(m, "name")
						break
					}
				}
				if name == "" {
					name = "Agent 默认模型"
				}
				return label(cx, name, 12, true)
			}
			return core.View{Style: core.Style{MaxWidth: core.Pt(220), FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{chat.Picker{TestID: "model-selector", Label: func() string {
				s.read()
				for _, m := range s.models {
					if str(m, "id") == s.model {
						return str(m, "name")
					}
				}
				if s.model != "" {
					return s.model
				}
				return "Agent 默认模型"
			}, Options: options, Selected: func() string { s.read(); return s.model }, OnChange: func(id string) { s.model = id; s.changed() }, Width: 300, SearchLabel: "搜索模型"}}}
		}), core.Show{When: func() bool { return running() && s.agentCapability("interrupt") }, Then: func(cx *core.Scope) core.Node {
			return core.View{TestID: "interrupt-generation", Role: core.RoleButton, Label: "停止生成", Tooltip: label(cx, "停止生成", 12, false), OnPress: func(core.PressEvent) { s.send("interrupt") }, Style: core.Style{Width: core.Pt(36), Height: core.Pt(36), CornerRadius: 18, FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}, StyleFunc: func() core.Style {
				return core.Style{Background: palette(cx).foreground}
			}, Children: []core.Node{core.View{Style: core.Style{Width: core.Pt(11), Height: core.Pt(11), CornerRadius: 2}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }}}}
		}}, core.View{TestID: "send-message", Role: core.RoleButton, LabelFunc: func() string {
			if s.pendingSend() != "" {
				return "正在发送"
			}
			return "发送消息"
		}, Disabled: func() bool { return !s.canSend() }, OnPress: func(core.PressEvent) { s.send(choose(running(), "steer", "send")) }, Style: core.Style{Width: core.Pt(36), Height: core.Pt(36), CornerRadius: 18, FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}, StyleFunc: func() core.Style {
			c := palette(cx)
			opacity := float32(1)
			if !s.canSend() && s.pendingSend() == "" {
				opacity = .35
			}
			return core.Style{Background: c.foreground, Color: c.background, Opacity: core.Alpha(opacity)}
		}, Children: []core.Node{core.Show{When: func() bool { return s.pendingSend() != "" }, Then: func(cx *core.Scope) core.Node {
			theme := *cx.Theme()
			theme.Light.TextMuted = theme.Light.Background
			theme.Dark.TextMuted = theme.Dark.Background
			cx.SetTheme(&theme)
			return ui.Spinner{TestID: "send-loading", Label: "正在发送", Variant: ui.SpinnerSpokes, Size: 18}
		}, Else: func(cx *core.Scope) core.Node {
			return core.Icon{SVG: lucide.ArrowUp(), Style: core.Style{Width: core.Pt(16), Height: core.Pt(16)}}
		}}}}}},
	}}}}
}
func (s *State) messageNavigation(cx *core.Scope, rows *core.Memo[[]Message], ref *core.Ref, viewport *core.Signal[core.Size], tailCount func() int) core.Node {
	indices := core.NewMemoFunc(cx, func() []int {
		out := []int{}
		for i, m := range rows.Get() {
			if m.Role == "user" {
				out = append(out, i)
			}
		}
		return out
	}, nil)
	return core.View{TestID: "message-navigation", Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(4), Width: core.Pt(30), JustifyContent: core.JustifyCenter}, Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
		v := indices.Get()
		h := viewport.Get().Height
		n := min(len(v), 24, max(1, int((h-48)/12)))
		ticks := []core.Node{}
		for i := 0; i < n; i++ {
			idx := 0
			if n > 1 {
				idx = i * (len(v) - 1) / (n - 1)
			}
			rowIndex := v[idx]
			message := rows.Get()[rowIndex]
			tick := core.View{TestID: fmt.Sprintf("message-tick-%d", i), Role: core.RoleButton, Label: messagePreviewText(message), Tooltip: core.View{Style: core.Style{Width: core.Pt(300), Padding: core.All(16), CornerRadius: 14, BorderWidth: 1}, StyleFunc: func() core.Style {
				c := palette(cx)
				return core.Style{Background: c.background, BorderColor: c.border}
			}, Children: []core.Node{core.Text{Content: messagePreviewText(message), MaxLines: 5, Style: core.Style{FontSize: 12}}}}, OnPress: func(core.PressEvent) { ref.ScrollToIndexAt(rowIndex+tailCount(), 0.5, 0, true) }, Style: core.Style{Height: core.Pt(12), Width: core.Pt(30), JustifyContent: core.JustifyCenter, Padding: core.Edges{Left: 8, Right: 11}}, Children: []core.Node{core.View{Style: core.Style{Height: core.Pt(2), CornerRadius: 1}, StyleFunc: func() core.Style {
				return core.Style{Background: palette(cx).mutedText, Opacity: core.Alpha(.4), Hover: &core.Style{Opacity: core.Alpha(1)}}
			}}}}
			ticks = append(ticks, tick)
		}
		return core.View{Style: core.Style{Gap: 0, MaxHeight: core.Pt(288)}, Children: ticks}
	})}}
}
func (s *State) approvalCard(cx *core.Scope, a Object) core.Node {
	params := object(a["params"])
	options := objects(params["options"])
	buttons := []core.Node{}
	for _, option := range options {
		o := option
		style := "outline"
		if str(o, "kind") == "allow_once" {
			style = "primary"
		}
		buttons = append(buttons, button(cx, "approval-option-"+str(a, "id")+"-"+str(o, "id"), str(o, "name"), nil, style, func() { s.decision(a, str(o, "id")) }))
	}
	if flag(params, "input") {
		value := core.NewSignal(cx, str(params, "value"))
		buttons = append(buttons, core.Input{TestID: "approval-input-" + str(a, "id"), Label: "Agent 请求的内容", Value: value.Get, OnChange: value.Set, Multiline: true, Style: core.Style{MinHeight: core.Pt(72), MaxHeight: core.Pt(180)}}, button(cx, "approval-submit-"+str(a, "id"), "提交", nil, "primary", func() { s.answerApproval(a, value.Get()) }))
	}
	if len(buttons) == 0 {
		buttons = []core.Node{button(cx, "approve-"+str(a, "id"), "允许", nil, "primary", func() { s.decision(a, "accept") }), button(cx, "deny-"+str(a, "id"), "拒绝", nil, "outline", func() { s.decision(a, "decline") })}
	}
	title := str(params, "title")
	if title == "" {
		title = str(a, "method")
	}
	return s.messageInset(cx, card(cx, label(cx, "Agent 请求授权", 14, false), label(cx, title, 12, true), core.View{Style: core.Style{Gap: 8}, Children: buttons}))
}
func (s *State) openLink(cx *core.Scope, m Message, url string) {
	for _, a := range m.Attachments {
		if str(a, "source") == url {
			s.download(cx, a)
			return
		}
	}
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		cx.OpenURL(url, nil)
	}
}

func (s *State) workspaceSurface(cx *core.Scope) core.Style {
	c := palette(cx)
	st := core.Style{Background: c.background}
	if s.width.Get() >= wideBreakpoint {
		st = st.Merge(core.Style{Margin: core.Edges{Top: 8, Right: 8, Bottom: 8}, CornerRadius: 20, BorderWidth: 1, BorderColor: c.border, Overflow: core.Hidden, Shadows: []core.BoxShadow{{Offset: core.Point{Y: 2}, Blur: 12, Color: core.RGB(0x000000).Faded(.05)}}})
	}
	return st
}
