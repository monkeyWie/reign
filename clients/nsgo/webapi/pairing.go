package webapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/clients/nsgo/browserclient"
)

type Object = map[string]any

func obj(v any) Object {
	m, _ := v.(map[string]any)
	if m == nil {
		return Object{}
	}
	return m
}
func arr(v any) []Object {
	out := []Object{}
	if a, ok := v.([]Object); ok {
		return a
	}
	a, _ := v.([]any)
	for _, x := range a {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func str(m Object, k string) string { s, _ := m[k].(string); return s }
func num(m Object, k string) int64 {
	switch n := m[k].(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
func clone(v Object) Object {
	out := Object{}
	for k, x := range v {
		out[k] = x
	}
	return out
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func post(ctx context.Context, service, token, path string, v Object) (Object, error) {
	u, e := url.Parse(service)
	if e != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && !(u.Scheme == "http" && browserclient.CleartextAllowed(u.Hostname())) {
		return nil, errors.New("服务地址必须使用 HTTP 或 HTTPS")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", service+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	b, e = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out Object
	e = json.Unmarshal(b, &out)
	return out, e
}
func pairingCipher(identity Object, public, context string) (cipher.AEAD, error) {
	priv, e := hex.DecodeString(str(identity, "private"))
	if e != nil {
		return nil, e
	}
	key, e := ecdh.X25519().NewPrivateKey(priv)
	if e != nil {
		return nil, e
	}
	pub, e := hex.DecodeString(public)
	if e != nil {
		return nil, e
	}
	remote, e := ecdh.X25519().NewPublicKey(pub)
	if e != nil {
		return nil, e
	}
	shared, e := key.ECDH(remote)
	if e != nil {
		return nil, e
	}
	extract := hmac.New(sha256.New, []byte("reign/pairing"))
	extract.Write(shared)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte(context))
	expand.Write([]byte{1})
	block, e := aes.NewCipher(expand.Sum(nil))
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func seal(identity Object, public, context string, v Object) (string, error) {
	a, e := pairingCipher(identity, public, context)
	if e != nil {
		return "", e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(a.Seal(nonce, nonce, b, []byte(context))), nil
}
func openEnvelope(identity Object, public, context, packet string) (Object, error) {
	a, e := pairingCipher(identity, public, context)
	if e != nil {
		return nil, e
	}
	b, e := base64.StdEncoding.DecodeString(packet)
	if e != nil || len(b) < a.NonceSize()+a.Overhead() {
		return nil, errors.New("无效授权包")
	}
	plain, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(context))
	if e != nil {
		return nil, e
	}
	var out Object
	e = json.Unmarshal(plain, &out)
	return out, e
}
