package provider

import "testing"

func TestSDKSessionVisibility(t *testing.T) {
	for _, id := range []string{"codex-app", "codex-cli"} {
		for _, s := range []Session{
			{Source: `"exec"`},
			{SourceKind: "exec"},
			{Source: `"appServer"`, Originator: "codex_sdk_ts"},
			{Originator: "codex_sdk"},
		} {
			s.ProviderID = id
			if SyncSession(s) {
				t.Fatalf("SDK included: %+v", s)
			}
		}
		for _, s := range []Session{
			{Source: `"cli"`},
			{Source: `"vscode"`, Originator: "Codex Desktop"},
			{Source: `"appServer"`, Originator: "reignhub"},
			{Source: `"unknown"`},
		} {
			s.ProviderID = id
			if !SyncSession(s) {
				t.Fatalf("interactive session excluded: %+v", s)
			}
		}
	}
	if !SyncSession(Session{ProviderID: "claude-code", SourceKind: "exec"}) {
		t.Fatal("unrelated provider filtered")
	}
}
