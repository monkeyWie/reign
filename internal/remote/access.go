package remote

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/pkg/access"
	"time"
)

func (w *wire) authorize(host, device string, req control.Request) error {
	if w.opts.GrantPublicKey == "" {
		if w.opts.GrantRequired || w.opts.ServiceID != "" {
			return errors.New("服务未提供有效的授权公钥")
		}
		return nil
	}
	key, err := base64.RawURLEncoding.DecodeString(w.opts.GrantPublicKey)
	if err != nil {
		return err
	}
	w.grantMu.RLock()
	ticket := w.grant
	w.grantMu.RUnlock()
	g, err := access.Verify(ed25519.PublicKey(key), ticket, time.Now())
	if err != nil || g.Role != "client" || !g.Allows(w.opts.ServiceID, host, device, "connect") {
		return errors.New("远控授权已过期，请重新连接或登录")
	}
	if req.Provider != "" && !g.AllowsProduct(req.Provider) {
		return errors.New("当前套餐未启用此 Agent，请在账号设备页调整 Agent 权益")
	}
	return nil
}
