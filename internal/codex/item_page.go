package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"strings"

	"github.com/monkeyWie/reignhub/internal/provider"
)

const itemCursorPrefix = "codex-items:v1:"

var errHistoryBoundary = errors.New("历史消息位置已变化，请刷新会话")

type itemCursor struct {
	Session, Source, Turn, Item string
	IPC                         bool
	Index                       int // inclusive last record still owed; -1 denotes an empty turn
}
type itemBoundaryKey struct{}

func historyBoundary(ctx context.Context) *itemCursor {
	v, _ := ctx.Value(itemBoundaryKey{}).(*itemCursor)
	return v
}
func pageContext(ctx context.Context, session, cursor string) (context.Context, string, error) {
	if !strings.HasPrefix(cursor, itemCursorPrefix) {
		return ctx, cursor, nil
	}
	var c itemCursor
	if len(cursor) > 32768 {
		return ctx, "", errHistoryBoundary
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, itemCursorPrefix))
	if err != nil || json.Unmarshal(data, &c) != nil || c.Session != session || c.Turn == "" || c.Index < -1 || strings.HasPrefix(c.Source, itemCursorPrefix) {
		return ctx, "", errHistoryBoundary
	}
	if provider.HistoryItems(ctx) == 0 {
		ctx = provider.WithHistoryItems(ctx, 50)
	}
	return context.WithValue(ctx, itemBoundaryKey{}, &c), c.Source, nil
}

// Find the last still-owed record by identity even if new live records were
// appended or earlier records shifted while the user was reading.
func throughBoundary(turns []map[string]any, c *itemCursor) ([]map[string]any, bool, error) {
	if c == nil {
		return turns, true, nil
	}
	for i, t := range turns {
		if stringValue(t["id"]) != c.Turn {
			continue
		}
		items := array(t["items"])
		at := c.Index
		if at >= 0 && (at >= len(items) || stringValue(object(items[at])["id"]) != c.Item) {
			at = -1
			for j, item := range items {
				if c.Item != "" && stringValue(object(item)["id"]) == c.Item {
					at = j
					break
				}
			}
			if at < 0 {
				return nil, false, errHistoryBoundary
			}
		}
		result := append([]map[string]any(nil), turns[:i+1]...)
		result[i] = maps.Clone(t)
		result[i]["items"] = items[:at+1]
		return result, true, nil
	}
	return nil, false, nil
}

// Account for empty turns as one record so the frontier always advances.
func itemWindow(turns []map[string]any, limit int, session, source, next string, ipc bool) ([]map[string]any, string, string) {
	if limit <= 0 {
		return turns, next, ""
	}
	left := limit
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		items := array(t["items"])
		count := max(1, len(items))
		if count <= left {
			left -= count
			continue
		}
		keep := turns[i+1:]
		at := len(items) - 1
		partial := ""
		if left > 0 {
			first := len(items) - left
			head := maps.Clone(t)
			head["items"] = items[first:]
			keep = append([]map[string]any{head}, keep...)
			at = first - 1
			partial = stringValue(t["id"])
		}
		c := itemCursor{Session: session, Source: source, IPC: ipc, Turn: stringValue(t["id"]), Index: at}
		if at >= 0 {
			c.Item = stringValue(object(items[at])["id"])
		}
		raw, _ := json.Marshal(c)
		return keep, itemCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), partial
	}
	return turns, next, ""
}
