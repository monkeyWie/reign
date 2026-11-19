package hubui

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func (s *State) pruneQuestionDrafts() {
	active := map[string]bool{}
	for _, a := range s.approvals {
		active[s.approvalKey(a)] = true
	}
	for key := range s.questionDrafts {
		if strings.HasPrefix(key, "approval:"+s.key()+":") && !active[key] {
			delete(s.questionDrafts, key)
		}
	}
}

func (s *State) questionCard(cx *core.Scope, a Object) core.Node {
	questions := objects(a["questions"])
	id, key, session := str(a, "id"), s.approvalKey(a), s.key()
	if s.questionDrafts == nil {
		s.questionDrafts = map[string]map[string]string{}
	}
	if s.questionDrafts[key] == nil {
		s.questionDrafts[key] = map[string]string{}
	}
	drafts := s.questionDrafts[key]
	values := make([]*core.Signal[string], len(questions))
	for i, q := range questions {
		values[i] = core.NewSignal(cx, drafts[str(q, "id")])
	}
	index := core.NewSignal(cx, 0)
	busy := func() bool { return s.actionPending(key) }
	disabled := func() bool {
		if s.key() != session || !s.connected() || busy() {
			return true
		}
		for _, current := range s.approvals {
			if str(current, "id") == id {
				return false
			}
		}
		return true
	}
	submit := func() {
		if disabled() {
			return
		}
		answers := Object{}
		for i, q := range questions {
			value := strings.TrimSpace(values[i].Get())
			if value != "" {
				answers[str(q, "id")] = Object{"answers": []string{value}}
			} else if !flag(a, "allowSkip") {
				index.Set(i)
				return
			}
		}
		decision := "input"
		if len(answers) == 0 && flag(a, "allowSkip") {
			decision = "skip"
		}
		s.approvalAction(a, Object{"decision": decision, "answers": answers})
	}
	content := core.Dynamic(func(cx *core.Scope) core.Node {
		i := index.Get()
		q, value := questions[i], values[i]
		set := func(v string) {
			if !disabled() {
				drafts[str(q, "id")] = v
				value.Set(v)
			}
		}
		heading := str(q, "header")
		if heading == "" {
			heading = "需要你的回答"
		}
		children := []core.Node{core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, Gap: 12}, Children: []core.Node{
			label(cx, heading, 12, true), label(cx, fmt.Sprintf("%d / %d", i+1, len(questions)), 12, true),
		}}, label(cx, str(q, "question"), 14, false)}
		options := objects(q["options"])
		for j, option := range options {
			text := str(option, "label")
			selected := func() bool { return value.Get() == text }
			body := []core.Node{label(cx, text, 13, false)}
			if description := str(option, "description"); description != "" {
				body = append(body, label(cx, description, 12, true))
			}
			children = append(children, core.View{TestID: fmt.Sprintf("question-option-%s-%d-%d", id, i, j), Role: core.RoleRadio, Label: text, Checked: selected, Disabled: disabled,
				OnPress: func(core.PressEvent) { set(text) },
				Style:   core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: 10, Padding: core.All(10), MinHeight: core.Pt(44), CornerRadius: 8},
				StyleFunc: func() core.Style {
					st := pressStyle(cx)
					if selected() {
						st.Background = palette(cx).brand.Faded(.12)
						st.Color = palette(cx).brand
					}
					return st
				}, Children: []core.Node{core.Icon{SVGFunc: func() *core.SVG { return choose(selected(), lucide.CircleCheck, lucide.Circle)() },
					Style: core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0}, StyleFunc: func() core.Style {
						return core.Style{Color: choose(selected(), palette(cx).brand, palette(cx).mutedText)}
					}},
					core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: body}}})
		}
		children = append(children, core.Input{TestID: fmt.Sprintf("question-input-%s-%d", id, i), Label: "自定义回答", Placeholder: choose(len(options) > 0, "或输入其他回答…", "输入你的回答…"), Disabled: disabled, Masked: flag(q, "isSecret"), Multiline: !flag(q, "isSecret"),
			Value: func() string {
				v := value.Get()
				for _, option := range options {
					if v == str(option, "label") {
						return ""
					}
				}
				return v
			}, OnChange: set, Style: core.Style{MinHeight: core.Pt(44), MaxHeight: core.Pt(120)}, StyleFunc: func() core.Style { return textInputStyle(cx) }})
		buttons := []core.Node{}
		if i > 0 {
			back := button(cx, "question-back-"+id, "上一题", lucide.ArrowLeft, "ghost", func() { index.Set(i - 1) }).(core.View)
			back.Disabled = disabled
			buttons = append(buttons, back)
		}
		buttons = append(buttons, core.View{Style: core.Style{FlexGrow: 1}})
		if flag(a, "allowSkip") {
			skip := button(cx, "question-skip-"+id, "跳过", nil, "ghost", func() {
				set("")
				if i+1 < len(questions) {
					index.Set(i + 1)
				} else {
					submit()
				}
			}).(core.View)
			skip.Disabled = disabled
			buttons = append(buttons, skip)
		}
		next := busyButton(cx, "question-submit-"+id, choose(i+1 < len(questions), "下一题", "发送"), nil, "primary", busy, func() {
			if i+1 < len(questions) {
				index.Set(i + 1)
			} else {
				submit()
			}
		}).(core.View)
		next.Disabled = func() bool { return disabled() || strings.TrimSpace(value.Get()) == "" }
		buttons = append(buttons, next)
		children = append(children, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: buttons})
		return core.View{Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: children}
	})
	return s.messageInset(cx, core.View{TestID: "question-card-" + id, Label: "Agent 提问", Style: core.Style{Padding: core.All(14), CornerRadius: 12, Gap: 10, MinWidth: core.Pt(0)},
		StyleFunc: func() core.Style { return core.Style{Background: palette(cx).muted} }, Children: []core.Node{content}})
}
