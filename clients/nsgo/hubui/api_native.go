//go:build !js

package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/monkeyWie/reign/internal/bridge"
	"github.com/nsgo-dev/nsgo/core"
)

type nativeAPI struct{ dir, uiPath string }

func platformAPI(cx *core.Scope) API {
	dir := filepath.Join(cx.DataDir(), "agenthub")
	if custom := os.Getenv("AGENTHUB_NSGO_DATA_DIR"); custom != "" {
		dir = custom
	}
	uiPath := ""
	if exe, err := os.Executable(); err == nil && runtime.GOOS == "darwin" {
		uiPath = filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	}
	return &nativeAPI{dir: dir, uiPath: uiPath}
}
func (*nativeAPI) CanHost() bool { return runtime.GOOS != "ios" && runtime.GOOS != "android" }
func (*nativeAPI) Close()        { bridge.Shutdown() }
func (n *nativeAPI) Call(ctx context.Context, method string, params Object, target string, request Object) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(Object{"method": method, "dataDir": n.dir, "hostDir": n.dir, "uiPath": n.uiPath, "params": params, "target": target, "request": request})
	if err != nil {
		return nil, err
	}
	var answer struct {
		Result any    `json:"result"`
		Error  string `json:"error"`
	}
	if err = json.Unmarshal([]byte(bridge.CallContext(ctx, string(b))), &answer); err != nil {
		return nil, err
	}
	if answer.Error != "" {
		return nil, errors.New(answer.Error)
	}
	return answer.Result, ctx.Err()
}
func serviceOrigin() string {
	if service := strings.TrimSpace(os.Getenv("AGENTHUB_SERVER_URL")); service != "" {
		return strings.TrimRight(service, "/")
	}
	return DefaultService
}
func demoRequested() bool { return os.Getenv("AGENTHUB_NSGO_DEMO") == "1" }
func isWeb() bool         { return false }

func platformDeviceName() string {
	if runtime.GOOS == "ios" {
		return "iPhone / iPad"
	}
	if runtime.GOOS == "android" {
		return "Android 设备"
	}
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return runtime.GOOS + " 设备"
}
func resolveDeviceName(cx *core.Scope, done func(string)) {
	if runtime.GOOS != "android" && runtime.GOOS != "ios" {
		done(platformDeviceName())
		return
	}
	cx.CallHost("ras.device", nil, func(v any, err error) {
		if err == nil {
			if name := strings.TrimSpace(str(object(v), "name")); name != "" {
				done(name)
				return
			}
		}
		done(platformDeviceName())
	})
}
