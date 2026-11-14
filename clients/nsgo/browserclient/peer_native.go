//go:build !js

package browserclient

import (
	"context"
	"errors"
	"github.com/coder/websocket"
)

func pingSocket(ctx context.Context, ws *websocket.Conn) (bool, error) {
	err := ws.Ping(ctx)
	return err == nil, err
}

func newPeer(*Client, []string) (peer, error) {
	return nil, errors.New("browser WebRTC adapter is only used on js/wasm")
}
