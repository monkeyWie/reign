//go:build !darwin

package host

import "context"

func showAuthorizationWindow(context.Context, string) {}
