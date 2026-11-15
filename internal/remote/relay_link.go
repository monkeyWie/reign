package remote

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
)

// The optional reply relay is independent of the authenticated signal lease.
// Repairing it must not discard healthy direct peers or their encryption state.
type relayLink struct {
	socket atomic.Pointer[websocket.Conn]
}

func (l *relayLink) close() {
	if socket := l.socket.Swap(nil); socket != nil {
		_ = socket.CloseNow()
	}
}

func (l *relayLink) run(ctx context.Context, p Pairing, role, peer string, status *HostStatus) {
	defer l.close()
	for ctx.Err() == nil {
		if socket := l.socket.Load(); socket != nil {
			life, cancel := context.WithCancel(ctx)
			go keepSocketAlive(life, socket, false)
			drainRelay(life, socket)
			cancel()
			l.socket.CompareAndSwap(socket, nil)
			if role == "client" {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		status.set("connected", "信令已连接，备用中继正在准备；P2P 直连仍可使用")
		connectivity.Report(ctx, "relay-ready", "后台准备备用中继通道", connectivity.StageTimeout)
		connectivity.Note(ctx, "relay-endpoint", "relay", "中继服务地址", p.Relay+"/p2p/relay", "info")
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
			socket, err := openRelaySocket(setup, relayEndpoint(p, role, peer), p.RelayToken, peer)
			cancel()
			if err == nil {
				l.socket.Store(socket)
				if ctx.Err() != nil {
					return
				}
				connectivity.Note(ctx, "relay-ready", "relay", "备用中继通道已就绪", "直连优先，备用通道独立维护", "success")
				status.set("connected", "信令与备用中继已连接，直连优先")
				break
			}
			connectivity.Note(ctx, "relay-ready", "relay", "备用中继暂不可用，继续尝试直连", err.Error(), "warning")
		}
	}
}
