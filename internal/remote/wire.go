package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/pion/webrtc/v4"
)

type Options struct {
	Progress       func(connectivity.Progress) `json:"-"`
	PublicSTUN     []string
	SelfSTUN       []string
	GrantRequired  bool
	GrantPublicKey string
	ServiceID      string
	RelayName      string
	TCPTrace       func(string, ...any) `json:"-"`
	ICEServers     []webrtc.ICEServer
	TCPSTUN        []string // TCP STUN host:port endpoints, discovered via /p2p/config
	DisableTCP     bool
	DisableWebRTC  bool
	ResolveKey     func(string) (string, bool) `json:"-"`
	RelayOnly      bool
}
type wire struct {
	progressMu        sync.Mutex
	progressLast      map[string]connectivity.Progress
	ws                *websocket.Conn
	dataWS            *websocket.Conn
	grantMu           sync.RWMutex
	grant             string
	crypto            *cryptor
	base              *cryptor
	key, clientNonce  string
	ready             chan struct{}
	device            string
	peer              string
	host              bool
	preferDirectUntil time.Time
	opts              Options
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	pc                *webrtc.PeerConnection
	dc                *webrtc.DataChannel
	direct            bool
	chunks            map[string]*assembly
	chunkBytes        int
	sendMu            sync.Mutex
	signalMu          sync.Mutex
	candidates        []webrtc.ICECandidateInit
	tcpMu             sync.Mutex
	tcpSelectMu       sync.Mutex
	tcpAgent          *connectivity.TCPAgent
	tcpStarted        bool
	tcpKey            [32]byte
	tcp               *tcpPath
	onData            func([]byte)
}
type chunk struct {
	ID    string `json:"id"`
	Index int    `json:"i"`
	Total int    `json:"n"`
	Data  []byte `json:"b"`
}
type assembly struct {
	parts   [][]byte
	count   int
	created time.Time
}

func dial(ctx context.Context, p Pairing, role string) (*websocket.Conn, *websocket.Conn, string, error) {
	return dialWithTimeout(ctx, p, role, connectivity.StageTimeout)
}

func dialWithTimeout(ctx context.Context, p Pairing, role string, timeout time.Duration) (*websocket.Conn, *websocket.Conn, string, error) {
	return dialWithRelayPolicy(ctx, p, role, timeout, true)
}

func dialWithRelayPolicy(ctx context.Context, p Pairing, role string, timeout time.Duration, requireRelay bool) (*websocket.Conn, *websocket.Conn, string, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, "", err
	}
	u, _ := url.Parse(p.Relay)
	u.Path = "/p2p/signal"
	q := u.Query()
	q.Set("role", role)
	q.Set("id", p.Host)
	u.RawQuery = q.Encode()
	connectivity.Report(ctx, "signal", "正在建立信令连接", timeout)
	signalCtx, stopSignal := context.WithTimeout(ctx, timeout)
	defer stopSignal()
	ws, resp, err := dialWebSocket(signalCtx, u.String(), p.RelayToken, "signal")
	if err != nil {
		return nil, nil, "", websocketDialFailure("建立信令连接", resp, err)
	}
	ws.SetReadLimit(2 << 20)
	_, raw, err := ws.Read(signalCtx)
	if err != nil {
		ws.CloseNow()
		return nil, nil, "", fmt.Errorf("等待信令服务响应失败: %w", err)
	}
	stopSignal()
	var f relay.Frame
	if json.Unmarshal(raw, &f) != nil || f.Kind != "welcome" || f.Peer == "" {
		ws.CloseNow()
		return nil, nil, "", errors.New("invalid relay handshake")
	}
	if f.Protocol != 1 {
		ws.CloseNow()
		return nil, nil, "", errors.New("unsupported service protocol")
	}
	connectivity.Note(ctx, "signal", "signal", "信令连接已建立", u.Scheme+"://"+u.Host+"/p2p/signal", "success")
	var data *websocket.Conn
	if f.AllowRelay {
		connectivity.Report(ctx, "relay-ready", "准备备用中继通道", timeout)
		connectivity.Note(ctx, "relay-endpoint", "relay", "中继服务地址", u.Scheme+"://"+u.Host+"/p2p/relay", "info")
		relayCtx, stopRelay := context.WithTimeout(ctx, timeout)
		defer stopRelay()
		u.Path = "/p2p/relay"
		q.Set("peer", f.Peer)
		u.RawQuery = q.Encode()
		data, err = openRelaySocket(relayCtx, u.String(), p.RelayToken, f.Peer)
		if err != nil {
			if requireRelay || ctx.Err() != nil {
				ws.CloseNow()
				return nil, nil, "", err
			}
			connectivity.Note(ctx, "relay-ready", "relay", "备用中继暂不可用，继续尝试 P2P 直连", err.Error(), "warning")
		}
	}
	if data != nil {
		connectivity.Note(ctx, "relay-ready", "relay", "备用中继通道已就绪", u.Host+"/p2p/relay · 优先直连", "success")
	} else if !f.AllowRelay {
		connectivity.Note(ctx, "relay-ready", "relay", "此服务未授予中继权限", u.Host, "warning")
	}
	return ws, data, f.Peer, nil
}
func newWire(ctx context.Context, ws *websocket.Conn, peer, key string, host bool, opts Options, onData func([]byte)) (*wire, error) {
	derived := sha256.Sum256([]byte("reign/p2p/peer/" + peer + "/" + key))
	crypto, err := newCryptor(hex.EncodeToString(derived[:]))
	if err != nil {
		return nil, err
	}
	// Handshake packets use random nonces under the long-term PSK. Data keys
	// additionally bind fresh randomness from both ends, even if relay IDs repeat.
	crypto.random = true
	life, cancel := context.WithCancel(ctx)
	return &wire{ws: ws, peer: peer, base: crypto, key: key, clientNonce: relay.ID() + relay.ID(), ready: make(chan struct{}), host: host, preferDirectUntil: time.Now().Add(1500 * time.Millisecond), opts: opts, ctx: life, cancel: cancel, chunks: make(map[string]*assembly), onData: onData}, nil
}

func (w *wire) aad(kind string, out bool) string {
	direction := "c2h"
	if w.host == out {
		direction = "h2c"
	}
	return "reign/p2p/" + w.peer + "/" + direction + "/" + kind
}
func (w *wire) relaySend(kind string, data []byte) error {
	return w.relaySendContext(w.ctx, kind, data)
}
func (w *wire) relaySendContext(parent context.Context, kind string, data []byte) error {
	crypto := w.cipher(kind)
	if crypto == nil {
		return errors.New("handshake not ready")
	}
	sealed, err := crypto.seal(w.aad(kind, true), data)
	if err != nil {
		return err
	}
	f := relay.Frame{Peer: w.peer, Kind: kind, Body: sealed}
	raw, _ := json.Marshal(f)
	ctx, cancel := context.WithTimeout(parent, connectivity.StageTimeout)
	defer cancel()
	return w.ws.Write(ctx, websocket.MessageText, raw)
}
func (w *wire) receive(f relay.Frame) {
	if w.ctx.Err() != nil {
		return
	}
	if f.Kind == "chunk" {
		w.onChunk(f.Body)
		return
	}
	crypto := w.cipher(f.Kind)
	if crypto == nil {
		return
	}
	plain, err := crypto.open(w.aad(f.Kind, false), f.Body)
	if err != nil {
		return
	}
	if f.Kind == "hello" {
		w.hello(plain)
		return
	}
	if f.Kind == "data" {
		w.onData(plain)
		return
	}
	if f.Kind == "signal" && !w.opts.RelayOnly {
		go w.signal(plain)
	}
}
func (w *wire) path() string {
	w.mu.Lock()
	direct := w.direct
	w.mu.Unlock()
	if direct {
		return "P2P · WebRTC"
	}
	if w.tcpSnapshot() != nil {
		return "P2P · TCP"
	}
	if w.opts.RelayName != "" {
		return "Relay · " + w.opts.RelayName
	}
	if w.dataWS == nil {
		return "Connecting · direct only"
	}
	return "Relay · encrypted"
}

func (w *wire) close() {
	w.cancel()
	w.closeTCP()
	w.mu.Lock()
	pc := w.pc
	w.direct = false
	w.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}
func (w *wire) waitPath(ctx context.Context) error {
	// Give direct negotiation a short head start even when relay is permitted.
	// P2P-only accounts must establish a direct path before any business RPC.
	if w.dataWS == nil || (!w.host && !w.opts.RelayOnly && time.Now().Before(w.preferDirectUntil)) {
		deadline := time.NewTimer(connectivity.StageTimeout)
		defer deadline.Stop()
		for {
			if w.dataWS != nil && !time.Now().Before(w.preferDirectUntil) {
				break
			}
			w.mu.Lock()
			direct := w.direct
			w.mu.Unlock()
			if direct || w.tcpSnapshot() != nil {
				break
			}
			select {
			case <-w.ctx.Done():
				return w.ctx.Err()
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return errors.New("直连失败，且当前没有可用的中继通道，请查看连接详情")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	return nil
}
func (w *wire) send(data []byte) error { return w.sendContext(w.ctx, data) }
func (w *wire) sendContext(ctx context.Context, data []byte) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return errors.New("response exceeds 16 MiB")
	}
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	if err := w.waitPath(ctx); err != nil {
		return err
	}
	w.mu.Lock()
	dc, direct, crypto := w.dc, w.direct, w.crypto
	w.mu.Unlock()
	if crypto == nil {
		return errors.New("handshake not ready")
	}
	if !direct || dc == nil {
		direct = false
	}
	tcp := w.tcpSnapshot()
	// Small ordered chunks avoid SCTP message-size limits for large transcripts.
	id := relay.ID()
	total := (len(data) + 11999) / 12000
	if total == 0 {
		total = 1
	}
	for i := 0; i < total; i++ {
		start, end := i*12000, min((i+1)*12000, len(data))
		part, _ := json.Marshal(chunk{ID: id, Index: i, Total: total, Data: data[start:end]})
		sealed, err := crypto.seal(w.aad("chunk", true), part)
		if err != nil {
			return err
		}
		if !direct && tcp != nil {
			if err = tcp.send(tcpChunk, sealed); err != nil {
				tcp.close()
				return fmt.Errorf("TCP P2P send failed; outcome uncertain, refresh before retrying: %w", err)
			}
			continue
		}
		if !direct {
			if w.dataWS == nil {
				return errors.New("直连不可用；当前权益不包含中继，请检查网络或升级套餐")
			}
			w.progress("path:relay", "relay", "正在使用加密中继", "直连尚未建立，先通过服务中继；后台继续尝试直连", "success")
			frame, _ := json.Marshal(relay.Frame{Peer: w.peer, Kind: "chunk", Body: sealed})
			sendCtx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
			err := w.dataWS.Write(sendCtx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return err
			}
			continue
		}
		deadline := time.Now().Add(8 * time.Second)
		for dc.BufferedAmount() > 512<<10 {
			if time.Now().After(deadline) {
				return errors.New("P2P backpressure timeout")
			}
			select {
			case <-w.ctx.Done():
				return w.ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		if err = dc.Send(sealed); err != nil {
			w.mu.Lock()
			w.direct = false
			w.mu.Unlock()
			return fmt.Errorf("P2P send failed; outcome uncertain, refresh before retrying: %w", err)
		}
	}
	return nil
}
func (w *wire) onChunk(data []byte) {
	crypto := w.cipher("chunk")
	if crypto == nil {
		return
	}
	plain, err := crypto.open(w.aad("chunk", false), data)
	if err != nil {
		return
	}
	var c chunk
	if json.Unmarshal(plain, &c) != nil || c.Total < 1 || c.Total > 1400 || c.Index < 0 || c.Index >= c.Total || len(c.Data) > 12000 {
		return
	}
	w.mu.Lock()
	for id, a := range w.chunks {
		if time.Since(a.created) > 30*time.Second {
			for _, p := range a.parts {
				w.chunkBytes -= len(p)
			}
			delete(w.chunks, id)
		}
	}
	a := w.chunks[c.ID]
	if a == nil {
		if len(w.chunks) >= 8 || w.chunkBytes > 20<<20 {
			w.mu.Unlock()
			return
		}
		a = &assembly{parts: make([][]byte, c.Total), created: time.Now()}
		w.chunks[c.ID] = a
	}
	if len(a.parts) != c.Total || a.parts[c.Index] != nil {
		w.mu.Unlock()
		return
	}
	a.parts[c.Index] = c.Data
	a.count++
	w.chunkBytes += len(c.Data)
	if a.count != c.Total {
		w.mu.Unlock()
		return
	}
	var full []byte
	for _, part := range a.parts {
		full = append(full, part...)
		w.chunkBytes -= len(part)
	}
	delete(w.chunks, c.ID)
	w.mu.Unlock()
	w.onData(full)
}
func (w *wire) setup() (*webrtc.PeerConnection, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pc != nil {
		return w.pc, nil
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: connectivity.STUNOnly(w.opts.ICEServers)})
	if err != nil {
		return nil, err
	}
	w.pc = pc
	for _, url := range w.opts.PublicSTUN {
		w.progress("stun:"+url, "udp", "公共 STUN 节点", url+" · 并行探测", "info")
	}
	for _, url := range w.opts.SelfSTUN {
		w.progress("stun:"+url, "udp", "自建 STUN 节点", url+" · 并行探测", "info")
	}
	w.progress("ice", "udp", "收集 UDP / IPv6 直连候选地址", "公共与自建节点同时参与探测", "running")
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			w.progress("ice", "udp", "UDP 候选地址收集结束", "正在检查可用直连路径", "success")
			go w.reportSTUNStats(pc)
			return
		}
		w.progress("candidate:"+candidate.Address+fmt.Sprint(candidate.Port), "udp", "发现 UDP 直连候选", fmt.Sprintf("%s · %s:%d · %s", candidate.Typ.String(), candidate.Address, candidate.Port, candidate.Protocol.String()), "info")
		raw, err := json.Marshal(map[string]any{"type": "candidate", "candidate": candidate.ToJSON()})
		if err == nil {
			_ = w.relaySend("signal", raw)
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnecting {
			w.progress("ice-check", "udp", "正在验证 UDP 直连路径", "交换候选地址并进行连通性检查", "running")
		}
		if s == webrtc.PeerConnectionStateFailed {
			w.progress("ice-check", "udp", "UDP 直连暂未建立", "继续使用可用的 TCP 直连或中继通道", "warning")
		}
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			w.mu.Lock()
			w.direct = false
			w.mu.Unlock()
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) { w.attach(dc) })
	return pc, nil
}
func (w *wire) attach(dc *webrtc.DataChannel) {
	if dc.Label() != "reign.p2p" {
		_ = dc.Close()
		return
	}
	w.mu.Lock()
	w.dc = dc
	w.mu.Unlock()
	dc.OnOpen(func() {
		w.mu.Lock()
		w.direct = true
		pc := w.pc
		w.mu.Unlock()
		detail := "端到端加密数据通道已建立"
		if pc != nil && pc.SCTP() != nil && pc.SCTP().Transport() != nil {
			if pair, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair(); err == nil && pair != nil {
				detail = pair.String()
			}
		}
		w.progress("path:webrtc", "direct", "已切换为 WebRTC 直连", detail, "success")
	})
	dc.OnClose(func() { w.mu.Lock(); w.direct = false; w.mu.Unlock() })
	dc.OnMessage(func(msg webrtc.DataChannelMessage) { w.onChunk(msg.Data) })
}
func (w *wire) offer() {
	if w.opts.RelayOnly {
		return
	}
	w.startTCP()
	if w.opts.DisableWebRTC {
		return
	}
	pc, err := w.setup()
	if err != nil {
		return
	}
	dc, err := pc.CreateDataChannel("reign.p2p", nil)
	if err != nil {
		return
	}
	w.attach(dc)
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return
	}
	_ = w.local(pc, offer)
}
func (w *wire) local(pc *webrtc.PeerConnection, sdp webrtc.SessionDescription) error {
	if err := pc.SetLocalDescription(sdp); err != nil {
		return err
	}
	// Trickle ICE: exchange SDP immediately. Public and self-hosted STUN
	// candidates follow as they arrive; an unreachable server cannot stall LAN
	// or IPv6 connectivity behind a full gathering timeout.
	raw, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		return err
	}
	return w.relaySend("signal", raw)
}
func (w *wire) signal(raw []byte) {
	w.signalMu.Lock()
	defer w.signalMu.Unlock()
	var signal struct {
		Type      string                   `json:"type"`
		Candidate *webrtc.ICECandidateInit `json:"candidate"`
	}
	if json.Unmarshal(raw, &signal) != nil {
		return
	}
	if signal.Type == "tcp-init" || signal.Type == "tcp-candidate" {
		w.tcpSignal(raw)
		return
	}
	if w.opts.DisableWebRTC {
		return
	}
	if signal.Type == "candidate" {
		if signal.Candidate == nil {
			return
		}
		pc, err := w.setup()
		if err != nil {
			return
		}
		if pc.RemoteDescription() == nil {
			if len(w.candidates) < 256 {
				w.candidates = append(w.candidates, *signal.Candidate)
			}
		} else {
			_ = pc.AddICECandidate(*signal.Candidate)
		}
		return
	}
	var sdp webrtc.SessionDescription
	if json.Unmarshal(raw, &sdp) != nil {
		return
	}
	if w.host && sdp.Type != webrtc.SDPTypeOffer || !w.host && sdp.Type != webrtc.SDPTypeAnswer {
		return
	}
	pc, err := w.setup()
	if err != nil {
		return
	}
	if err = pc.SetRemoteDescription(sdp); err != nil {
		return
	}
	for _, candidate := range w.candidates {
		_ = pc.AddICECandidate(candidate)
	}
	w.candidates = nil
	if w.host {
		answer, err := pc.CreateAnswer(nil)
		if err == nil {
			_ = w.local(pc, answer)
		}
	}
}

var nextRequest atomic.Uint64

type helloMessage struct {
	ClientNonce string `json:"clientNonce"`
	HostNonce   string `json:"hostNonce,omitempty"`
}

func (w *wire) cipher(kind string) *cryptor {
	if kind == "hello" {
		return w.base
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.crypto
}
func (w *wire) handshake(ctx context.Context) error {
	raw, _ := json.Marshal(helloMessage{ClientNonce: w.clientNonce})
	if err := w.relaySendContext(ctx, "hello", raw); err != nil {
		return err
	}
	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}
func (w *wire) hello(raw []byte) {
	var hello helloMessage
	if json.Unmarshal(raw, &hello) != nil {
		return
	}
	clientBytes, err := hex.DecodeString(hello.ClientNonce)
	if err != nil || len(clientBytes) != 32 {
		return
	}
	w.mu.Lock()
	if w.crypto != nil {
		w.mu.Unlock()
		return
	}
	if w.host {
		if hello.HostNonce != "" {
			w.mu.Unlock()
			return
		}
		hello.HostNonce = relay.ID() + relay.ID()
	} else {
		if hello.ClientNonce != w.clientNonce {
			w.mu.Unlock()
			return
		}
		hostBytes, err := hex.DecodeString(hello.HostNonce)
		if err != nil || len(hostBytes) != 32 {
			w.mu.Unlock()
			return
		}
	}
	crypto, err := sessionCryptor(w.key, w.peer, hello, w.host)
	if err != nil {
		w.mu.Unlock()
		return
	}
	w.crypto = crypto
	w.tcpMu.Lock()
	w.tcpKey = sha256.Sum256([]byte("reign/p2p/tcp/" + w.key + "/" + w.peer + "/" + hello.ClientNonce + "/" + hello.HostNonce))
	w.tcpMu.Unlock()
	close(w.ready)
	w.mu.Unlock()
	if w.host {
		response, _ := json.Marshal(hello)
		_ = w.relaySend("hello", response)
	}
}
func sessionCryptor(key, peer string, hello helloMessage, host bool) (*cryptor, error) {
	derived := sha256.Sum256([]byte("reign/p2p/session/" + key + "/" + peer + "/" + hello.ClientNonce + "/" + hello.HostNonce))
	crypto, err := newCryptor(hex.EncodeToString(derived[:]))
	if err != nil {
		return nil, err
	}
	crypto.prefix = [4]byte{1}
	if host {
		crypto.prefix = [4]byte{2}
	}
	return crypto, nil
}

// Native protocol pings are handled by Cloudflare without waking a hibernating
// Durable Object. Keep the outbound relay link healthy even during P2P traffic.
func keepAlive(ctx context.Context, ws *websocket.Conn) {
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	pulses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			probe, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := ws.Ping(probe)
			pulses++
			if err == nil && pulses%3 == 0 {
				err = ws.Write(probe, websocket.MessageText, []byte(`{"kind":"renew"}`))
			}
			cancel()
			if err != nil {
				_ = ws.CloseNow()
				return
			}
		}
	}
}

// Relay is a write-only business ingress; its reader consumes control frames.
func drainRelay(ctx context.Context, ws *websocket.Conn) {
	if ws == nil {
		return
	}
	defer ws.CloseNow()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			return
		}
	}
}

func (w *wire) progress(id, stage, message, detail, state string) {
	if w.opts.Progress == nil || w.ctx.Err() != nil {
		return
	}
	event := connectivity.Progress{ID: id, Stage: stage, Message: message, Detail: detail, State: state}
	w.progressMu.Lock()
	if w.progressLast == nil {
		w.progressLast = map[string]connectivity.Progress{}
	}
	if prior, ok := w.progressLast[id]; ok && prior == event {
		w.progressMu.Unlock()
		return
	}
	if len(w.progressLast) < 128 || strings.HasPrefix(id, "path:") {
		w.progressLast[id] = event
	}
	w.progressMu.Unlock()
	w.opts.Progress(event)
}

func (w *wire) reportSTUNStats(pc *webrtc.PeerConnection) {
	if w.opts.Progress == nil || w.ctx.Err() != nil {
		return
	}
	for _, stat := range pc.GetStats() {
		if c, ok := stat.(webrtc.ICECandidateStats); ok && c.Type == webrtc.StatsTypeLocalCandidate && c.URL != "" {
			w.progress("stun:"+c.URL, "udp", "STUN 节点已返回公网映射", fmt.Sprintf("%s → %s:%d", c.URL, c.IP, c.Port), "success")
		}
	}
}
