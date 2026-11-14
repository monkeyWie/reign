package remote

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

const (
	tcpChunk byte = 1
	tcpPing  byte = 2
	tcpPong  byte = 3
)

type tcpPath struct {
	w    *wire
	conn net.Conn
	mu   sync.Mutex
	once sync.Once
	done chan struct{}
}

func (w *wire) tcpSnapshot() *tcpPath {
	w.tcpMu.Lock()
	defer w.tcpMu.Unlock()
	return w.tcp
}
func (w *wire) closeTCP() {
	w.tcpMu.Lock()
	a, p := w.tcpAgent, w.tcp
	w.tcpMu.Unlock()
	if a != nil {
		a.Close()
	}
	if p != nil {
		p.close()
	}
}
func (w *wire) startTCP() {
	if w.opts.RelayOnly || w.opts.DisableTCP || w.ctx.Err() != nil {
		return
	}
	w.tcpMu.Lock()
	if w.tcpStarted {
		w.tcpMu.Unlock()
		return
	}
	w.tcpStarted = true
	a, err := connectivity.NewTCPAgent(w.ctx, func(c connectivity.TCPCandidate) {
		w.progress("tcp-candidate:"+c.Address, "tcp", "发现 TCP 直连候选", c.Kind+" · "+c.Address, "info")
		raw, _ := json.Marshal(struct {
			Type      string                    `json:"type"`
			Candidate connectivity.TCPCandidate `json:"tcpCandidate"`
		}{"tcp-candidate", c})
		_ = w.relaySend("signal", raw)
	}, w.onTCPConn)
	if err != nil {
		w.tcpMu.Unlock()
		w.progress("tcp", "tcp", "无法初始化 TCP 直连", err.Error(), "warning")
		return
	}
	w.tcpAgent = a
	a.Trace = w.opts.TCPTrace
	a.Progress = w.opts.Progress
	w.tcpMu.Unlock()
	_ = w.relaySend("signal", []byte(`{"type":"tcp-init"}`))
	w.progress("tcp", "tcp", "正在尝试 TCP 直连", "与 UDP 探测并行", "running")
	if len(w.opts.TCPSTUN) == 0 {
		w.progress("tcp-config", "tcp", "未配置 TCP 公网探测节点", "仍尝试局域网与 IPv6 TCP 直连", "info")
	}
	a.Start(w.opts.TCPSTUN)
}
func (w *wire) tcpSignal(raw []byte) {
	if w.opts.RelayOnly || w.opts.DisableTCP {
		return
	}
	var s struct {
		Type      string                    `json:"type"`
		Candidate connectivity.TCPCandidate `json:"tcpCandidate"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	w.startTCP()
	w.tcpMu.Lock()
	a := w.tcpAgent
	w.tcpMu.Unlock()
	if a != nil && s.Type == "tcp-candidate" {
		a.AddRemote(s.Candidate)
	}
}

// Both dialing and accepting ends prove possession of a fresh session key.
// Two random challenges bind the proofs to this particular TCP connection,
// preventing replay, reflection, and cross-session socket substitution.
func tcpAuthenticate(c net.Conn, key [32]byte, host bool) error {
	role := byte(0)
	if host {
		role = 1
	}
	local := make([]byte, 41)
	copy(local, "RASTCP01")
	local[8] = role
	if _, err := rand.Read(local[9:]); err != nil {
		return err
	}
	if err := tcpWriteAll(c, local); err != nil {
		return err
	}
	other := make([]byte, len(local))
	if _, err := io.ReadFull(c, other); err != nil {
		return err
	}
	if string(other[:8]) != "RASTCP01" || other[8] != (role^1) {
		return errors.New("invalid TCP peer role/version")
	}
	client, server := local[9:], other[9:]
	if host {
		client, server = other[9:], local[9:]
	}
	proof := func(r byte) []byte {
		m := hmac.New(sha256.New, key[:])
		m.Write([]byte("ras-tcp/v1"))
		m.Write([]byte{r})
		m.Write(client)
		m.Write(server)
		return m.Sum(nil)
	}
	if err := tcpWriteAll(c, proof(role)); err != nil {
		return err
	}
	remote := make([]byte, 32)
	if _, err := io.ReadFull(c, remote); err != nil {
		return err
	}
	if !hmac.Equal(remote, proof(role^1)) {
		return errors.New("TCP peer authentication failed")
	}
	return nil
}

func (w *wire) onTCPConn(c net.Conn) {
	retained := false
	defer func() {
		if !retained {
			_ = c.Close()
		}
	}()
	if w.ctx.Err() != nil || w.tcpSnapshot() != nil {
		return
	}
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	stop := context.AfterFunc(w.ctx, func() { _ = c.Close() })
	defer stop()
	w.tcpMu.Lock()
	key := w.tcpKey
	w.tcpMu.Unlock()
	if key == ([32]byte{}) {
		return
	}
	err := tcpAuthenticate(c, key, w.host)
	if err != nil {
		if w.opts.TCPTrace != nil {
			w.opts.TCPTrace("TCP auth failed host=%t remote=%s: %v", w.host, c.RemoteAddr(), err)
		}
		return
	}
	if w.opts.TCPTrace != nil {
		w.opts.TCPTrace("TCP authenticated host=%t remote=%s", w.host, c.RemoteAddr())
	}
	// The controller selects ONE of the simultaneously established sockets.
	// Without this step opposite ends could choose different crossing dials.
	w.tcpSelectMu.Lock()
	defer w.tcpSelectMu.Unlock()
	if w.ctx.Err() != nil || w.tcpSnapshot() != nil {
		return
	}
	marker := make([]byte, 4)
	if w.host {
		if _, err := io.ReadFull(c, marker); err != nil || string(marker) != "USE1" {
			return
		}
		if tcpWriteAll(c, []byte("ACK1")) != nil {
			return
		}
	} else {
		if tcpWriteAll(c, []byte("USE1")) != nil {
			return
		}
		if _, err := io.ReadFull(c, marker); err != nil || string(marker) != "ACK1" {
			return
		}
	}
	_ = c.SetDeadline(time.Time{})
	p := &tcpPath{w: w, conn: c, done: make(chan struct{})}
	w.tcpMu.Lock()
	w.tcp = p
	a := w.tcpAgent
	w.tcpMu.Unlock()
	if w.opts.TCPTrace != nil {
		w.opts.TCPTrace("TCP selected host=%t remote=%s", w.host, c.RemoteAddr())
	}
	w.progress("path:tcp", "direct", "TCP 直连通道已建立", c.LocalAddr().String()+" → "+c.RemoteAddr().String(), "success")
	retained = true
	if a != nil {
		a.Close()
	}
	go p.read()
	go p.heartbeat()
}
func (p *tcpPath) close() {
	p.once.Do(func() {
		_ = p.conn.Close()
		close(p.done)
		p.w.tcpMu.Lock()
		if p.w.tcp == p {
			p.w.tcp = nil
		}
		p.w.tcpMu.Unlock()
	})
}
func (p *tcpPath) send(kind byte, payload []byte) error {
	if len(payload) > 16<<10 {
		return errors.New("TCP frame too large")
	}
	select {
	case <-p.done:
		return net.ErrClosed
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header, uint32(len(payload)+1))
	header[4] = kind
	if err := tcpWriteAll(p.conn, header); err != nil {
		return err
	}
	return tcpWriteAll(p.conn, payload)
}
func tcpWriteAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func (p *tcpPath) read() {
	defer p.close()
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		header := make([]byte, 4)
		if _, err := io.ReadFull(p.conn, header); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(header)
		if n < 2 || n > 1+(16<<10) {
			return
		}
		packet := make([]byte, int(n))
		if _, err := io.ReadFull(p.conn, packet); err != nil {
			return
		}
		switch packet[0] {
		case tcpChunk:
			p.w.onChunk(packet[1:])
		case tcpPing, tcpPong:
			crypto := p.w.cipher("tcp-control")
			if crypto == nil {
				return
			}
			if _, err := crypto.open(p.w.aad(tcpControlKind(packet[0]), false), packet[1:]); err != nil {
				return
			}
			if packet[0] == tcpPing && p.control(tcpPong) != nil {
				return
			}
		default:
			return
		}
	}
}
func tcpControlKind(kind byte) string {
	if kind == tcpPing {
		return "tcp-ping"
	}
	return "tcp-pong"
}
func (p *tcpPath) control(kind byte) error {
	crypto := p.w.cipher("tcp-control")
	if crypto == nil {
		return errors.New("handshake not ready")
	}
	packet, err := crypto.seal(p.w.aad(tcpControlKind(kind), true), nil)
	if err != nil {
		return err
	}
	return p.send(kind, packet)
}
func (p *tcpPath) heartbeat() {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.w.ctx.Done():
			p.close()
			return
		case <-p.done:
			return
		case <-tick.C:
			if p.control(tcpPing) != nil {
				p.close()
				return
			}
		}
	}
}
