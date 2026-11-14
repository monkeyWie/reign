package remote

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/relay"
)

// RunHost reconnects the authenticated signal lease. Losing that lease closes
// existing peers; the optional data relay is repaired independently.
func RunHost(ctx context.Context, p Pairing, opts Options, d *control.Dispatcher) {
	RunHostObserved(ctx, p, opts, d, nil)
}
func RunHostObserved(ctx context.Context, p Pairing, opts Options, d *control.Dispatcher, status *HostStatus) {
	defer status.set("stopped", "后台连接已停止")
	for ctx.Err() == nil {
		status.set("connecting", "正在连接中继服务器")
		if err := serveHostObserved(ctx, p, opts, d, status); err != nil && ctx.Err() == nil {
			log.Printf("relay host: %v", err)
			status.set("error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
func serveHost(ctx context.Context, p Pairing, opts Options, d *control.Dispatcher) error {
	return serveHostObserved(ctx, p, opts, d, nil)
}
func serveHostObserved(ctx context.Context, p Pairing, opts Options, d *control.Dispatcher, status *HostStatus) error {
	ws, dataWS, hostPeer, err := dialWithRelayPolicy(ctx, p, "host", connectivity.StageTimeout, opts.RelayOnly)
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	if dataWS != nil {
		defer dataWS.CloseNow()
	}
	if dataWS != nil {
		status.set("connected", "信令与备用中继已连接，等待控制端配对")
	} else {
		status.set("connected", "信令已连接，可尝试 P2P 直连；暂无可用中继")
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	go keepAlive(life, ws)
	var replyRelay *hostRelayLink
	if dataWS != nil {
		replyRelay = &hostRelayLink{}
		replyRelay.socket.Store(dataWS)
		defer replyRelay.close()
		go replyRelay.run(life, p, hostPeer, status)
	}
	peers := map[string]*wire{}
	defer func() {
		for _, w := range peers {
			w.close()
		}
	}()
	for {
		_, raw, err := ws.Read(life)
		if err != nil {
			return err
		}
		var f relay.Frame
		if json.Unmarshal(raw, &f) != nil || f.Peer == "" {
			continue
		}
		if f.Kind == "bye" {
			if w := peers[f.Peer]; w != nil {
				w.close()
				delete(peers, f.Peer)
				status.track(f.Peer, nil)
			}
			continue
		}
		w := peers[f.Peer]
		if w == nil {
			if len(peers) >= 16 {
				continue
			}
			key := p.Key
			deviceID := f.Device
			if opts.ResolveKey != nil {
				var ok bool
				key, ok = opts.ResolveKey(deviceID)
				if !ok {
					continue
				}
			}
			var peer *wire
			limiter := make(chan struct{}, 8)
			var watchMu sync.Mutex
			var cancelWatch context.CancelFunc
			peer, err = newWire(life, ws, f.Peer, key, true, opts, func(data []byte) {
				if opts.ResolveKey != nil {
					if _, ok := opts.ResolveKey(deviceID); !ok {
						return
					}
				}
				var req control.Request
				if len(data) > 1<<20 || json.Unmarshal(data, &req) != nil || req.ID == "" {
					return
				}
				if err := peer.authorize(p.Host, deviceID, req); err != nil {
					encoded, _ := json.Marshal(control.Response{ID: req.ID, Error: err.Error()})
					go peer.send(encoded)
					return
				}
				// Agent work can outlive a fixed RPC budget. Cancel with the peer
				// instead; connection liveness is checked independently.
				callCtx, stop := context.WithCancel(peer.ctx)
				watch := req.Method == "watch"
				if watch {
					// One visible conversation per controller. Replacing its read
					// cancels the old one without occupying command/interrupt slots.
					watchMu.Lock()
					if cancelWatch != nil {
						cancelWatch()
					}
					cancelWatch = stop
					watchMu.Unlock()
				} else {
					select {
					case limiter <- struct{}{}:
					default:
						stop()
						return
					}
				}
				go func() {
					if !watch {
						defer func() { <-limiter }()
					}
					defer stop()
					originalID := req.ID
					if deviceID != "" {
						req.ID = deviceID + "/" + req.ID
					}
					result := d.HandleRemote(callCtx, req)
					result.ID = originalID
					if opts.ResolveKey != nil {
						if _, ok := opts.ResolveKey(deviceID); !ok {
							return
						}
					}
					if peer.authorize(p.Host, deviceID, req) != nil {
						result = control.Response{ID: originalID, Error: "远控授权已过期，请重新连接"}
					}
					encoded, _ := json.Marshal(result)
					if len(encoded) > 16<<20 {
						encoded, _ = json.Marshal(control.Response{ID: originalID, Error: "响应超过 16 MiB，请缩小查询范围"})
					}
					_ = peer.send(encoded)
				}()
			})
			if err != nil {
				continue
			}
			peer.device = deviceID
			peer.dataWS = dataWS
			peer.hostRelay = replyRelay
			peer.grant = f.Grant
			w = peer
			peers[f.Peer] = w
			status.track(f.Peer, w)
		}
		if f.Grant != "" {
			w.grantMu.Lock()
			w.grant = f.Grant
			w.grantMu.Unlock()
		}
		w.receive(f)
	}
}
