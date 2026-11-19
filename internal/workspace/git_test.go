package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func TestGitMetadataUsesExecutionCheckoutAndRefreshes(t *testing.T) {
	repo, work := filepath.Join(t.TempDir(), "repo"), filepath.Join(t.TempDir(), "work")
	git := filepath.Join(repo, ".git")
	workGit := filepath.Join(git, "worktrees", "feature")
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(git, "HEAD"), "ref: refs/heads/main\n")
	write(filepath.Join(git, "config"), "[remote \"origin\"]\nurl = https://fixture-user:fixture-secret@example.com/team/repo.git?token=fixture-secret#fragment\n")
	write(filepath.Join(work, ".git"), "gitdir: "+workGit+"\n")
	write(filepath.Join(workGit, "commondir"), "../..\n")
	write(filepath.Join(workGit, "HEAD"), "ref: refs/heads/feature/sidebar\n")
	var resolver Resolver
	main := resolver.Session(provider.Session{CWD: repo})
	linked := resolver.Session(provider.Session{CWD: work, ProjectDir: repo})
	if main.GitInfo.Branch != "main" || linked.GitInfo.Branch != "feature/sidebar" || linked.ProjectDir != main.ProjectDir || linked.ProjectKey != main.ProjectKey {
		t.Fatal("worktree lost its own branch or shared project identity", main, linked)
	}
	if linked.GitInfo.Repository != "example.com/team/repo" {
		t.Fatal("unsafe or missing repository name", linked.GitInfo)
	}
	write(filepath.Join(workGit, "HEAD"), strings.Repeat("a", 40)+"\n")
	// Only one read per checkout in a snapshot; the next request sees changes.
	if resolver.Session(provider.Session{CWD: work}).GitInfo.Branch != "feature/sidebar" {
		t.Fatal("snapshot cache changed mid-request")
	}
	resolver = Resolver{}
	detached := resolver.Session(provider.Session{CWD: work})
	if detached.GitInfo.Branch != "" || detached.GitInfo.Commit != strings.Repeat("a", 40) {
		t.Fatal("detached HEAD reported a branch", detached.GitInfo)
	}
	write(filepath.Join(workGit, "HEAD"), "ref: refs/heads/fixed\n")
	resolver = Resolver{}
	if got := resolver.Session(provider.Session{CWD: work}).GitInfo; got.Branch != "fixed" || got.Commit != "" {
		t.Fatal("branch switch failed to recover", got)
	}
}

func TestGitMetadataOptionalAndBounded(t *testing.T) {
	root := t.TempDir()
	var r Resolver
	if r.Session(provider.Session{CWD: root}).GitInfo != nil {
		t.Fatal("non-Git directory acquired Git metadata")
	}
	git := filepath.Join(root, ".git")
	if err := os.MkdirAll(git, 0700); err != nil {
		t.Fatal(err)
	}
	for _, head := range []string{"", "ref: refs/tags/v1", "ref: refs/heads/bad\nbranch", strings.Repeat("x", 5000)} {
		if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte(head), 0600); err != nil {
			t.Fatal(err)
		}
		r = Resolver{}
		got := r.Session(provider.Session{CWD: root}).GitInfo
		if got == nil || got.Branch != "" || got.Commit != "" {
			t.Fatal("unknown metadata fabricated a branch", got)
		}
	}
	if err := os.Remove(filepath.Join(git, "HEAD")); err != nil {
		t.Fatal(err)
	}
	r = Resolver{}
	if got := r.Session(provider.Session{CWD: root}).GitInfo; got.Branch != "" {
		t.Fatal("missing HEAD fabricated a branch")
	}
	for _, remote := range []string{"git@example.com:team/repo.git", "ssh://git@example.com/team/repo.git", "https://u:p@example.com/team/repo.git?token=secret#fragment"} {
		if got := repositoryAddress(remote); got != "example.com/team/repo" {
			t.Fatalf("repository display mismatch: %q", got)
		}
	}
	if repositoryAddress("file:///private/repo") != "" || repositoryAddress("/private/repo") != "" {
		t.Fatal("local remote path exposed")
	}
}
