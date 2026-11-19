package hubui

import (
	"slices"
	"strings"
)

type groupDirectory struct{ Target, Path string }

func appendProjectDetails(repositories []string, directories []groupDirectory, session Object) ([]string, []groupDirectory) {
	if repo := str(object(session["gitInfo"]), "repository"); repo != "" && !slices.Contains(repositories, repo) {
		repositories = append(repositories, repo)
	}
	roots := []string{}
	switch values := object(session["project"])["roots"].(type) {
	case []string:
		roots = values
	case []any:
		for _, value := range values {
			if path, ok := value.(string); ok && path != "" {
				roots = append(roots, path)
			}
		}
	}
	if len(roots) == 0 {
		roots = []string{projectDirectory(session).Path}
	}
	for _, path := range roots {
		if path == "" {
			continue
		}
		dir := groupDirectory{Target: str(session, "deviceId"), Path: path}
		if !slices.ContainsFunc(directories, func(old groupDirectory) bool {
			return old.Target == dir.Target && projectPath(old.Path) == projectPath(path)
		}) {
			directories = append(directories, dir)
		}
	}
	return repositories, directories
}

func agentFamily(id string) string {
	if id == "codex-app" {
		return "codex"
	}
	return id
}

// Only normalize path syntax. The host supplies projectDir; a controller must
// never inspect its own filesystem to interpret another device's directories.
func projectPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if (len(path) > 1 && path[1] == ':') || strings.HasPrefix(path, "//") {
		path = strings.ToLower(path)
	}
	return path
}

func projectDirectory(session Object) Directory {
	path := str(session, "projectDir")
	if path == "" {
		path = str(session, "cwd")
	}
	name := str(object(session["project"]), "name")
	if name == "" {
		name = "无工作目录"
		parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
	}
	return Directory{Key: projectPath(path), Path: path, Title: name}
}
