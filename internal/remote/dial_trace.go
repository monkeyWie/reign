package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

// A trace explains where the stage budget was spent. It never records headers,
// credentials, query strings or WebSocket message contents.
type socketDialTrace struct {
	ctx            context.Context
	prefix         string
	stage          string
	mu             sync.Mutex
	phase, message string
}

func traceSocketDial(ctx context.Context, stage string, attempt int) (context.Context, *socketDialTrace) {
	t := &socketDialTrace{ctx: ctx, prefix: fmt.Sprintf("%s:dial:%d:", stage, attempt), stage: stage, phase: "route", message: "等待建立网络连接"}
	trace := &httptrace.ClientTrace{
		GetConn:  func(host string) { t.begin("route", "正在准备 WebSocket 网络连接", host) },
		DNSStart: func(info httptrace.DNSStartInfo) { t.begin("dns", "正在解析服务域名", info.Host) },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			ips := []string{}
			for _, ip := range info.Addrs {
				if len(ips) < 6 {
					ips = append(ips, ip.String())
				}
			}
			t.result("dns", "服务域名解析完成", strings.Join(ips, ", "), info.Err)
		},
		ConnectStart: func(network, address string) {
			t.begin("tcp:"+address, "正在建立 TCP 连接", network+" · "+address)
		},
		ConnectDone: func(network, address string, err error) {
			t.result("tcp:"+address, "TCP 连接已建立", network+" · "+address, err)
		},
		TLSHandshakeStart: func() { t.begin("tls", "正在建立 TLS 加密连接", "") },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			t.result("tls", "TLS 加密连接已建立", tls.VersionName(state.Version), err)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			detail := info.Conn.RemoteAddr().String()
			if info.Reused {
				detail += " · 复用连接"
			}
			t.result("route", "网络连接已就绪", detail, nil)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil {
				t.result("http-upgrade", "发送 WebSocket 升级请求失败", "", info.Err)
				return
			}
			t.begin("http-upgrade", "升级请求已发送，等待服务响应", "")
		},
		GotFirstResponseByte: func() { t.begin("http-upgrade", "已收到服务响应，正在确认 WebSocket 升级", "") },
	}
	return httptrace.WithClientTrace(ctx, trace), t
}
func (t *socketDialTrace) begin(phase, message, detail string) {
	t.mu.Lock()
	t.phase, t.message = phase, message
	t.mu.Unlock()
	connectivity.Note(t.ctx, t.prefix+phase, t.stage, message, detail, "running")
}
func (t *socketDialTrace) result(phase, message, detail string, err error) {
	state := "success"
	if err != nil {
		state = "warning"
		message = "网络步骤未完成"
		if detail != "" {
			detail += " · "
		}
		detail += err.Error()
	}
	connectivity.Note(t.ctx, t.prefix+phase, t.stage, message, detail, state)
}
func (t *socketDialTrace) finish(response *http.Response, err error) error {
	t.mu.Lock()
	phase, message := t.phase, t.message
	t.mu.Unlock()
	if err == nil {
		connectivity.Note(t.ctx, t.prefix+"http-upgrade", t.stage, "WebSocket 升级成功", "HTTP 101", "success")
		return nil
	}
	detail := err.Error()
	if response != nil {
		detail = fmt.Sprintf("HTTP %d · %s", response.StatusCode, detail)
	}
	// The stage context may already have expired. Emit its final diagnostic using
	// the observer directly rather than Note, which suppresses canceled events.
	if observe := connectivity.Observer(t.ctx); observe != nil {
		observe(connectivity.Progress{ID: t.prefix + phase, Stage: t.stage, Message: "WebSocket 建连未完成", Detail: message + " · " + detail, State: "warning"})
	}
	return fmt.Errorf("%s: %w", message, err)
}
