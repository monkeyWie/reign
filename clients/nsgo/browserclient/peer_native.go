//go:build !js

package browserclient

import "errors"

func newPeer(*Client, []string) (peer, error) {
	return nil, errors.New("browser WebRTC adapter is only used on js/wasm")
}
