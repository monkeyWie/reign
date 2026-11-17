package servercmd

import (
	"crypto/tls"
	"net/http"
	"net/url"
)

func configureTLS(s *http.Server, cert, key string) error {
	identity, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}
	s.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{identity}}
	return nil
}
func validOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}
