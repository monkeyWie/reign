package codex

import (
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func TestActivitySummaryDistinguishesWaitingFailureAndUnknown(t *testing.T) {
	for _, tc := range []struct{ runtime, turn, want string }{
		{"active", "completed", "running"}, {"idle", "inProgress", "running"},
		{"idle", "completed", "completed"}, {"systemError", "inProgress", "failed"},
		{"", "", "unknown"},
	} {
		ss := provider.Session{ID: "s", Status: "unknown"}
		state := map[string]any{"id": "s", "threadRuntimeStatus": map[string]any{"type": tc.runtime}}
		if tc.turn != "" {
			state["turns"] = []any{map[string]any{"turnId": "t", "status": tc.turn}}
		}
		applyIPCActivity(&ss, rawJSON(state))
		if ss.Status != tc.want {
			t.Fatal(tc, ss.Status)
		}
	}
	ss := provider.Session{ID: "s", Status: "unknown"}
	applyIPCActivity(&ss, rawJSON(map[string]any{"id": "s", "threadRuntimeStatus": map[string]any{"type": "active", "activeFlags": []any{"waitingOnApproval"}}}))
	if ss.Status != "waiting" {
		t.Fatal("approval was not classified as waiting")
	}
}
