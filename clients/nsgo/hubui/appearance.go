package hubui

import "github.com/nsgo-dev/nsgo/core"

type colorScheme struct {
	canvas, sidebar, page, elevated, muted, selected, border, text, secondary, accent, accentText, code uint32
}

// Shadcn zinc surfaces with the same green accent as the official website.
var lightColors = colorScheme{
	canvas: 0xfafafa, sidebar: 0xfafafa, page: 0xffffff, elevated: 0xffffff,
	muted: 0xf4f4f5, selected: 0xf4f4f5, border: 0xe4e4e7,
	text: 0x09090b, secondary: 0x71717a, accent: 0x16805d, accentText: 0xffffff, code: 0xf4f4f5,
}
var darkColors = colorScheme{
	canvas: 0x09090b, sidebar: 0x18181b, page: 0x09090b, elevated: 0x18181b,
	muted: 0x27272a, selected: 0x27272a, border: 0x27272a,
	text: 0xfafafa, secondary: 0xa1a1aa, accent: 0x7fc6a6, accentText: 0x09090b, code: 0x18181b,
}

func schemePalette(c colorScheme, p core.Palette) core.Palette {
	p.Background, p.Surface = core.RGB(c.page), core.RGB(c.muted)
	p.SurfaceHover, p.SurfacePressed = core.RGB(c.selected).Faded(.7), core.RGB(c.selected)
	p.Text, p.TextMuted, p.Border = core.RGB(c.text), core.RGB(c.secondary), core.RGB(c.border)
	p.Accent, p.AccentHover, p.AccentPressed = core.RGB(c.accent), core.RGB(c.accent).Faded(.85), core.RGB(c.accent)
	p.AccentText = core.RGB(c.accentText)
	p.Focus, p.Selection, p.Scrollbar = p.Accent, p.Accent.Faded(.22), core.RGB(c.secondary).Faded(.35)
	// Keep NSGo component roles alongside the workspace surface roles.
	roles := map[string]core.Color{
		"canvas": core.RGB(c.canvas), "sidebar": core.RGB(c.sidebar), "elevated": core.RGB(c.elevated), "selected": core.RGB(c.selected), "codeSurface": core.RGB(c.code),
		"field": core.RGB(c.elevated), "fieldBorder": p.Border, "inset": core.RGB(c.code), "raised": core.RGB(c.elevated), "line": p.Border, "faint": p.TextMuted,
		"foreground": p.Text, "card": core.RGB(c.elevated), "card-foreground": p.Text,
		"popover": core.RGB(c.elevated), "popover-foreground": p.Text,
		"primary": p.Accent, "primary-foreground": p.AccentText,
		"secondary": p.Surface, "secondary-foreground": p.Text,
		"muted": p.Surface, "muted-foreground": p.TextMuted, "accent-foreground": p.Text,
		"input": p.Border, "ring": p.Focus,
		"sidebar-foreground": p.Text, "sidebar-primary": p.Accent, "sidebar-primary-foreground": p.AccentText,
		"sidebar-accent": core.RGB(c.selected), "sidebar-accent-foreground": p.Text,
		"sidebar-border": p.Border, "sidebar-ring": p.Focus,
		"code": p.Text, "accentFill": p.Accent, "light": p.Text, "onLight": p.Background, "track": p.SurfacePressed, "knob": p.Text, "info": p.Accent,
	}
	for name, value := range roles {
		p.Colors[name] = value
	}

	return p
}
func (s *State) applyColors() {
	t := core.DefaultTheme()
	t.Light = schemePalette(lightColors, t.Light)
	t.Dark = schemePalette(darkColors, t.Dark)
	t.Radius = 10
	s.cx.SetTheme(t)
}
func (s *State) loadAppearance() {
	s.appearance = "system"
	if v, ok := s.cx.Storage().GetItem("appearance"); ok {
		switch v {
		case "true", "dark":
			s.appearance = "dark"
		case "false", "light":
			s.appearance = "light"
		}
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
