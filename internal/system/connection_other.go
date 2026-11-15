//go:build !darwin || ios

package system

import (
	"context"
	"errors"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func DesktopConnections(context.Context, []provider.Instance, string) (int, error) {
	return 0, errors.New(DesktopUnsupportedReason)
}
