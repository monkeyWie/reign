package hubui

import (
	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func (s *State) emptyCatalogue(cx *core.Scope) core.Node {
	count := s.sessionFilterCount(cx)
	return core.Show{When: func() bool { s.read(); return count.Get() == 0 && s.filters.count() == 0 }, Then: func(cx *core.Scope) core.Node {
		mode := core.NewMemo(cx, func() string {
			s.read()
			for _, d := range s.devices {
				if h := s.connections[str(d, "id")]; h != nil && h.issues["catalogue"] != "" {
					return "error"
				}
			}
			if s.cataloguePending() {
				return "loading"
			}
			for _, d := range s.devices {
				id := str(d, "id")
				if !s.connectionReady(id) {
					continue
				}
				for _, p := range s.workspaces[id].providers {
					if providerReady(p) && flag(object(p["capabilities"]), "create") {
						return "create"
					}
				}
			}
			if s.api.CanHost() {
				return "agents"
			}
			return "connect"
		})
		return core.View{TestID: "session-list-placeholder", Style: core.Style{Padding: core.Edges{Top: 24, Left: 20, Right: 20, Bottom: 16}, Gap: 12, AlignItems: core.AlignStart}, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				kind := mode.Get()
				l := locales.For(cx)
				if kind == "loading" {
					return row(s.loadingRing("sessions-loading", l.SessionsLoading(), 16), label(cx, l.SessionsLoading(), 12, true))
				}
				title, action := l.SidebarEmpty(), l.ManageAgents()
				if kind == "error" {
					title, action = l.SessionsUnavailable(), l.ViewDetails()
				} else if kind == "create" {
					action = l.NewSession()
				} else if kind == "connect" {
					action = l.AddDevice()
				}
				link := button(cx, "empty-sessions-action", action, lucide.ArrowRight, "ghost", func() {
					switch kind {
					case "error":
						for _, d := range s.devices {
							id := str(d, "id")
							if h := s.connections[id]; h != nil && h.issues["catalogue"] != "" {
								s.connectionDetailTarget = id
								s.dialog.Set("connection-status")
								break
							}
						}
					case "create":
						s.createDirectory = ""
						s.dialog.Set("create")
					case "agents":
						s.manageDevice(control.LocalTarget)
					case "connect":
						s.scanned = nil
						s.dialog.Set("connect")
					}
				}).(core.View)
				link.Style.Margin = core.Edges{Left: -buttonHorizontalPadding}
				// The action's glyph follows the label, like a navigation link.
				link.Children[0], link.Children[1] = link.Children[1], link.Children[0]
				return core.View{Style: core.Style{Gap: 6, AlignItems: core.AlignStart}, Children: []core.Node{
					core.View{Style: core.Style{Margin: core.Edges{Bottom: 6}}, Children: []core.Node{glyph(cx, lucide.MessagesSquare, 24)}},
					label(cx, title, 13, true), link,
				}}
			}),
		}}
	}}
}
