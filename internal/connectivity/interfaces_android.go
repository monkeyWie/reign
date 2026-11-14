//go:build android && !cgo

package connectivity

import "github.com/wlynxg/anet"

func init() {
	// NSGo's arm64 APK is pure Go. Without CGo, anet's API-level detector
	// returns -1 and Pion falls back to RTM_GETLINK, blocked on Android 11+.
	// Select its RTM_GETADDR/ioctl implementation explicitly. That path also
	// works on our older supported Android versions; no JNI/NDK is required.
	// Set once at startup, before Pion or TCP candidates enumerate interfaces.
	anet.SetAndroidVersion(11)
}
