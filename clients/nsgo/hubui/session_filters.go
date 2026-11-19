package hubui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// Filters are temporary view state. An empty dimension includes everything;
// choices within it are ORed, while the three dimensions are ANDed.
type sessionFilters struct {
	Devices, Agents, Projects map[string]bool
}

func (f sessionFilters) count() int { return len(f.Devices) + len(f.Agents) + len(f.Projects) }

func sessionProjectKey(ss Object) string {
	identity := []string{"repository", str(ss, "projectKey")}
	if identity[1] == "" {
		identity = []string{"directory", str(ss, "deviceId"), projectDirectory(ss).Key}
	}
	b, _ := json.Marshal(identity)
	return string(b)
}

func (s *State) filteredSessions() []Object { return s.sessionsMatching(s.filters) }

func (s *State) sectionFilters(section string) *sessionFilters {
	switch section {
	case "recent":
		return &s.recentFilters
	case "pinned":
		return &s.pinnedFilters
	}
	return &s.filters
}

func (s *State) sessionsMatching(f sessionFilters) []Object {
	result := uniqueSessions(s.sessions)
	filtered := result[:0]
	for _, ss := range result {
		if s.removedSessions[sessionIdentity(ss)] {
			continue
		}
		if (len(f.Devices) == 0 || f.Devices[str(ss, "deviceId")]) &&
			(len(f.Agents) == 0 || f.Agents[str(ss, "providerId")]) &&
			(len(f.Projects) == 0 || f.Projects[sessionProjectKey(ss)]) {
			filtered = append(filtered, ss)
		}
	}
	return filtered
}

func (s *State) filtersChanged() {
	s.catalogueVersion++
	s.changed()
}

func (s *State) clearSessionFilters(section string) {
	*s.sectionFilters(section) = sessionFilters{}
	s.filtersChanged()
}

type sessionFilterOption struct{ Key, Title, Detail string }

func (s *State) sessionFilterOptions(dimension string) []sessionFilterOption {
	options := map[string]sessionFilterOption{}
	projectDevices := map[string]map[string]string{}
	// Build from the whole directory so selecting one filter never removes
	// the controls needed to undo or combine another filter.
	for _, ss := range uniqueSessions(s.sessions) {
		var option sessionFilterOption
		target, agent := str(ss, "deviceId"), str(ss, "providerId")
		switch dimension {
		case "device":
			option = sessionFilterOption{Key: target, Title: s.environmentName(target)}
		case "agent":
			option = sessionFilterOption{Key: agent, Title: s.providerName(target, agent)}
		case "project":
			dir := projectDirectory(ss)
			option = sessionFilterOption{Key: sessionProjectKey(ss), Title: dir.Title, Detail: s.environmentName(target) + " · " + dir.Path}
			if str(ss, "projectKey") != "" {
				if projectDevices[option.Key] == nil {
					projectDevices[option.Key] = map[string]string{}
				}
				projectDevices[option.Key][target] = s.environmentName(target)
			}
		}
		if _, exists := options[option.Key]; !exists {
			options[option.Key] = option
		}
	}
	result := make([]sessionFilterOption, 0, len(options))
	for _, option := range options {
		if devices := projectDevices[option.Key]; len(devices) > 1 {
			names := make([]string, 0, len(devices))
			for _, name := range devices {
				names = append(names, name)
			}
			sort.Strings(names)
			option.Detail = strings.Join(names, " · ")
		}
		result = append(result, option)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Title != result[j].Title {
			return result[i].Title < result[j].Title
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func sessionFilterID(dimension, key string) string {
	return "filter-" + dimension + "-" + base64.RawURLEncoding.EncodeToString([]byte(key))
}

func (s *State) sessionFilterFeedback(cx *core.Scope, section string) core.Node {
	f := s.sectionFilters(section)
	count := s.sessionFilterCount(cx, section)
	return core.Show{When: func() bool { s.read(); return f.count() > 0 }, Then: func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Padding: core.Symmetric(2, 14), Gap: 4}, Children: []core.Node{
			row(dynamicText(cx, func() string { return locales.For(cx).FilterMatches(fmt.Sprint(count.Get())) }, 12, true), spacer(), labelledIconButton(cx, "clear-"+section+"-filters", locales.For(cx).ClearFilters(), lucide.X, func() { s.clearSessionFilters(section) })),
			core.Show{When: func() bool { return count.Get() == 0 }, Then: func(cx *core.Scope) core.Node {
				return core.View{TestID: section + "-filter-empty", Children: []core.Node{label(cx, locales.For(cx).NoFilterMatches(), 12, true)}}
			}},
		}}
	}}
}

// Streaming transcript revisions do not need to rescan the entire directory.
func (s *State) sessionFilterCount(cx *core.Scope, section string) *core.Memo[int] {
	stamp := core.NewMemo(cx, func() uint64 { s.read(); return s.catalogueVersion })
	return core.NewMemo(cx, func() int {
		stamp.Get()
		return core.Untracked(cx, func() int {
			n := 0
			for _, ss := range s.sessionsMatching(*s.sectionFilters(section)) {
				if (s.pinned[sessionIdentity(ss)] != 0) == (section == "pinned") {
					n++
				}
			}
			return n
		})
	})
}
