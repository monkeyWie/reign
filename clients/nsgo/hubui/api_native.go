//go:build !js

package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reignhub/internal/device"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/bridge"
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
func platformClientInfo() device.ClientInfo {
	return device.ClientInfo{Name: platformDeviceName(), Platform: runtime.GOOS, Kind: "native"}.Normalize()
}
func resolveClientInfo(cx *core.Scope, done func(device.ClientInfo)) {
	info := platformClientInfo()
	if runtime.GOOS == "darwin" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
				info.Name = strings.TrimSpace(string(out))
			}
			cx.Post(func() { done(info.Normalize()) })
		}()
		return
	}
	if runtime.GOOS != "android" && runtime.GOOS != "ios" {
		done(info)
		return
	}
	cx.CallHost("ras.device", nil, func(v any, err error) {
		if err == nil {
			if name := strings.TrimSpace(str(object(v), "name")); name != "" {
				info.Name = name
			}
			info.Model = str(object(v), "model")
			if runtime.GOOS == "ios" {
				info.Name = appleDeviceName(info.Name, info.Model)
			}
		}
		done(info.Normalize())
	})
}
