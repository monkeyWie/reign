package persistence

import (
	"encoding/json"
	"maps"
	"reflect"
	"strconv"
)

type OverlayRow struct {
	Table, ID, Ref string
	Position       int
	Value          Object
}

const Outgoing = "outgoing_messages"
const Matches = "outgoing_matches"
const Queued = "queued_messages"

var OverlayTables = []string{Outgoing, Matches, Queued}

type overlayKey struct{ table, id, ref string }

func overlayRows(history Object) map[overlayKey]OverlayRow {
	out := map[overlayKey]OverlayRow{}
	add := func(table, id, ref string, pos int, v Object) {
		out[overlayKey{table, id, ref}] = OverlayRow{table, id, ref, pos, v}
	}
	// The UI uses immutable copies of its delivery structs. Only pending sends
	// use this conversion. The transcript and its turns never enter it.
	local := Objects(history["localMessages"])
	if local == nil && history["localMessages"] != nil {
		raw, _ := json.Marshal(history["localMessages"])
		_ = json.Unmarshal(raw, &local)
	}
	for i, row := range local {
		id := rowID(row, i)
		value := maps.Clone(row)
		delete(value, "known")
		delete(value, "anchors")
		add(Outgoing, id, "", i, value)
		known, _ := row["known"].(map[string]any)
		for ref, yes := range known {
			if yes == true {
				add(Matches, id, "known:"+ref, 0, Object{"kind": "known", "id": ref})
			}
		}
		a, _ := row["anchors"].([]any)
		for j, v := range a {
			ref, _ := v.(string)
			add(Matches, id, "anchor:"+strconv.Itoa(j), j, Object{"kind": "anchor", "id": ref})
		}
	}
	q, _ := history["queue"].(Object)
	for i, v := range Objects(q["messages"]) {
		add(Queued, rowID(v, i), "", i, v)
	}
	return out
}
func diffOverlay(p *HistoryPatch, old, next Object) {
	prev, rows := overlayRows(old), overlayRows(next)
	for k, row := range rows {
		prior, exists := prev[k]
		if !exists || prior.Position != row.Position || !reflect.DeepEqual(prior.Value, row.Value) {
			p.Overlay = append(p.Overlay, row)
		}
		delete(prev, k)
	}
	for _, r := range prev {
		r.Value = nil
		p.Overlay = append(p.Overlay, r)
	}
}

// Restore receives rows in ordinal order.
func RestoreOverlay(head Object, rows []OverlayRow) {
	var pending, queued []Object
	byID := map[string]Object{}
	for _, r := range rows {
		switch r.Table {
		case Outgoing:
			byID[r.ID] = r.Value
			pending = append(pending, r.Value)
		case Queued:
			queued = append(queued, r.Value)
		}
	}
	for _, r := range rows {
		if r.Table != Matches {
			continue
		}
		v := byID[r.ID]
		if v == nil {
			continue
		}
		ref, _ := r.Value["id"].(string)
		if r.Value["kind"] == "known" {
			known, _ := v["known"].(Object)
			if known == nil {
				known = Object{}
				v["known"] = known
			}
			known[ref] = true
		} else {
			a, _ := v["anchors"].([]any)
			v["anchors"] = append(a, ref)
		}
	}
	if len(pending) > 0 {
		head["localMessages"] = pending
	}
	if q, ok := head["queue"].(Object); ok {
		q["messages"] = queued
	}
}

// DeliveryChanges selects the sender's outgoing edits that still apply to the
// committed delivery record. A concurrent confirmation may already have removed it.
func (p HistoryPatch) DeliveryChanges(base, committed Object) []OverlayRow {
	var before, current map[overlayKey]OverlayRow
	if !same(base, committed) {
		before = overlayRows(Object{"localMessages": base["localMessages"]})
		current = overlayRows(Object{"localMessages": committed["localMessages"]})
	}
	var changes []OverlayRow
	for _, row := range p.Overlay {
		if row.Table == Queued {
			continue
		}
		if current != nil {
			key := overlayKey{table: Outgoing, id: row.ID}
			if !reflect.DeepEqual(before[key].Value, current[key].Value) {
				continue
			}
		}
		changes = append(changes, row)
	}
	return changes
}
