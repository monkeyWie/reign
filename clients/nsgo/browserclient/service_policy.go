package browserclient

// AllowLANHTTP is retained for compatibility with existing build scripts.
var AllowLANHTTP = "false"

func CleartextAllowed(host string) bool {
	return host != ""
}
