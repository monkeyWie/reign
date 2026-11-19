package hubui

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestQuestionReplyCard(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, newDemoAPI())
		m := Message{ID: "reply", Role: "user", Text: "<send_user_message_question_reply>\n[{\"questionItemId\":\"internal-call\",\"question\":\"是否允许我点击 Chrome 中的 Authorize，授予 Wrangler 部署 Workers、D1 和绑定 reignhub.app 所需的权限？\",\"answer\":\"允许，点击 Authorize 并继续部署\"}]\n</send_user_message_question_reply>"}
		return core.View{Style: fill.Merge(core.Style{Padding: core.All(20)}), Children: []core.Node{s.message(cx, func() Message { return m })}}
	}, nsgotest.Size(390, 600))
	app.Find("id:question-replies")
	if app.Exists("text:已回答") || app.Exists("text:你的回答") {
		t.Fatal("reply duplicates its meaning with status labels")
	}
	app.Find("text:允许，点击 Authorize 并继续部署")
	if app.Exists("text:internal-call") {
		t.Fatal("internal ID is visible")
	}
	if b := app.Find("id:question-replies").Frame; b.Size.Width > 350 || b.Size.Height <= 0 {
		t.Fatalf("bad reply card layout: %#v", b)
	}
	app.Screenshot("question-reply-card")
}
