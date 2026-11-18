package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func TestAgentsResolveSharedProjectAndPreserveWorktreeAssignments(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "gopeed")
	work := filepath.Join(root, "feature", "gopeed")
	gitDir := filepath.Join(repo, ".git")
	workGit := filepath.Join(gitDir, "worktrees", "feature")
	for _, dir := range []string{workGit, filepath.Join(repo, "ui"), filepath.Join(work, "src")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, text := range map[string]string{
		filepath.Join(work, ".git"):         "gitdir: " + workGit + "\n",
		filepath.Join(workGit, "commondir"): "../..\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var resolver Resolver
	for _, agent := range []string{"codex-app", "claude-code", "pi", "future-acp"} {
		for _, cwd := range []string{repo, filepath.Join(repo, "ui"), work, filepath.Join(work, "src")} {
			session := resolver.Session(provider.Session{ProviderID: agent, CWD: cwd})
			if session.ProjectDir != CanonicalPath(repo) || session.CWD != cwd || session.ProviderID != agent {
				t.Fatalf("lost generic project identity or execution route: %+v", session)
			}
		}
	}
	if GitCommonDir(work) != CanonicalPath(gitDir) {
		t.Fatal("lost shared Git identity")
	}
	for _, project := range []provider.Project{
		{ID: "new-project", Root: work},
		{ID: "website", Root: filepath.Join(root, "website")},
	} {
		session := resolver.Session(provider.Session{CWD: work, Project: &project})
		if session.ProjectDir != CanonicalPath(project.Root) || session.CWD != work || session.Project != &project {
			t.Fatalf("inference overrode explicit assignment or mutated snapshot: %+v", session)
		}
	}
	explicit := filepath.Join(root, "explicit")
	session := resolver.Session(provider.Session{CWD: work, ProjectDir: explicit, Project: &provider.Project{Root: repo}})
	if session.ProjectDir != CanonicalPath(explicit) {
		t.Fatal("native projectDir was replaced")
	}
}

func TestUnrelatedDirectoriesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "gopeed")
	second := filepath.Join(root, "two", "gopeed")
	for _, dir := range []string{first, second, filepath.Join(first, "child")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var resolver Resolver
	a := resolver.Session(provider.Session{CWD: first})
	b := resolver.Session(provider.Session{CWD: second})
	child := resolver.Session(provider.Session{CWD: filepath.Join(first, "child")})
	if a.ProjectDir == b.ProjectDir || a.ProjectDir == child.ProjectDir {
		t.Fatal("unversioned directories were merged by name or ancestor cache")
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Skip(err)
	}
	linked := resolver.Session(provider.Session{CWD: alias})
	if linked.ProjectDir != a.ProjectDir || linked.CWD != alias {
		t.Fatal("symlink identity changed execution directory", linked)
	}
}

func TestResultDoesNotMutateProviderPages(t *testing.T) {
	page := provider.SessionPage{Sessions: []provider.Session{{CWD: t.TempDir()}}}
	enriched := Result(page).(provider.SessionPage)
	if enriched.Sessions[0].ProjectDir == "" || page.Sessions[0].ProjectDir != "" {
		t.Fatal("enrichment mutated a shared provider page")
	}
	empty := Result(provider.SessionPage{Sessions: []provider.Session{}}).(provider.SessionPage)
	if empty.Sessions == nil {
		t.Fatal("empty JSON array became null")
	}
}

func TestRepositoryIdentityAcrossDevices(t *testing.T) {
	root := t.TempDir()
	makeRepo := func(name, remote string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[remote \"origin\"]\n url = "+remote+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	a := makeRepo("mac", "git@github.com:team/repo.git")
	b := makeRepo("linux", "https://fixture-user:fixture-secret@github.com/team/repo.git?access=fixture-secret#fragment")
	c := makeRepo("unrelated", "https://github.com/other/repo.git")
	var r Resolver
	first := r.Session(provider.Session{CWD: a})
	second := r.Session(provider.Session{CWD: b})
	other := r.Session(provider.Session{CWD: c})
	if first.ProjectKey == "" || first.ProjectKey != second.ProjectKey || first.ProjectKey == other.ProjectKey {
		t.Fatal("repository identity mismatch", first.ProjectKey, second.ProjectKey, other.ProjectKey)
	}
	if strings.Contains(second.ProjectKey, "fixture") || strings.Contains(second.ProjectKey, "github.com") {
		t.Fatal("raw origin leaked")
	}
	sub := r.Session(provider.Session{CWD: filepath.Join(b, "ui"), ProjectDir: filepath.Join(b, "ui")})
	if sub.ProjectKey == first.ProjectKey || sub.ProjectKey == "" {
		t.Fatal("subproject identity lost")
	}
	if repositoryIdentity("/local/repo") != "" || repositoryIdentity("file:///local/repo") != "" {
		t.Fatal("local origin acquired global identity")
	}
}
