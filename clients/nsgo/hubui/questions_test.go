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
func questionUIFixture(t *testing.T, width core.Points, dark, optional bool) (*nsgotest.App, *questionAPI) {
	t.Helper()
	f := &questionAPI{demoAPI: newDemoAPI(), writes: make(chan questionSubmission, 4), approvals: []Object{
		{"id": "question", "sessionId": "demo-chat", "allowSkip": optional, "questions": []Object{
			{"id": "scope", "question": "这次调整希望应用到哪些设备？", "options": []Object{{"label": "仅当前设备", "description": "只调整这台设备上的显示方式。"}, {"label": "所有已连接设备", "description": "保持本机与远程设备的体验一致。"}}},
			{"id": "notes", "question": "还有其他要求吗？"},
		}}, {"id": "foreign", "sessionId": "unopened-session", "questions": []Object{{"id": "other", "question": "Must not show"}}},
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
				if len(f.writes) != 0 {
					t.Fatal("selection submitted without confirmation")
				}
				app.Screenshot(fmt.Sprintf("question-options-%g-dark-%t", width, dark))
				activateTestControl(app, "question-submit-question")
				app.WaitFor("id:question-input-question-1", time.Second)
				app.Type("id:question-input-question-1", "保留键盘操作")
				activateTestControl(app, "question-back-question")
				if !app.Find("id:question-option-question-0-1").Props.Checked {
					t.Fatal("back navigation lost choice")
				}
				activateTestControl(app, "question-submit-question")
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
	activateTestControl(app, sessionRowID("all", Object{"deviceId": "demo-device", "providerId": "codex-cli", "id": "demo-cli"}))
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:question-card-question") })
	activateTestControl(app, sessionRowID("all", Object{"deviceId": "demo-device", "providerId": "codex-app", "id": "demo-chat"}))
	app.WaitFor("id:question-input-question-0", 3*time.Second)
	if app.Find("id:question-input-question-0").Text != "custom draft" {
		t.Fatal("navigation lost answer draft")
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
