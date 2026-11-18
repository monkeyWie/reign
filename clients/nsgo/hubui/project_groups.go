package hubui

import "strings"

func agentFamily(id string) string {
	if id == "codex-app" || id == "codex-cli" {
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
