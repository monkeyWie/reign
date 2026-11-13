// Package hubui is the replacement UI. The existing providers, embedded host,
// SQLite stores and transport services remain owned by the root Go module.
package hubui

import (
	"context"
	"encoding/json"
	"fmt"
)

type Object = map[string]any
type API interface {
	Call(context.Context, string, Object, string, Object) (any, error)
	CanHost() bool
	Close()
}

var DefaultService = "https://ras.lihuipeng.love"

func object(v any) Object {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return Object{}
}
func objects(v any) []Object {
	if a, ok := v.([]Object); ok {
		return a
	}
	a, _ := v.([]any)
	out := make([]Object, 0, len(a))
	for _, v := range a {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func str(m Object, k string) string { v, _ := m[k].(string); return v }
func flag(m Object, k string) bool  { v, _ := m[k].(bool); return v }
func number(m Object, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}
func copyObject(m Object) Object {
	out := Object{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func text(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func rpc(ctx context.Context, api API, target, provider, session, method string, extra Object) (any, error) {
	r := Object{"method": method, "provider": provider, "session": session}
	for k, v := range extra {
		r[k] = v
	}
	return api.Call(ctx, "client.call", nil, target, r)
}
