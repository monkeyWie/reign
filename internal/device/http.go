package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reign/internal/connectivity"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AllowLANHTTP is retained for older build scripts. HTTP/WS are now supported
// for both IP and domain deployments; the user chooses HTTP or HTTPS explicitly.
var AllowLANHTTP = "false"

// Network falls back to public STUN if an older/unreachable service cannot
// advertise its endpoints. This must never prevent pairing or WSS fallback.
func Network(ctx context.Context, service string) connectivity.Policy {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var policy connectivity.Policy
	if err := Post(ctx, service, "", "/v2/network", map[string]any{}, &policy); err != nil {
		return connectivity.DefaultPolicy()
	}
	return policy
}

func CleartextAllowed(host string) bool {
	return host != ""
}

func ServiceURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimRight(raw, "/"))
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("无效服务地址")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && CleartextAllowed(u.Hostname())) {
		return "", fmt.Errorf("服务地址必须使用 HTTP 或 HTTPS")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("服务地址不能包含路径")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func Post(ctx context.Context, service, token, path string, in, out any) error {
	base, e := ServiceURL(service)
	if e != nil {
		return e
	}
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := (&http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1000))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}
func Register(ctx context.Context, service string, i Identity, host bool) (Identity, error) {
	var r struct{ ID, ConnectionID, Token string }
	if e := Post(ctx, service, "", "/v2/register", map[string]any{"name": i.Name, "public": i.Public, "isHost": host}, &r); e != nil {
		return i, e
	}
	i.ID = r.ID
	i.ConnectionID = r.ConnectionID
	i.Token = r.Token
	i.Service = service
	return i, nil
}
