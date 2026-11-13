package hubui

import (
	"github.com/monkeyWie/reign/pkg/messagecontent"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func questionReplyCard(cx *core.Scope, replies []messagecontent.Reply) core.Node {
	children := []core.Node{row(glyph(cx, lucide.Check, 14), label(cx, "已回答", 12, true))}
	for _, reply := range replies {
		answer := reply.Answer
		if answer == "" {
			answer = "未填写"
		}
		children = append(children, core.View{Style: core.Style{Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			label(cx, reply.Question, 14, false),
			core.View{Style: core.Style{Gap: 4, MinWidth: core.Pt(0)}, Children: []core.Node{
				label(cx, "你的回答", 12, true), label(cx, answer, 14, false),
			}},
		}})
	}
	return core.View{TestID: "question-replies", Label: "已回答的问题", Selectable: true,
		Style:     core.Style{Gap: 14, Padding: core.All(12), BorderWidth: 1, CornerRadius: 12, MinWidth: core.Pt(0)},
		StyleFunc: func() core.Style { return core.Style{BorderColor: palette(cx).border} }, Children: children}
}
