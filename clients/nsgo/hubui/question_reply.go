package hubui

import (
	"github.com/monkeyWie/reignhub/pkg/messagecontent"
	"github.com/nsgo-dev/nsgo/core"
)

func questionReplyCard(cx *core.Scope, replies []messagecontent.Reply) core.Node {
	children := []core.Node{}
	for _, reply := range replies {
		answer := reply.Answer
		if answer == "" {
			answer = "未填写"
		}
		children = append(children, core.View{Style: core.Style{Gap: 4, MinWidth: core.Pt(0)}, Children: []core.Node{
			label(cx, reply.Question, 14, true), label(cx, answer, 15, false),
		}})
	}
	// The enclosing user bubble owns padding and the surface, just as it does
	// for ordinary messages. Replies only add muted question context.
	return core.View{TestID: "question-replies", Label: "问题与回答", Selectable: true,
		Style: core.Style{Gap: 12, MinWidth: core.Pt(0)}, Children: children}
}
