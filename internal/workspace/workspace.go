// Package workspace resolves device-local project paths for every Agent.
package workspace

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func CanonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

type checkout struct{ root, repository, gitDir string }

// Resolver caches filesystem reads for one catalogue snapshot/request. Nothing
// is persisted: newly created projects and worktrees are picked up next time.
type Resolver struct{ paths map[string]checkout }

func (r *Resolver) directory(path string) checkout {
	path = CanonicalPath(path)
	if value, ok := r.paths[path]; ok {
		return value
	}
	if r.paths == nil {
		r.paths = map[string]checkout{}
	}
	var visited []string
	result := checkout{}
	for dir := path; ; dir = filepath.Dir(dir) {
		if cached, ok := r.paths[dir]; ok && cached.gitDir != "" {
			result = cached
			break
		}
		visited = append(visited, dir)
		marker := filepath.Join(dir, ".git")
		if info, err := os.Stat(marker); err == nil {
			result = checkout{root: dir, repository: dir}
			if info.IsDir() {
				result.gitDir = CanonicalPath(marker)
			} else if raw, err := os.ReadFile(marker); err == nil && strings.HasPrefix(strings.TrimSpace(string(raw)), "gitdir:") {
				gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
				if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
					common := strings.TrimSpace(string(raw))
					if !filepath.IsAbs(common) {
						common = filepath.Join(gitDir, common)
					}
					gitDir = common
					if filepath.Base(filepath.Clean(common)) == ".git" {
						result.repository = CanonicalPath(filepath.Dir(common))
					}
				}
				result.gitDir = CanonicalPath(gitDir)
			}
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if result.root == "" {
		// Unversioned directories have no implicit relationship to siblings.
		result.root, result.repository = path, path
		r.paths[path] = result
	} else {
		for _, dir := range visited {
			r.paths[dir] = result
		}
	}
	return result
}

func GitCommonDir(path string) string {
	var resolver Resolver
	return resolver.directory(path).gitDir
}

func (r *Resolver) Session(session provider.Session) provider.Session {
	if session.ProjectDir != "" {
		session.ProjectDir = CanonicalPath(session.ProjectDir)
	} else if session.Project != nil && session.Project.Root != "" {
		session.ProjectDir = CanonicalPath(session.Project.Root)
	} else if session.CWD != "" {
		info := r.directory(session.CWD)
		session.ProjectDir = info.repository
	}
	return session
}

// Result enriches the common API boundary without mutating provider snapshots.
func Result(value any) any {
	var resolver Resolver
	switch result := value.(type) {
	case provider.Session:
		return resolver.Session(result)
	case provider.SessionPage:
		if result.Sessions != nil {
			rows := make([]provider.Session, len(result.Sessions))
			copy(rows, result.Sessions)
			result.Sessions = rows
		}
		for i := range result.Sessions {
			result.Sessions[i] = resolver.Session(result.Sessions[i])
		}
		return result
	case provider.Transcript:
		result.Session = resolver.Session(result.Session)
		return result
	default:
		return value
	}
}
