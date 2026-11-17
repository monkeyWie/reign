package syncstate

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/internal/workspace"
)

type projectProvider struct {
	provider.Provider
	session provider.Session
}

func (p *projectProvider) Status(context.Context) provider.Status {
	return provider.Status{State: "ready"}
}
func (p *projectProvider) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{p.session}}, nil
}

func TestProjectDirectoryMatchesFirstPageAndIncrementalCatalogue(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index := &Index{DB: db, Epoch: "epoch"}
	cwd, project := t.TempDir(), t.TempDir()
	p := &projectProvider{session: provider.Session{ID: "thread", ProviderID: "any-agent", CWD: cwd, Project: &provider.Project{ID: "native", Root: project}}}
	dispatcher := &control.Dispatcher{Providers: map[string]provider.Provider{"any-agent": p}}
	response := dispatcher.Handle(context.Background(), control.Request{Method: "sessions", Provider: "any-agent"})
	var page provider.SessionPage
	if response.Error != "" || json.Unmarshal(response.Result, &page) != nil || len(page.Sessions) != 1 {
		t.Fatal("first page failed", response)
	}
	delta, err := index.Sync(context.Background(), p, "any-agent", "", 0)
	if err != nil || !delta.Reset || len(delta.Items) != 1 {
		t.Fatal("first sync failed", delta, err)
	}
	var synced provider.Session
	if json.Unmarshal(delta.Items[0], &synced) != nil || synced.ProjectDir != page.Sessions[0].ProjectDir || synced.ProjectDir != workspace.CanonicalPath(project) || synced.CWD != cwd {
		t.Fatal("first metadata page and catalogue disagree", synced, page)
	}
	unchanged, err := index.Sync(context.Background(), p, "any-agent", index.Epoch, delta.Cursor)
	if err != nil || unchanged.Reset || len(unchanged.Changes) != 0 {
		t.Fatal("unchanged project caused extra sync traffic", unchanged, err)
	}
	// A native reassignment must flow through the incremental cache even when
	// the conversation has not received any new messages.
	p.session.Project = &provider.Project{ID: "standalone-worktree", Root: cwd}
	changed, err := index.Sync(context.Background(), p, "any-agent", index.Epoch, unchanged.Cursor)
	if err != nil || changed.Reset || len(changed.Changes) != 1 {
		t.Fatal("project reassignment was not delivered incrementally", changed, err)
	}
	if json.Unmarshal(changed.Changes[0].Value, &synced) != nil || synced.ProjectDir != workspace.CanonicalPath(cwd) || synced.CWD != cwd {
		t.Fatal("lost worktree project reassignment", synced)
	}
}
