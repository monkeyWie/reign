package remote

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/relay"
)

// RunHost reconnects the outbound relay link. Losing relay also closes existing
// peers, providing a single, explicit lifetime and authorization boundary.
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
	dialCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	ws, dataWS, _, err := dial(dialCtx, p, "host")
	stop()
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	if dataWS != nil {
		defer dataWS.CloseNow()
	}
	status.set("connected", "中继已连接，等待移动端配对")
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	go keepAlive(life, ws)
	go drainRelay(life, dataWS)
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
				callCtx, stop := context.WithTimeout(peer.ctx, 40*time.Second)
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
