//go:build !ios

package nativecap

import "github.com/nsgo-dev/nsgo"

// Android registers capabilities in RasActivity, its NSGo embedding host.
func platformOptions() []nsgo.Option { return nil }
