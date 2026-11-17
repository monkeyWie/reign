package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/workspace"
)

type codexProject struct {
	ID, Name string
	Roots    []struct{ Path string }
}
type projectRoot struct {
	path, gitDir string
	project      provider.Project
}
type projectIndex struct {
	byID        map[string]provider.Project
	assignments map[string]provider.Project
	roots       []projectRoot
	directories sync.Map
}

// Project metadata is small and shared by every session page. Failure is
// optional: older Agent servers can still list and control sessions normally.
func (p *Provider) refreshProjects(ctx context.Context) {
	p.projectMu.Lock()
	defer p.projectMu.Unlock()
	if time.Since(p.projectsAt) < time.Minute {
		return
	}
	p.projectsAt = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var projects []codexProject
	cursor := ""
	for {
		var page struct {
			Data       []codexProject
			NextCursor string
		}
		if err := p.call(ctx, "project/list", map[string]any{"limit": 100, "cursor": cursor}, &page); err != nil {
			if p.projects != nil {
				return
			}
			break
		}
		projects = append(projects, page.Data...)
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	index := newProjectIndex(projects)
	index.loadDesktopAssignments(codexProjectHome())
	p.projects = index
}
func newProjectIndex(projects []codexProject) *projectIndex {
	index := &projectIndex{byID: map[string]provider.Project{}}
	for _, project := range projects {
		info := provider.Project{ID: project.ID, Name: project.Name}
		if len(project.Roots) > 0 {
			info.Root = project.Roots[0].Path
		}
		index.byID[project.ID] = info
		for _, root := range project.Roots {
			if root.Path == "" {
				continue
			}
			index.roots = append(index.roots, projectRoot{workspace.CanonicalPath(root.Path), workspace.GitCommonDir(root.Path), info})
		}
	}
	return index
}
func (p *Provider) sessionProject(t thread) *provider.Project {
	p.projectMu.Lock()
	index := p.projects
	p.projectMu.Unlock()
	if index != nil {
		if project, ok := index.byID[t.ProjectID]; ok {
			return &project
		}
		if t.ProjectID == "" {
			if project, ok := index.assignments[t.ID]; ok {
				return &project
			}
			return index.forDirectory(t.Cwd)
		}
	}
	if t.ProjectID != "" {
		return &provider.Project{ID: t.ProjectID}
	}
	return nil
}
func (index *projectIndex) forDirectory(cwd string) *provider.Project {
	if cwd == "" {
		return nil
	}
	if cached, ok := index.directories.Load(cwd); ok {
		return cached.(*provider.Project)
	}
	path := workspace.CanonicalPath(cwd)
	var best *provider.Project
	longest := 0
	for _, root := range index.roots {
		if (path == root.path || strings.HasPrefix(path, root.path+string(filepath.Separator))) && len(root.path) > longest {
			project := root.project
			best = &project
			longest = len(root.path)
		}
	}
	if best == nil {
		gitDir := workspace.GitCommonDir(path)
		if gitDir != "" {
			for _, root := range index.roots {
				if root.gitDir == gitDir {
					project := root.project
					best = &project
					break
				}
			}
		}
	}
	index.directories.Store(cwd, best)
	return best
}

// Desktop can still own assignments while its app-server migration is pending.
// Read those assignments without changing either Codex's state or membership.
// Once thread.projectId is populated, it always wins over this local metadata.
func (index *projectIndex) loadDesktopAssignments(home string) {
	file, err := os.Open(filepath.Join(home, ".codex-global-state.json"))
	if err != nil {
		return
	}
	defer file.Close()
	var state struct {
		Projects map[string]struct {
			ID, Name  string
			RootPaths []string
		} `json:"local-projects"`
		Assignments map[string]struct{ ProjectKind, ProjectID string } `json:"thread-project-assignments"`
		Migrated    map[string]map[string]string                       `json:"app-server-project-id-by-legacy-project-id-by-host"`
	}
	if json.NewDecoder(io.LimitReader(file, 16<<20)).Decode(&state) != nil {
		return
	}
	mapping := state.Migrated["local:"+home]
	index.assignments = map[string]provider.Project{}
	for thread, assignment := range state.Assignments {
		if assignment.ProjectKind != "local" {
			continue
		}
		local, ok := state.Projects[assignment.ProjectID]
		if !ok {
			continue
		}
		project := provider.Project{ID: assignment.ProjectID, Name: local.Name}
		if len(local.RootPaths) > 0 {
			project.Root = local.RootPaths[0]
		}
		if id := mapping[assignment.ProjectID]; id != "" {
			project.ID = id
			if canonical, ok := index.byID[id]; ok {
				project = canonical
			}
		}
		index.assignments[thread] = project
	}
}
func codexProjectHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}
