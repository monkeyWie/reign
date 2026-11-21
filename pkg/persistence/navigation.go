package persistence

import (
	"context"
	"unicode/utf8"
)

type Landmark struct {
	ID, Preview string
	Position    PageCursor
}
type NavigationDAO interface {
	Navigation(context.Context, Session) ([]Landmark, error)
}

// Keep previews small even when a user pastes a large source file.
func Snippet(text string) string {
	for i := 0; i < len(text); {
		if i >= 512 {
			return text[:i]
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return text
}
func Samples(total, limit int) []int {
	n := min(total, limit)
	out := make([]int, n)
	if n > 1 {
		for i := range n {
			out[i] = i * (total - 1) / (n - 1)
		}
	}
	return out
}
