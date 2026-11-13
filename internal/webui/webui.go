// Package webui serves the NSGo Web client compiled into release binaries.
// The release builder populates assets in an isolated staging tree, so a
// release never accidentally embeds an old developer build or private data.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:assets
var assets embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(root, "nsgo.json"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "RAS Web is not embedded in this development build. Build a release with clients/nsgo/scripts/package-service.py or supply --web DIR.", http.StatusServiceUnavailable)
		})
	}
	return fileHandler(root)
}

func fileHandler(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The manifest names content-hashed assets. Always refresh the entry
		// points after upgrading the single binary, including browser caches.
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/nsgo.json" || r.URL.Path == "/manifest.webmanifest" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
