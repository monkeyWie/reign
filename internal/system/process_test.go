package system

import (
	"github.com/monkeyWie/reignhub/internal/provider"
	"testing"
)

func TestInstanceGenerationGuard(t *testing.T) {
	a := []provider.Instance{{PID: 42, StartedAt: "today", Path: "/Applications/Codex.app"}}
	b := []provider.Instance{{PID: 42, StartedAt: "tomorrow", Path: a[0].Path}}
	if SameInstances(a, b) {
		t.Fatal("recycled PID accepted")
	}
	if !SameInstances(a, a) || SameInstances(a, nil) {
		t.Fatal("invalid comparison")
	}
}
