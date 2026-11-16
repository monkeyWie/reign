package device

import (
	"context"
	"strings"
	"unicode"
)

// ClientInfo is display metadata, never an authorization or hardware identity.
// It travels inside the encrypted pairing and authenticated peer connection.
type ClientInfo struct {
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (i ClientInfo) Normalize() ClientInfo {
	clean := func(s string) string {
		s = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s))
		r := []rune(s)
		if len(r) > 128 {
			s = string(r[:128])
		}
		return s
	}
	i.Name, i.Model = clean(i.Name), clean(i.Model)
	switch i.Platform {
	case "darwin", "windows", "linux", "android", "ios":
	default:
		i.Platform = ""
	}
	switch i.Kind {
	case "native", "browser":
	default:
		i.Kind = ""
	}
	return i
}

type clientInfoKey struct{}

func WithClientInfo(ctx context.Context, info ClientInfo) context.Context {
	return context.WithValue(ctx, clientInfoKey{}, info.Normalize())
}
func ClientInfoFrom(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoKey{}).(ClientInfo)
	return info
}
func CurrentClientInfo(ctx context.Context, fallback ClientInfo) ClientInfo {
	if info := ClientInfoFrom(ctx); info.Name != "" {
		return info
	}
	return fallback.Normalize()
}
