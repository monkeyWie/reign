package remote

import (
	"context"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
)

// The optional reply relay is independent of the authenticated signal lease.
// Repairing it must not discard healthy direct peers or their encryption state.
type hostRelayLink struct {
	socket atomic.Pointer[websocket.Conn]
}

func (l *hostRelayLink) close() {
	if socket := l.socket.Swap(nil); socket != nil {
		_ = socket.CloseNow()
	}
}

func (l *hostRelayLink) run(ctx context.Context, p Pairing, peer string, status *HostStatus) {
	defer l.close()
	u, _ := url.Parse(p.Relay)
	u.Path = "/p2p/relay"
	q := u.Query()
	q.Set("role", "host")
	q.Set("id", p.Host)
	q.Set("peer", peer)
	u.RawQuery = q.Encode()
	for ctx.Err() == nil {
		if socket := l.socket.Load(); socket != nil {
			life, cancel := context.WithCancel(ctx)
			go keepSocketAlive(life, socket, false)
			drainRelay(life, socket)
			cancel()
			l.socket.CompareAndSwap(socket, nil)
		}
		if ctx.Err() != nil {
			return
		}
		status.set("connected", "备用中继暂不可用，正在恢复；P2P 直连仍可使用")
		for attempt := 0; ctx.Err() == nil; attempt++ {
			if attempt > 0 {
				timer := time.NewTimer(time.Duration(min(attempt, 10)) * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			setup, cancel := context.WithTimeout(ctx, connectivity.StageTimeout)
			socket, err := openRelaySocket(setup, u.String(), p.RelayToken, peer)
			cancel()
			if err == nil {
				l.socket.Store(socket)
				status.set("connected", "信令与备用中继已连接，直连优先")
				break
			}
		}
	}
}
