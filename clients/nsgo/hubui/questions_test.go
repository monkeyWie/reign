package hubui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type questionSubmission struct {
	target  string
	request Object
	result  chan error
}
type questionAPI struct {
	*demoAPI
	lock      sync.Mutex
	approvals []Object
	writes    chan questionSubmission
}

func (f *questionAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && str(r, "method") == "approvals" {
		f.lock.Lock()
		defer f.lock.Unlock()
		return append([]Object{}, f.approvals...), nil
	}
	if method == "client.call" && str(r, "method") == "decide" {
		submission := questionSubmission{target, r, make(chan error, 1)}
		select {
		case f.writes <- submission:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case err := <-submission.result:
			if err == nil {
				f.lock.Lock()
				kept := []Object{}
				for _, a := range f.approvals {
					if str(a, "id") != str(r, "approval") {
						kept = append(kept, a)
					}
				}
				f.approvals = kept
				f.lock.Unlock()
			}
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func questionUIFixture(t *testing.T, width core.Points, dark, optional bool, questions ...Object) (*nsgotest.App, *questionAPI) {
	t.Helper()
	if len(questions) == 0 {
		questions = []Object{
			{"id": "scope", "question": "这次调整希望应用到哪些设备？", "options": []Object{{"label": "仅当前设备", "description": "只调整这台设备上的显示方式。"}, {"label": "所有已连接设备", "description": "保持本机与远程设备的体验一致。"}}},
			{"id": "notes", "question": "还有其他要求吗？"},
		}
	}
	f := &questionAPI{demoAPI: newDemoAPI(), writes: make(chan questionSubmission, 4), approvals: []Object{
		{"id": "question", "sessionId": "demo-chat", "allowSkip": optional, "questions": questions},
		{"id": "foreign", "sessionId": "unopened-session", "questions": []Object{{"id": "other", "question": "Must not show"}}},
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		// Test the real session subscription and decisions with an isolated
		// directory. OS window occlusion must not suspend this form fixture.
		s := newState(cx, f)
		s.loadAppearance()
		s.setAppearance(choose(dark, "dark", "light"))
		s.ready = true
		s.devices = []Object{{"id": "demo-device", "name": "Test device", "connected": true}}
		s.workspaces = map[string]workspaceCache{"demo-device": {providers: demoProviders()}}
		for _, session := range f.sessions {
			session = copyObject(session)
			session["deviceId"] = "demo-device"
			s.sessions = append(s.sessions, session)
		}
		return s.workspaceView(cx)
	}, nsgotest.Size(width, 844), nsgotest.Environment(core.Environment{Dark: dark}))
	openDemoChat(t, app)
	app.WaitFor("id:question-card-question", 3*time.Second)
	return app, f
}
func awaitQuestion(t *testing.T, f *questionAPI) questionSubmission {
	t.Helper()
	select {
	case write := <-f.writes:
		return write
	case <-time.After(3 * time.Second):
		t.Fatal("question answer not sent")
		return questionSubmission{}
	}
}

func TestQuestionOptionsCustomAnswersAndRecovery(t *testing.T) {
	for _, width := range []core.Points{390, 1100} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%g-dark-%t", width, dark), func(t *testing.T) {
				app, f := questionUIFixture(t, width, dark, false)
				if app.Exists("id:question-card-foreign") || app.Exists("id:approve-question") || app.Exists("id:question-skip-question") {
					t.Fatal("question shown as unrelated permission or skippable sync request")
				}
				if !app.Find("id:question-submit-question").Props.Disabled {
					t.Fatal("empty answer can submit")
				}
				activateTestControl(app, "question-option-question-0-1")
				app.WaitFor("id:question-input-question-1", time.Second)
				if len(f.writes) != 0 {
					t.Fatal("submitted before all required questions were answered")
				}
				app.Type("id:question-input-question-1", "保留键盘操作")
				activateTestControl(app, "question-back-question")
				if !app.Find("id:question-option-question-0-1").Props.Checked {
					t.Fatal("back navigation lost choice")
				}
				if !app.Find("id:question-submit-question").Props.Disabled {
					t.Fatal("Send must apply only to custom text")
				}
				app.Screenshot(fmt.Sprintf("question-options-%g-dark-%t", width, dark))
				activateTestControl(app, "question-option-question-0-1")
				app.WaitFor("id:question-input-question-1", time.Second)
				activateTestControl(app, "question-submit-question")
				write := awaitQuestion(t, f)
				if write.target != "demo-device" || str(write.request, "session") != "demo-chat" || str(write.request, "provider") != "codex-app" {
					t.Fatalf("answer routed incorrectly: %+v", write)
				}
				answers := object(object(write.request["decision"])["answers"])
				if object(answers["scope"])["answers"].([]string)[0] != "所有已连接设备" || object(answers["notes"])["answers"].([]string)[0] != "保留键盘操作" {
					t.Fatalf("wrong answers: %v", answers)
				}
				app.WaitFor("id:question-submit-question-loading", time.Second)
				if !app.Find("id:question-input-question-1").Props.Disabled {
					t.Fatal("pending answer remains editable")
				}
				app.PressAt("id:question-submit-question", core.Point{X: 8, Y: 8})
				if len(f.writes) != 0 {
					t.Fatal("duplicate submission")
				}
				write.result <- errors.New("Fixture: connection rejected before submission")
				app.WaitUntil(time.Second, func() bool { return !app.Find("id:question-submit-question").Props.Disabled })
				activateTestControl(app, "question-submit-question")
				retry := awaitQuestion(t, f)
				retry.result <- nil
				app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
			})
		}
	}
}

func TestQuestionSkipAndSessionNavigation(t *testing.T) {
	app, f := questionUIFixture(t, 1100, false, true)
	app.Type("id:question-input-question-0", "custom draft")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:question-input-question-0").Text == "custom draft" })
	activateTestControl(app, sessionRowID("all", Object{"deviceId": "demo-device", "providerId": "pi", "id": "demo-pi"}))
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
	activateTestControl(app, sessionRowID("all", Object{"deviceId": "demo-device", "providerId": "codex-app", "id": "demo-chat"}))
	app.WaitFor("id:question-input-question-0", 3*time.Second)
	if app.Find("id:question-input-question-0").Text != "custom draft" {
		t.Fatalf("navigation lost answer draft: %q", app.Find("id:question-input-question-0").Text)
	}
	activateTestControl(app, "question-skip-question")
	app.WaitFor("id:question-input-question-1", time.Second)
	activateTestControl(app, "question-skip-question")
	write := awaitQuestion(t, f)
	if str(object(write.request["decision"]), "decision") != "skip" || len(object(object(write.request["decision"])["answers"])) != 0 {
		t.Fatal("skip sent an answer")
	}
	write.result <- nil
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
}

func TestQuestionOptionConfirmsImmediatelyAndCanRetryRejection(t *testing.T) {
	app, f := questionUIFixture(t, 1100, false, false, Object{"id": "choice", "question": "选择范围", "options": []Object{{"label": "当前设备"}, {"label": "所有设备"}}})
	activateTestControl(app, "question-option-question-0-1")
	write := awaitQuestion(t, f)
	answers := object(object(write.request["decision"])["answers"])
	if object(answers["choice"])["answers"].([]string)[0] != "所有设备" {
		t.Fatalf("wrong option submitted: %v", answers)
	}
	app.WaitFor("id:question-option-loading-question", time.Second)
	if !app.Find("id:question-submit-question").Props.Disabled || app.Exists("id:question-submit-question-loading") {
		t.Fatal("option submission used the custom Send control")
	}
	app.PressAt("id:question-option-question-0-1", core.Point{X: 8, Y: 8})
	if len(f.writes) != 0 {
		t.Fatal("duplicate option submission")
	}
	write.result <- errors.New("Fixture: rejected before submission")
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:question-option-question-0-1").Props.Disabled })
	if !app.Find("id:question-option-question-0-1").Props.Checked {
		t.Fatal("rejection lost chosen answer")
	}
	activateTestControl(app, "question-option-question-0-1")
	retry := awaitQuestion(t, f)
	retry.result <- nil
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
}

func TestQuestionCustomTextMatchingOptionStillRequiresSend(t *testing.T) {
	app, f := questionUIFixture(t, 1100, false, false, Object{"id": "choice", "question": "选择范围", "options": []Object{{"label": "当前设备"}}})
	app.Type("id:question-input-question-0", "当前设备")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:question-input-question-0").Text == "当前设备" })
	if len(f.writes) != 0 || app.Find("id:question-option-question-0-0").Props.Checked {
		t.Fatal("custom text was treated as an option click")
	}
	activateTestControl(app, "question-submit-question")
	write := awaitQuestion(t, f)
	answers := object(object(write.request["decision"])["answers"])
	if object(answers["choice"])["answers"].([]string)[0] != "当前设备" {
		t.Fatalf("custom answer lost: %v", answers)
	}
	write.result <- nil
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
}

func TestQuestionCompactResponseAndDismissal(t *testing.T) {
	for _, width := range []core.Points{320, 1100} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%g-dark-%t", width, dark), func(t *testing.T) {
				app, f := questionUIFixture(t, width, dark, true, Object{"id": "status", "question": "切换到其他会话再切回来，消息会恢复到最新吗？", "options": []Object{
					{"label": "仍然停在旧消息"}, {"label": "切回来就恢复了"}, {"label": "还没试过"},
				}})
				if app.Exists("text:1 / 1") || app.Exists("id:question-back-question") {
					t.Fatal("single question has redundant step navigation")
				}
				app.Focus("id:question-input-question-0")
				app.Settle()
				option := globalBounds(app.Find("id:question-option-icon-question-0-0"))
				for _, id := range []string{"question-heading-icon-question", "question-input-icon-question"} {
					b := globalBounds(app.Find("id:" + id))
					if dx := b.Origin.X + b.Size.Width/2 - option.Origin.X - option.Size.Width/2; dx < -.5 || dx > .5 {
						t.Fatalf("leading icon columns differ: %s delta %g", id, dx)
					}
				}
				bar := globalBounds(app.Find("id:question-response-question"))
				for _, id := range []string{"question-input-question-0", "question-skip-question", "question-submit-question"} {
					b := globalBounds(app.Find("id:" + id))
					if b.Origin.X < bar.Origin.X || b.Origin.X+b.Size.Width > bar.Origin.X+bar.Size.Width+1 || b.Origin.Y < bar.Origin.Y || b.Origin.Y+b.Size.Height > bar.Origin.Y+bar.Size.Height+1 {
						t.Fatalf("response control escaped its shared row: %s %+v in %+v", id, b, bar)
					}
					if delta := b.Origin.Y + b.Size.Height/2 - bar.Origin.Y - bar.Size.Height/2; delta < -1 || delta > 1 {
						t.Fatalf("response control is not vertically centered: %s %g", id, delta)
					}
				}
				app.Screenshot(fmt.Sprintf("question-compact-%g-dark-%t", width, dark))
				app.Type("id:question-input-question-0", "暂存的自定义回答")
				activateTestControl(app, "question-dismiss-question")
				write := awaitQuestion(t, f)
				if decision := object(write.request["decision"]); str(decision, "decision") != "skip" || len(object(decision["answers"])) != 0 {
					t.Fatal("dismissal submitted the draft")
				}
				write.result <- nil
				app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
			})
		}
	}
}
