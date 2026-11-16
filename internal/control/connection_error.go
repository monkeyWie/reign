package control

const ConnectionAuthorizationExpired = "CONNECTION_AUTH_EXPIRED"

// NeedsConnectionRenewal also recognizes older hosts' messages so upgrading
// either endpoint first recovers the connection without suggesting account login.
func NeedsConnectionRenewal(code, message string) bool {
	return code == ConnectionAuthorizationExpired || message == "远控授权已过期，请重新连接或登录" || message == "远控授权已过期，请重新连接"
}
