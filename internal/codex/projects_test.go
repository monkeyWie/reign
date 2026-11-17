package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsResolveWorktreesAndRespectExplicitAssignment(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "gopeed")
	work := filepath.Join(root, "workspaces", "feature")
	gitDir := filepath.Join(repo, ".git")
	workGit := filepath.Join(gitDir, "worktrees", "feature")
	for _, dir := range []string{workGit, filepath.Join(work, "src")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, ".git"), []byte("gitdir: "+workGit+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workGit, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projects := []codexProject{{ID: "gopeed", Name: "Gopeed"}, {ID: "other", Name: "Other"}}
	projects[0].Roots = append(projects[0].Roots, struct{ Path string }{repo})
	p := &Provider{projects: newProjectIndex(projects)}
	for _, cwd := range []string{repo, work, filepath.Join(work, "src")} {
		session := p.session(thread{Cwd: cwd})
		if session.Project == nil || session.Project.ID != "gopeed" || session.Project.Name != "Gopeed" || session.Project.Root != repo || session.CWD != cwd {
			t.Fatalf("lost project/execution directory: %+v", session)
		}
	}
	explicit := p.session(thread{Cwd: work, ProjectID: "other"})
	if explicit.Project.ID != "other" {
		t.Fatal("cwd overrode explicit Agent assignment")
	}
	unknown := p.session(thread{Cwd: work, ProjectID: "not-listed"})
	if unknown.Project.ID != "not-listed" {
		t.Fatal("unresolved project assignment was discarded")
	}
	unrelated := p.session(thread{Cwd: filepath.Join(root, "elsewhere", "gopeed")})
	if unrelated.Project != nil {
		t.Fatal("unrelated same-name directory was merged")
	}
}

func TestProjectRootMatchingPrefersNestedProjectAndHandlesSymlink(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	outer, inner := codexProject{ID: "outer", Name: "Outer"}, codexProject{ID: "inner", Name: "Inner"}
	outer.Roots = append(outer.Roots, struct{ Path string }{alias})
	inner.Roots = append(inner.Roots, struct{ Path string }{nested})
	index := newProjectIndex([]codexProject{outer, inner})
	if got := index.forDirectory(filepath.Join(alias, "nested")); got == nil || got.ID != "inner" {
		t.Fatalf("nested project lost: %+v", got)
	}
	if got := index.forDirectory(root + "-other"); got != nil {
		t.Fatal("matched a partial path prefix")
	}
}

func TestDesktopAssignmentWinsOverWorktreeUntilServerMigration(t *testing.T) {
	home := t.TempDir()
	state, err := json.Marshal(map[string]any{
		"local-projects":                                     map[string]any{"legacy": map[string]any{"id": "legacy", "name": "Website", "rootPaths": []string{"/website"}}},
		"thread-project-assignments":                         map[string]any{"thread": map[string]string{"projectKind": "local", "projectId": "legacy"}},
		"app-server-project-id-by-legacy-project-id-by-host": map[string]any{"local:" + home: map[string]string{"legacy": "canonical"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	project := codexProject{ID: "canonical", Name: "Website"}
	project.Roots = append(project.Roots, struct{ Path string }{"/website"})
	index := newProjectIndex([]codexProject{project})
	index.loadDesktopAssignments(home)
	p := &Provider{projects: index}
	session := p.session(thread{ID: "thread", Cwd: "/worktrees/gopeed"})
	if session.Project == nil || session.Project.ID != "canonical" || session.Project.Name != "Website" || session.CWD != "/worktrees/gopeed" {
		t.Fatalf("lost desktop project assignment: %+v", session)
	}
	session = p.session(thread{ID: "thread", Cwd: "/worktrees/gopeed", ProjectID: "server-assignment"})
	if session.Project.ID != "server-assignment" {
		t.Fatal("local metadata overrode server membership")
	}
}
