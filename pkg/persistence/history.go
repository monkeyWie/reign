// Package persistence defines the controller's local history contract.
package persistence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
)

type Object = map[string]any
type Session struct{ Env, Provider, ID string }
type Row struct {
	Turn, Item string
	Position   int
	Value      Object
}
type HistoryPatch struct {
	Base, Revision   string
	Replace          bool
	PreserveOutgoing bool
	Head             Object
	Turns, Messages  []Row
	Overlay          []OverlayRow
}

var ErrConflict = errors.New("local history changed")

type HistoryDAO interface {
	ApplyHistory(context.Context, Session, HistoryPatch) error
	ReadPage(context.Context, Session, PageQuery) (HistoryPage, error)
	ForgetHistory(context.Context, string) error
}

func Revision() string { var b [16]byte; rand.Read(b[:]); return hex.EncodeToString(b[:]) }

// An older-page append advances the CAS revision but leaves the live tail
// unchanged. Multiple appends retain the same tail revision.
func OlderPageRevision(base string) string {
	tail, _, _ := strings.Cut(base, "/")
	return tail + "/" + Revision()
}

func OnlyOlderPagesChanged(base, latest string) bool {
	tail, _, page := strings.Cut(latest, "/")
	before, _, _ := strings.Cut(base, "/")
	return page && tail == before
}

func (s Session) Valid() bool { return s.Env != "" && s.Provider != "" && s.ID != "" }
func Objects(v any) []Object {
	switch a := v.(type) {
	case []Object:
		return a
	case []any:
		rows := make([]Object, 0, len(a))
		for _, v := range a {
			if row, ok := v.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
		return rows
	}
	return nil
}
func without(o Object, field string) Object { m := maps.Clone(o); delete(m, field); return m }
func rowID(o Object, pos int) string {
	if id, _ := o["id"].(string); id != "" {
		return "id:" + id
	}
	return "at:" + strconv.Itoa(pos)
}
func same(a, b Object) bool { return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer() }

type position struct {
	index int
	value Object
}

// Compare immutable projections on the writer, before any JSON or bridge. The
// unchanged prefix of turns/items requires neither allocations nor encoding.
func changes(old, next []Object, oldStart, start int, change func(string, int, int, Object, Object)) {
	i := 0
	for oldStart == start && i < len(old) && i < len(next) && same(old[i], next[i]) {
		i++
	}
	if i == len(old) && i == len(next) {
		return
	}
	remaining := make(map[string]position, len(old)-i)
	for n := i; n < len(old); n++ {
		remaining[rowID(old[n], n+oldStart)] = position{n, old[n]}
	}
	for n := i; n < len(next); n++ {
		id := rowID(next[n], n+start)
		prev, ok := remaining[id]
		delete(remaining, id)
		if !ok || prev.index+oldStart != n+start || !same(prev.value, next[n]) {
			change(id, n, prev.index, prev.value, next[n])
		}
	}
	for id, prev := range remaining {
		change(id, prev.index, prev.index, prev.value, nil)
	}
}
func Diff(base string, old, next Object) HistoryPatch {
	p := HistoryPatch{Base: base, Revision: Revision(), Replace: old == nil, Head: HistoryHead(next)}
	origin := Origin(next)
	oldOrigin := Origin(old)
	changes(Objects(old["turns"]), Objects(next["turns"]), oldOrigin.Turn, origin.Turn, func(id string, pos, oldPos int, before, after Object) {
		if after == nil && next["_localOlder"] == true && oldOrigin.Turn+oldPos < origin.Turn {
			return
		}
		var meta Object
		if after != nil {
			meta = without(after, "items")
		}
		if before == nil || after == nil || pos+origin.Turn != oldPos+oldOrigin.Turn || !reflect.DeepEqual(without(before, "items"), meta) {
			p.Turns = append(p.Turns, Row{Turn: id, Position: pos + origin.Turn, Value: meta})
		}
		if after == nil {
			return
		}
		start, oldStart := 0, 0
		if pos == 0 {
			start = origin.Item
		}
		if oldPos == 0 {
			oldStart = oldOrigin.Item
		}
		changes(Objects(before["items"]), Objects(after["items"]), oldStart, start, func(item string, index, _ int, _, v Object) {
			if v == nil && next["_localOlder"] == true && index+oldStart < start {
				return
			}

			p.Messages = append(p.Messages, Row{Turn: id, Item: item, Position: index + start, Value: v})
		})
	})
	diffOverlay(&p, old, next)
	return p
}

// HistoryHead omits reconstructed rows and controller-only window metadata.
func HistoryHead(t Object) Object {
	head := make(Object, len(t))
	for key, value := range t {
		switch key {
		case "turns", "localMessages", "_turnStart", "_itemStart", "_localOlder", "_historyRevision", "_refreshNavigation", "_baseline", "_refreshHistory", "_deliveryOnly":
			continue
		case "queue":
			if q, ok := value.(Object); ok {
				value = without(q, "messages")
			}
		}
		head[key] = value
	}
	return head
}

func (p HistoryPatch) Validate(s Session) error {
	if !s.Valid() || p.Head == nil || p.Revision == "" {
		return fmt.Errorf("invalid history checkpoint")
	}
	if _, ok := p.Head["turns"]; ok {
		return fmt.Errorf("history head contains turns")
	}
	return nil
}

// Assemble consumes rows ordered by their respective ordinals.
func Assemble(head Object, turns, items []Row) Object {
	if head == nil {
		return nil
	}
	list := make([]Object, 0, len(turns))
	index := make(map[string]Object, len(turns))
	for _, r := range turns {
		r.Value["items"] = []Object{}
		index[r.Turn] = r.Value
		list = append(list, r.Value)
	}
	for _, r := range items {
		if t := index[r.Turn]; t != nil {
			t["items"] = append(t["items"].([]Object), r.Value)
		}
	}
	head["turns"] = list
	return head
}
