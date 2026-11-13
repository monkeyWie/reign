// Package link contains transport-independent route credentials and validation.
package link

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// TunnelToken is domain-separated from the end-to-end encryption key and from
// the device directory's token. Neither secret is disclosed to Cloudflare.
func TunnelToken(key, host, client string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("reign/tunnel/access/v1/" + host + "/" + client))
	return hex.EncodeToString(mac.Sum(nil))
}

// PublicEndpoint permits HTTPS origins only. In particular a directory response
// must never redirect a browser to an arbitrary cleartext or credentialed URL.
func PublicEndpoint(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" && u.Port() != "443" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return "", errors.New("tunnel endpoint must be an HTTPS origin")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
