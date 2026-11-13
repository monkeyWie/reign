// Package nativecap contains host capabilities registered through NSGo's public
// Native/Host APIs. UI and remote protocols do not depend on platform SDKs.
package nativecap

import "github.com/nsgo-dev/nsgo"

func Options() []nsgo.Option { return platformOptions() }
