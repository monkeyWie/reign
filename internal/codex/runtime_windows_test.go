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
type fixturePipeDiagnostic struct{ cause error }

func (e *fixturePipeDiagnostic) Error() string { return e.cause.Error() }
func (e *fixturePipeDiagnostic) Unwrap() error { return e.cause }
func (e *fixturePipeDiagnostic) ConnectionGuidance() string {
	return "Codex 连接访问被拒绝，检测到连接服务可能来自 SSH 环境（PID 42）。请在该环境中关闭使用 Codex 的实例，然后重新打开桌面 Codex App 并重试"
}

func TestWindowsAppIPCDiagnosticGuidance(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "fixture", Err: windows.ERROR_ACCESS_DENIED}
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprint(known), func(t *testing.T) {
			var input error = cause
			if known {
				input = &fixturePipeDiagnostic{cause: cause}
			}
			err := appIPCConnectionError(fmt.Errorf("fixture: %w", input))
			var original *os.PathError
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.Is(err, os.ErrPermission) ||
				!errors.As(err, &original) || original != cause {
				t.Fatalf("lost original error: %v", err)
			}
			if known {
				if !strings.Contains(err.Error(), "可能来自 SSH 环境（PID 42）") ||
					!strings.Contains(err.Error(), "在该环境中关闭使用 Codex 的实例") ||
					strings.Contains(err.Error(), "相同权限级别") {
					t.Fatalf("incorrect source guidance: %v", err)
				}
			} else if !strings.Contains(err.Error(), "相同权限级别") || strings.Contains(err.Error(), "SSH") {
				t.Fatalf("unknown source lost generic guidance: %v", err)
			}
		})
	}
}
