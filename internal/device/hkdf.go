package device

import (
	"crypto/hmac"
	"crypto/sha256"
)

func derive(shared []byte, context string) []byte {
	extract := hmac.New(sha256.New, []byte("reign/pairing"))
	extract.Write(shared)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte(context))
	expand.Write([]byte{1})
	return expand.Sum(nil)
}
