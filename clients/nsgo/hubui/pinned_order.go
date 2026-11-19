package hubui

import (
	"math"
	"sort"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type pinnedDrag struct {
	source, over string
	after        bool
	origin       core.Points
}

func (s *State) pinnedOrder() []string {
	keys := make([]string, 0, len(s.pinned))
	for k, v := range s.pinned {
		if v != 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.pinned[keys[i]], s.pinned[keys[j]]
		if a != b {
			return a > b
		}
		return keys[i] < keys[j]
	})
	return keys
}
func (s *State) reorderPinned(source, target string, after bool) {
	if source == target || s.pinned[source] == 0 || s.pinned[target] == 0 {
		return
	}
	keys := s.pinnedOrder()
	next := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == source {
			continue
		}
		if key == target && !after {
			next = append(next, source)
		}
		next = append(next, key)
		if key == target && after {
			next = append(next, source)
		}
	}
	// Ranks also include currently unavailable devices, preserving their places.
	for i, key := range next {
		s.pinned[key] = int64(len(next) - i)
	}
	s.saveSidebarPreferences()
}
func (s *State) movePinned(ss Object, delta int) {
	keys := s.pinnedOrder()
	key := sessionIdentity(ss)
	for i, k := range keys {
		if k == key && i+delta >= 0 && i+delta < len(keys) {
			s.reorderPinned(key, keys[i+delta], delta > 0)
			return
		}
	}
}
func (s *State) pinOrderMenu(cx *core.Scope, item func() directoryRow) core.Node {
	return core.Show{When: func() bool { s.read(); return item().Section == "pinned" }, Then: func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			ui.MenuItem{TestID: "pin-move-up", Text: "上移", Icon: lucide.ArrowUp, Disabled: func() bool {
				s.read()
				keys := s.pinnedOrder()
				return len(keys) < 2 || keys[0] == sessionIdentity(item().Session)
			}, OnSelect: func() { s.movePinned(item().Session, -1) }},
			ui.MenuItem{TestID: "pin-move-down", Text: "下移", Icon: lucide.ArrowDown, Disabled: func() bool {
				s.read()
				keys := s.pinnedOrder()
				return len(keys) < 2 || keys[len(keys)-1] == sessionIdentity(item().Session)
			}, OnSelect: func() { s.movePinned(item().Session, 1) }},
		}}
	}}
}
func (s *State) pinnedPan(cx *core.Scope, ss Object, ref *core.Ref) func(core.PanEvent) {
	if !cx.Env().Desktop() {
		return nil
	}
	key := sessionIdentity(ss)
	if s.pinnedRefs == nil {
		s.pinnedRefs = map[string]*core.Ref{}
	}
	s.pinnedRefs[key] = ref
	cx.Cleanup(func() {
		if s.pinnedRefs[key] == ref {
			delete(s.pinnedRefs, key)
		}
		if s.pinDrag != nil && s.pinDrag.source == key {
			s.pinDrag = nil
		}
	})
	return func(e core.PanEvent) {
		if e.Phase == core.GestureBegan {
			bounds, ok := ref.Bounds()
			if !ok {
				return
			}
			s.pinDrag = &pinnedDrag{source: key, origin: bounds.Origin.Y + bounds.Size.Height/2}
		}
		d := s.pinDrag
		if d == nil || d.source != key {
			return
		}
		if e.Phase == core.GestureCancelled {
			s.pinDrag = nil
			s.changed()
			return
		}
		y := d.origin + e.Translation.Y
		distance := math.Inf(1)
		d.over = ""
		for k, r := range s.pinnedRefs {
			bounds, ok := r.Bounds()
			if !ok || s.pinned[k] == 0 {
				continue
			}
			center := bounds.Origin.Y + bounds.Size.Height/2
			if gap := math.Abs(float64(y - center)); gap < distance {
				distance = gap
				d.over = k
				d.after = y > center
			}
		}
		if e.Phase == core.GestureEnded {
			s.pinDrag = nil
			s.reorderPinned(d.source, d.over, d.after)
		}
		s.changed()
	}
}
func (s *State) pinnedDropMarker(cx *core.Scope, key string) core.Node {
	return core.View{PointerEvents: core.PointerEventsNone, Style: core.Style{Position: core.Absolute, Left: core.Pt(7), Right: core.Pt(7), Height: core.Pt(2)}, StyleFunc: func() core.Style {
		s.read()
		d := s.pinDrag
		if d == nil || d.over != key || d.source == key {
			return core.Style{Display: core.DisplayNone}
		}
		st := core.Style{Background: palette(cx).foreground, CornerRadius: 1}
		if d.after {
			st.Bottom = core.Pt(0)
		} else {
			st.Top = core.Pt(0)
		}
		return st
	}}
}
