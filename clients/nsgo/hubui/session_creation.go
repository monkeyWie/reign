package hubui

// Project creation keeps each device's own path. A shared project key does not
// imply that its checkout has the same path on another device.
type projectLocation struct {
	Target, Provider, Path string
}

func (s *State) openSessionCreation(project *directoryRow) {
	s.createProject = nil
	if project != nil {
		s.createProject = []projectLocation{}
		seen := map[projectLocation]bool{}
		for _, ss := range s.filteredSessions() {
			target, agent := str(ss, "deviceId"), str(ss, "providerId")
			if s.pinned[sessionIdentity(ss)] != 0 || sessionProjectKey(ss) != project.Project ||
				(project.Target != "" && project.Target != target) || (project.Provider != "" && project.Provider != agent) {
				continue
			}
			location := projectLocation{Target: target, Provider: agent, Path: projectDirectory(ss).Path}
			if !seen[location] {
				seen[location] = true
				s.createProject = append(s.createProject, location)
			}
		}
	}
	s.dialog.Set("create")
}

func projectLocationFor(locations []projectLocation, target string) projectLocation {
	for _, location := range locations {
		if location.Target == target {
			return location
		}
	}
	return projectLocation{}
}
