//go:build (!darwin || ios) && !windows

package system

import (
	"context"
	"errors"

	"github.com/monkeyWie/reignhub/internal/provider"
)

const DesktopSupported = false
const DesktopUnsupportedReason = "当前系统暂不支持 Codex App 检测与接入"

func FindApp(string) string                                  { return "" }
func FindAppContext(context.Context, string) (string, error) { return "", nil }
func AppExecutable(string) string                            { return "" }
func bundledCLIPaths(string) []string                        { return nil }

func Discover(context.Context, string) ([]provider.Instance, error) {
	return []provider.Instance{}, nil
}

func ConnectDesktop(context.Context, string, string, []provider.Instance, bool) error {
	return errors.New(DesktopUnsupportedReason)
}
