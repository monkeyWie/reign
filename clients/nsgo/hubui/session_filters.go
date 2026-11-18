package hubui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/i18n"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
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

func (s *State) filteredSessions() []Object {
	result := uniqueSessions(s.sessions)
	f := s.filters
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

func (s *State) clearSessionFilters() {
	s.filters = sessionFilters{}
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

func (s *State) sessionFilterGroup(cx *core.Scope, dimension, title string, selection *map[string]bool) core.Node {
	stamp := core.NewMemo(cx, func() uint64 { s.read(); return s.catalogueVersion })
	options := core.NewMemoFunc(cx, func() []sessionFilterOption {
		stamp.Get()
		i18n.Language(cx)
		return core.Untracked(cx, func() []sessionFilterOption { return s.sessionFilterOptions(dimension) })
	}, slices.Equal[[]sessionFilterOption])
	return core.View{Style: core.Style{Gap: 3}, Children: []core.Node{
		label(cx, title, 12, true),
		ui.Checkbox{TestID: "filter-" + dimension + "-all", Text: locales.For(cx).FilterAll(),
			Checked:  func() bool { s.read(); return len(*selection) == 0 },
			OnChange: func(bool) { *selection = nil; s.filtersChanged() }, Style: core.Style{MinHeight: core.Pt(36)},
		},
		core.Each(options.Get, func(o sessionFilterOption) string { return o.Key }, func(cx *core.Scope, option func() sessionFilterOption, _ func() int) core.Node {
			key := option().Key
			return ui.Checkbox{TestID: sessionFilterID(dimension, key), Label: option().Title + " " + option().Detail,
				Style:   core.Style{MinHeight: core.Pt(38), MinWidth: core.Pt(0)},
				Checked: func() bool { s.read(); return (*selection)[key] },
				OnChange: func(checked bool) {
					if *selection == nil {
						*selection = map[string]bool{}
					}
					if checked {
						(*selection)[key] = true
					} else {
						delete(*selection, key)
					}
					s.filtersChanged()
				}, Children: []core.Node{core.View{Style: core.Style{MinWidth: core.Pt(0), FlexShrink: 1, Gap: 2}, Children: []core.Node{
					singleLine(cx, func() string { return option().Title }, 13, false),
					core.Show{When: func() bool { return option().Detail != "" }, Then: func(cx *core.Scope) core.Node {
						return singleLine(cx, func() string { return option().Detail }, 11, true)
					}},
				}}},
			}
		}),
	}}
}

func (s *State) sessionFiltersButton(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, false)
	count := s.sessionFilterCount(cx)
	return base.Popover{TestID: "session-filters", Label: locales.For(cx).SessionFilters(), Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, Placement: core.PlaceBottom, Align: core.AlignEnd, Offset: 6, Margin: 8,
		Style: core.Style{Width: core.Pt(36), Height: core.Pt(36), CornerRadius: 10, FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		StyleFunc: func() core.Style {
			s.read()
			st := pressStyle(cx)
			if open.Get() || s.filters.count() > 0 {
				st.Background = palette(cx).selected
			}
			return st
		}, Tooltip: tooltipCard(cx, label(cx, locales.For(cx).SessionFilters(), 12, false)),
		Trigger: core.View{Children: []core.Node{
			glyph(cx, lucide.ListFilter, 16),
			core.Show{When: func() bool { s.read(); return s.filters.count() > 0 }, Then: func(cx *core.Scope) core.Node {
				return core.View{Style: core.Style{Position: core.Absolute, Top: core.Pt(-4), Right: core.Pt(-5), Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).accent} }}
			}},
		}},
		PopupStyle: core.Style{Padding: core.All(14), Gap: 10, CornerRadius: 12, BorderWidth: 1},
		PopupStyleFunc: func() core.Style {
			c := palette(cx)
			return core.Style{Width: core.Pt(min(320, max(160, cx.Env().Window().Width-24))), Background: c.elevated, Color: c.foreground, BorderColor: c.border,
				Shadows: []core.BoxShadow{{Offset: core.Point{Y: 6}, Blur: 20, Color: core.RGB(0).Faded(.16)}}}
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			i18n.Language(cx)
			l := locales.For(cx)
			return core.View{TestID: "session-filter-panel", Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
				row(label(cx, l.SessionFilters(), 14, false), spacer(), labelledIconButton(cx, "close-session-filters", l.CloseFilters(), lucide.X, func() { open.Set(false) })),
				core.Scroll{TestID: "session-filter-scroll", StyleFunc: func() core.Style {
					return core.Style{MaxHeight: core.Pt(min(500, max(100, cx.Env().Window().Height-240)))}
				}, Children: []core.Node{
					core.View{Style: core.Style{Gap: 16, MinWidth: core.Pt(0)}, Children: []core.Node{
						core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
							label(cx, l.SessionGrouping(), 12, true),
							ui.Select[string]{TestID: "session-grouping", Style: core.Style{Width: core.Pct(100), MinWidth: core.Pt(0)}, Label: l.SessionGrouping(), Options: []ui.Option[string]{{Value: groupProject, Label: l.GroupByProject()}, {Value: groupDevice, Label: l.GroupByDevice()}, {Value: groupAgent, Label: l.GroupByAgent()}}, Value: func() string { s.read(); return s.grouping }, OnChange: s.setGrouping},
						}},
						label(cx, l.FilterHint(), 11, true),
						s.sessionFilterGroup(cx, "device", l.FilterDevices(), &s.filters.Devices),
						s.sessionFilterGroup(cx, "agent", l.FilterAgents(), &s.filters.Agents),
						s.sessionFilterGroup(cx, "project", l.FilterProjects(), &s.filters.Projects),
					}},
				}},
				row(dynamicText(cx, func() string { return l.FilterMatches(fmt.Sprint(count.Get())) }, 12, true), spacer(), button(cx, "reset-session-filters", l.ClearFilters(), nil, "ghost", s.clearSessionFilters)),
			}}
		})},
	}
}

func (s *State) sessionFilterFeedback(cx *core.Scope) core.Node {
	count := s.sessionFilterCount(cx)
	return core.Show{When: func() bool { s.read(); return s.filters.count() > 0 }, Then: func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Padding: core.Symmetric(2, 14), Gap: 4}, Children: []core.Node{
			row(dynamicText(cx, func() string { return locales.For(cx).FilterMatches(fmt.Sprint(count.Get())) }, 12, true), spacer(), labelledIconButton(cx, "clear-session-filters", locales.For(cx).ClearFilters(), lucide.X, s.clearSessionFilters)),
			core.Show{When: func() bool { return count.Get() == 0 }, Then: func(cx *core.Scope) core.Node {
				return core.View{TestID: "session-filter-empty", Children: []core.Node{label(cx, locales.For(cx).NoFilterMatches(), 12, true)}}
			}},
		}}
	}}
}

// Streaming transcript revisions do not need to rescan the entire directory.
func (s *State) sessionFilterCount(cx *core.Scope) *core.Memo[int] {
	stamp := core.NewMemo(cx, func() uint64 { s.read(); return s.catalogueVersion })
	return core.NewMemo(cx, func() int {
		stamp.Get()
		return core.Untracked(cx, func() int { return len(s.filteredSessions()) })
	})
}
