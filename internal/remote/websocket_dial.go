package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/relay"
)

// Retry only transient transport failures during the upgrade. All attempts
// share the caller's stage deadline; application rejections are never retried.
func dialWebSocket(ctx context.Context, endpoint, token, stage string) (*websocket.Conn, *http.Response, error) {
	return dialWebSocketBudget(ctx, endpoint, token, stage, connectivity.SocketAttemptTimeout)
}
func dialWebSocketBudget(ctx context.Context, endpoint, token, stage string, budget time.Duration) (*websocket.Conn, *http.Response, error) {
	for attempt := 1; ; attempt++ {
		setup, stop := context.WithTimeout(ctx, budget)
		traceCtx, trace := traceSocketDial(setup, stage, attempt)
		ws, response, err := websocket.Dial(traceCtx, endpoint, &websocket.DialOptions{
			HTTPClient: connectivity.ConnectionWebSocketClient,
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		err = trace.finish(response, err)
		stop()
		if err == nil {
			return ws, response, nil
		}
		if attempt >= 3 || ctx.Err() != nil || response != nil || !connectivity.RetryableSetup(err) {
			return nil, response, err
		}
		u, _ := url.Parse(endpoint)
		connectivity.Note(ctx, fmt.Sprintf("%s:transport-retry:%d", stage, attempt), stage,
			"WebSocket 建连被中断，正在重试",
			fmt.Sprintf("%s://%s%s · 第 %d/3 次 · %v", u.Scheme, u.Host, u.Path, attempt+1, err), "warning")
		delay := time.NewTimer(time.Duration(attempt) * 500 * time.Millisecond)
		select {
		case <-ctx.Done():
			delay.Stop()
			return nil, nil, ctx.Err()
		case <-delay.C:
		}
	}
}
func transientDialError(err error) bool {
	return errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
func websocketDialFailure(stage string, response *http.Response, err error) error {
	detail := ""
	if response != nil {
		detail = fmt.Sprintf("HTTP %d", response.StatusCode)
		if response.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 2000))
			response.Body.Close()
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(raw, &payload) == nil && payload.Message != "" {
				detail += " · " + payload.Message
				if payload.Code != "" {
					detail += "（" + payload.Code + "）"
				}
			}
		}
		if ray := response.Header.Get("CF-Ray"); ray != "" && len(ray) < 128 {
			detail += " · CF-Ray: " + ray
		}
	}
	if detail != "" {
		return fmt.Errorf("%s失败（%s）: %w", stage, detail, err)
	}
	if transientDialError(err) {
		return fmt.Errorf("%s失败（底层连接在 WebSocket 握手完成前中断）: %w", stage, err)
	}
	return fmt.Errorf("%s失败: %w", stage, err)
}

func openRelaySocket(ctx context.Context, endpoint, token, peer string) (*websocket.Conn, error) {
	ws, response, err := dialWebSocket(ctx, endpoint, token, "relay")
	if err != nil {
		return nil, websocketDialFailure("建立备用中继通道", response, err)
	}
	ws.SetReadLimit(2 << 20)
	_, raw, err := ws.Read(ctx)
	if err != nil {
		ws.CloseNow()
		return nil, fmt.Errorf("等待中继服务响应失败: %w", err)
	}
	var welcome relay.Frame
	if json.Unmarshal(raw, &welcome) != nil || welcome.Kind != "welcome" || welcome.Peer != peer || welcome.Protocol != 1 {
		ws.CloseNow()
		return nil, errors.New("备用中继服务返回了无效的握手响应")
	}
	return ws, nil
}
