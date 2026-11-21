package codex

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Exercise the transport's optional guidance contract without touching a live pipe.
type fixturePipeDiagnostic struct {
	cause    error
	guidance string
}

func (e *fixturePipeDiagnostic) Error() string { return e.cause.Error() }
func (e *fixturePipeDiagnostic) Unwrap() error { return e.cause }
func (e *fixturePipeDiagnostic) ConnectionGuidance() string {
	return e.guidance
}

func TestWindowsAppIPCDiagnosticGuidance(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "fixture", Err: windows.ERROR_ACCESS_DENIED}
	for _, guidance := range []string{
		"",
		"Codex 连接访问被拒绝，检测到连接服务可能来自 SSH 环境（PID 42）。请在该环境中关闭使用 Codex 的实例，然后重新打开桌面 Codex App 并重试",
		"Codex 连接访问被拒绝，暂未确认连接服务来源。请检查 SSH、VS Code 中是否仍有使用 Codex 的实例；如有，请关闭该实例，再重新打开桌面 Codex App 并重试。若仍失败，请检查相同权限级别",
	} {
		t.Run(guidance, func(t *testing.T) {
			var input error = cause
			if guidance != "" {
				input = &fixturePipeDiagnostic{cause: cause, guidance: guidance}
			}
			err := appIPCConnectionError(fmt.Errorf("fixture: %w", input))
			var original *os.PathError
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.Is(err, os.ErrPermission) ||
				!errors.As(err, &original) || original != cause {
				t.Fatalf("lost original error: %v", err)
			}
			if guidance != "" {
				if !strings.HasPrefix(err.Error(), guidance+"：") {
					t.Fatalf("provider replaced source or fallback guidance: %v", err)
				}
			} else if !strings.Contains(err.Error(), "相同权限级别") || strings.Contains(err.Error(), "SSH") {
				t.Fatalf("unknown source lost generic guidance: %v", err)
			}
		})
	}
}
