package hubui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

type outgoingSyncAPI struct {
	*independentStreamAPI
	saved chan Object
}

func (f *outgoingSyncAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.snapshot" {
		data, _ := json.Marshal(p["transcript"])
		var value Object
		_ = json.Unmarshal(data, &value)
		select {
		case f.saved <- value:
		default:
		}
		return nil, nil
	}
	return f.independentStreamAPI.Call(ctx, method, p, target, r)
}
func TestCompleteFreshHistoryClearsAbsentFailuresAndPersistsReconciliation(t *testing.T) {
	f := &outgoingSyncAPI{independentStreamAPI: &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}, saved: make(chan Object, 16)}
	app, s := conversationFixture(t, f, 390, Object{}, true)
	app.Post(func() {
		s.cx.Post(func() {
			settled := time.Now().Add(-time.Second)
			s.outgoing[s.key()] = []*outgoingMessage{
				{ID: "absent", Text: "failed and absent", Status: "failed", FinishedAt: settled},
				{ID: "echo", Text: "actually sent", Status: "failed", FinishedAt: settled},
				{ID: "inflight", Text: "still sending", Status: "sending"},
			}
			s.rebuildMessages()
			s.startSessionWatch()
		})
	})
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request := <-f.snapshots
	request.fragments <- provider.Transcript{Turns: []provider.Turn{{ID: "turn", Status: "completed", Items: []provider.Item{{ID: "partial", Role: "assistant", Text: "first history fragment"}}}}}
	app.WaitFor("text:first history fragment", time.Second)
	app.Find("id:delivery-absent")
	app.Find("id:retry-message-absent")
	request.reply <- outgoingTranscript(Object{"id": "remote", "role": "user", "text": "actually sent"}, Object{"id": "answer", "role": "assistant", "text": "fully synchronized"})
	app.WaitFor("text:fully synchronized", time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:delivery-absent") && !app.Exists("id:delivery-echo") })
	app.Find("id:delivery-inflight")
	// Cleanup must survive navigating away or reopening the app, rather than
	// leaving the old failed overlay in the next persisted projection.
	app.WaitUntil(2*time.Second, func() bool {
		for {
			select {
			case value := <-f.saved:
				local := objects(value["localMessages"])
				if len(local) == 1 && str(local[0], "id") == "inflight" {
					return true
				}
			default:
				return false
			}
		}
	})
	app.Screenshot("failed-message-reconciled-after-sync")
}
