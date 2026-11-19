package hubui

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func mergeMessageQueue(old, next Object) Object {
	if str(next, "state") == "ready" {
		return next // An explicit empty list clears the queue.
	}
	out := copyObject(next)
	messages := append([]Object{}, objects(next["messages"])...)
	seen := map[string]bool{}
	for _, m := range messages {
		seen[str(m, "id")] = true
	}
	for _, m := range objects(old["messages"]) {
		if !seen[str(m, "id")] {
			messages = append(messages, m)
		}
	}
	// A stale union cannot grow forever while one source is unavailable.
	if len(messages) > 200 {
		out["messages"] = old["messages"]
	} else {
		out["messages"] = messages
	}
	return out
}

func queuedMessagePreview(message Object) string {
	if text := strings.TrimSpace(str(message, "text")); text != "" {
		return strings.Join(strings.Fields(text), " ")
	}
	if attachments := objects(message["attachments"]); len(attachments) > 0 {
		if len(attachments) == 1 && str(attachments[0], "name") != "" {
			return str(attachments[0], "name")
		}
		return fmt.Sprintf("%d 个附件", len(attachments))
	}
	return "待发送消息"
}

func queuedMessageStatus(message Object) string {
	switch str(message, "status") {
	case "preparing":
		return "准备发送"
	case "paused":
		return "已暂停"
	case "uncertain":
		return "等待确认"
	default:
		return "等待发送"
	}
}

func (s *State) messageQueueView(cx *core.Scope) core.Node {
	// Transcript text changes preserve this immutable object. Avoid reflowing
	// every queued preview for each assistant delta or unrelated status update.
	queue := core.NewMemoFunc(cx, func() Object { s.read(); return object(s.transcript["queue"]) }, func(a, b Object) bool { return reflect.DeepEqual(a, b) }).Get
	return core.Show{When: func() bool { return len(objects(queue()["messages"])) > 0 }, Then: func(cx *core.Scope) core.Node {
		return core.View{TestID: "message-queue", Label: "待发送消息", Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(860), MinWidth: core.Pt(0), Padding: core.Edges{Left: 8, Right: 8, Bottom: 8}, Gap: 2}, Children: []core.Node{
			dynamicText(cx, func() string {
				s.read()
				if !s.connected() || str(queue(), "state") != "ready" {
					return "待发送 · 等待同步"
				}
				return "待发送"
			}, 11, true),
			core.Scroll{TestID: "message-queue-scroll", StyleFunc: func() core.Style {
				return core.Style{MaxHeight: core.Pt(min(144, max(72, cx.Env().Window().Height/4))), MinWidth: core.Pt(0)}
			}, Children: []core.Node{core.Each(func() []Object { return objects(queue()["messages"]) }, func(m Object) string { return str(m, "id") }, func(cx *core.Scope, message func() Object, _ func() int) core.Node {
				return s.queuedMessageRow(cx, message)
			})}},
		}}
	}}
}

func (s *State) queuedMessageRow(cx *core.Scope, message func() Object) core.Node {
	id := core.Untracked(cx, func() string { return str(message(), "id") })
	open := core.NewSignal(cx, false)
	text := dynamicText(cx, func() string { return str(message(), "text") }, 13, false)
	text.Selectable = core.SelectableOn
	more := core.View{Style: core.Style{FlexShrink: 0}, StyleFunc: func() core.Style { return contextualAffordance(cx, "queued-message", true) }, Children: []core.Node{glyph(cx, lucide.Ellipsis, 16)}}
	return base.Popover{TestID: "queued-message-" + id, Label: "查看待发送消息", Open: open.Get, OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, Placement: core.PlaceTop, Align: core.AlignStart, Offset: 6, Margin: 8,
		Style: core.Style{Group: "queued-message", MinWidth: core.Pt(0), MinHeight: core.Pt(36), Padding: core.Symmetric(6, 6), CornerRadius: 6}, StyleFunc: func() core.Style { return pressStyle(cx) },
		Trigger: row(glyph(cx, lucide.ListEnd, 16), singleLine(cx, func() string { return queuedMessagePreview(message()) }, 13, true), spacer(),
			core.Show{When: func() bool { return len(objects(message()["attachments"])) > 0 && str(message(), "text") != "" }, Then: func(cx *core.Scope) core.Node {
				return row(glyph(cx, lucide.Paperclip, 13), dynamicText(cx, func() string { return fmt.Sprint(len(objects(message()["attachments"]))) }, 11, true))
			}},
			core.Show{When: func() bool { return str(message(), "status") != "queued" }, Then: func(cx *core.Scope) core.Node {
				return dynamicText(cx, func() string { return queuedMessageStatus(message()) }, 11, true)
			}}, more),
		PopupStyle: core.Style{Padding: core.All(14), Gap: 10, CornerRadius: 12, BorderWidth: 1}, PopupStyleFunc: func() core.Style {
			p := palette(cx)
			return core.Style{Width: core.Pt(min(460, max(160, cx.Env().Window().Width-24))), Background: p.elevated, Color: p.foreground, BorderColor: p.border, Shadows: []core.BoxShadow{{Offset: core.Point{Y: 6}, Blur: 20, Color: core.RGB(0).Faded(.16)}}}
		}, Children: []core.Node{
			row(dynamicText(cx, func() string { return queuedMessageStatus(message()) }, 12, true), spacer(), contextualIconButton(cx, "queued-message-close-"+id, "关闭预览", lucide.X, func() { open.Set(false) })),
			core.Scroll{TestID: "queued-message-preview-" + id, StyleFunc: func() core.Style {
				return core.Style{MaxHeight: core.Pt(min(300, max(100, cx.Env().Window().Height-200))), MinWidth: core.Pt(0)}
			}, Children: []core.Node{
				core.View{Style: core.Style{Gap: 8}, Children: []core.Node{text, core.Each(func() []int {
					rows := make([]int, len(objects(message()["attachments"])))
					for i := range rows {
						rows[i] = i
					}
					return rows
				}, func(i int) int { return i }, func(cx *core.Scope, index func() int, _ func() int) core.Node {
					attachment := func() Object {
						rows := objects(message()["attachments"])
						if index() >= len(rows) {
							return nil
						}
						return rows[index()]
					}
					return row(glyph(cx, choose(str(attachment(), "kind") == "image", lucide.Image, lucide.File), 16), singleLine(cx, func() string {
						if name := str(attachment(), "name"); name != "" {
							return name
						}
						return choose(str(attachment(), "kind") == "image", "图片附件", "附件")
					}, 12, true))
				})}},
			}},
		},
	}
}
