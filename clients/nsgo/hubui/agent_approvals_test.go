package hubui

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type agentApprovalAPI struct {
	*demoAPI
	input     bool
	decisions chan Object
}

func (a *agentApprovalAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && str(r, "method") == "approvals" {
		params := Object{"title": "Agent permission", "options": []Object{{"id": "always", "name": "Always allow", "kind": "allow_always"}, {"id": "once", "name": "Allow once", "kind": "allow_once"}, {"id": "deny", "name": "Reject", "kind": "reject_once"}}}
		if a.input {
			params = Object{"title": "Your answer", "input": true, "value": "initial"}
		}
		return []Object{{"id": "approval", "sessionId": "demo-chat", "params": params}}, nil
	}
	if method == "client.call" && str(r, "method") == "decide" {
		a.decisions <- object(r["decision"])
		return nil, nil
	}
	return a.demoAPI.Call(ctx, method, p, target, r)
}
func TestAgentPermissionUsesExactOption(t *testing.T) {
	api := &agentApprovalAPI{demoAPI: newDemoAPI(), decisions: make(chan Object, 1)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, api, true) }, nsgotest.Size(390, 844))
	openDemoChat(t, app)
	app.WaitFor("id:approval-option-approval-once", 3*time.Second)
	if app.Exists("id:approve-approval") {
		t.Fatal("ACP permission fell back to generic allow button")
	}
	app.Press("id:approval-option-approval-once")
	select {
	case decision := <-api.decisions:
		if str(decision, "decision") != "once" {
			t.Fatalf("sent wrong permission: %v", decision)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("permission not submitted")
	}
}
func TestAgentTextInputUsesStructuredAnswer(t *testing.T) {
	api := &agentApprovalAPI{demoAPI: newDemoAPI(), input: true, decisions: make(chan Object, 1)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, api, true) }, nsgotest.Size(390, 844))
	openDemoChat(t, app)
	app.WaitFor("id:approval-input-approval", 3*time.Second)
	app.Clear("id:approval-input-approval")
	app.Type("id:approval-input-approval", "my answer")
	app.Press("id:approval-submit-approval")
	select {
	case decision := <-api.decisions:
		if str(decision, "decision") != "input" {
			t.Fatalf("not a text answer: %v", decision)
		}
		value := object(object(decision["answers"])["value"])
		answers := value["answers"].([]string)
		if len(answers) != 1 || answers[0] != "my answer" {
			t.Fatalf("wrong answer: %v", value)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("text answer not submitted")
	}
}
