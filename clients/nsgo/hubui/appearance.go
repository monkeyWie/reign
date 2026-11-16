package hubui

import "github.com/nsgo-dev/nsgo/core"

type colorScheme struct {
	id, name                                                                                string
	canvas, sidebar, page, elevated, muted, selected, border, text, secondary, accent, code uint32
}

var lightSchemes = []colorScheme{
	{"ocean", "Ocean", 0xdbe7f2, 0xdbe7f2, 0xf6f9fc, 0xffffff, 0xebeff4, 0xc6d8e8, 0xd7e0e9, 0x202d38, 0x687b8c, 0x3b719b, 0xe6f2f6},
	{"lagoon", "Lagoon", 0xdcebed, 0xdcebed, 0xf7fbfb, 0xffffff, 0xeaf1f1, 0xc7dfe3, 0xd4e3e5, 0x223439, 0x637d83, 0x347f90, 0xe4f2f2},
	{"lilac", "Lilac", 0xe6e1f1, 0xe6e1f1, 0xfaf8fc, 0xffffff, 0xefedf5, 0xd8cfeb, 0xe1daeb, 0x302a40, 0x7c718e, 0x7760ad, 0xefeaf8},
	{"mist", "Mist", 0xe7e9ed, 0xe7e9ed, 0xf8f9fb, 0xffffff, 0xedf0f3, 0xd6dce4, 0xdde2e8, 0x27313d, 0x747e8c, 0x526c8e, 0xebeff6},
}
var darkSchemes = []colorScheme{
	{"aurora", "Aurora", 0x111e23, 0x111e23, 0x18272d, 0x203239, 0x25363d, 0x294750, 0x30444c, 0xe5eff0, 0x9bb2b9, 0x81c6c1, 0x1c343a},
	{"graphite", "Graphite", 0x171b22, 0x171b22, 0x202630, 0x282f3a, 0x2d3541, 0x344356, 0x39424f, 0xe8edf5, 0xa2afc0, 0x8cadde, 0x253342},
	{"nebula", "Nebula", 0x1d182a, 0x1d182a, 0x282135, 0x312a40, 0x3a314b, 0x493c61, 0x483c58, 0xf0e9f9, 0xb4a5c7, 0xb69be8, 0x30263f},
	{"ink", "Ink", 0x101c2a, 0x101c2a, 0x182839, 0x203448, 0x263b50, 0x2c4c66, 0x30485f, 0xe5eff9, 0x9bb3ca, 0x7bbde7, 0x1c344a},
}

func findScheme(id string, choices []colorScheme) colorScheme {
	for _, c := range choices {
		if c.id == id {
			return c
		}
	}
	return choices[0]
}
func schemePalette(c colorScheme, p core.Palette) core.Palette {
	p.Background, p.Surface = core.RGB(c.page), core.RGB(c.muted)
	p.SurfaceHover, p.SurfacePressed = core.RGB(c.selected).Faded(.7), core.RGB(c.selected)
	p.Text, p.TextMuted, p.Border = core.RGB(c.text), core.RGB(c.secondary), core.RGB(c.border)
	p.Accent, p.AccentHover, p.AccentPressed = core.RGB(c.accent), core.RGB(c.accent).Faded(.85), core.RGB(c.accent)
	p.AccentText = core.RGB(c.page)
	p.Focus, p.Selection, p.Scrollbar = p.Accent, p.Accent.Faded(.22), core.RGB(c.secondary).Faded(.35)
	// Keep NSGo component roles alongside the workspace surface roles.
	roles := map[string]core.Color{
		"canvas": core.RGB(c.canvas), "sidebar": core.RGB(c.sidebar), "elevated": core.RGB(c.elevated), "selected": core.RGB(c.selected), "codeSurface": core.RGB(c.code),
		"field": core.RGB(c.elevated), "fieldBorder": p.Border, "inset": core.RGB(c.code), "raised": core.RGB(c.elevated), "line": p.Border, "faint": p.TextMuted,
		"code": p.Text, "accentFill": p.Accent, "light": p.Text, "onLight": p.Background, "track": p.SurfacePressed, "knob": p.Text, "info": p.Accent,
	}
	for name, value := range roles {
		p.Colors[name] = value
	}

	return p
}
func (s *State) applyColors() {
	t := core.DefaultTheme()
	t.Light = schemePalette(findScheme(s.lightScheme, lightSchemes), t.Light)
	t.Dark = schemePalette(findScheme(s.darkScheme, darkSchemes), t.Dark)
	t.Radius = 10
	s.cx.SetTheme(t)
}
func (s *State) loadAppearance() {
	s.appearance, s.lightScheme, s.darkScheme = "system", lightSchemes[0].id, darkSchemes[0].id
	if v, ok := s.cx.Storage().GetItem("appearance"); ok {
		switch v {
		case "true", "dark":
			s.appearance = "dark"
		case "false", "light":
			s.appearance = "light"
		}
	}
	if v, ok := s.cx.Storage().GetItem("light-colors"); ok {
		s.lightScheme = findScheme(v, lightSchemes).id
	}
	if v, ok := s.cx.Storage().GetItem("dark-colors"); ok {
		s.darkScheme = findScheme(v, darkSchemes).id
	}
	s.applyColors()
	s.setAppearance(s.appearance)
}
func (s *State) setAppearance(value string) {
	s.appearance = value
	switch value {
	case "dark":
		s.cx.SetAppearance(core.AppearanceDark)
	case "light":
		s.cx.SetAppearance(core.AppearanceLight)
	default:
		s.appearance = "system"
		s.cx.SetAppearance(core.AppearanceSystem)
	}
	s.dark = s.cx.Dark()
	_ = s.cx.Storage().SetItem("appearance", s.appearance)
	s.changed()
}
func (s *State) selectColors(dark bool, id string) {
	if dark {
		s.darkScheme = id
		_ = s.cx.Storage().SetItem("dark-colors", id)
	} else {
		s.lightScheme = id
		_ = s.cx.Storage().SetItem("light-colors", id)
	}
	s.applyColors()
	s.changed()
}
