// Package webui serves the NSGo Web console from a release binary or asset directory.
// Release builders populate assets in an isolated tree, never embedding local state.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
)

//go:embed all:assets
var assets embed.FS
var immutable = regexp.MustCompile(`\.[0-9a-f]{10}\.`)

func Handler() http.Handler {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	if err := validate(root); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Web console is not embedded in this development build. Use scripts/platform/package-server.py or --web DIR with built console assets.", 503)
		})
	}
	return consoleHandler(root)
}
func Directory(path string) (http.Handler, error) {
	root := os.DirFS(path)
	if err := validate(root); err != nil {
		return nil, fmt.Errorf("Web assets: %w", err)
	}
	return consoleHandler(root), nil
}
func validate(root fs.FS) error {
	for _, name := range []string{"index.html", "nsgo.json"} {
		info, err := fs.Stat(root, name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("%s must be a file", name)
		}
	}
	return nil
}
func consoleHandler(root fs.FS) http.Handler {
	files := fileHandler(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/chat" {
			http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
			return
		}
		req := r.Clone(r.Context())
		switch {
		case r.URL.Path == "/chat/":
			req.URL.Path = "/"
		case strings.HasPrefix(r.URL.Path, "/assets/chat/"):
			req.URL.Path = strings.TrimPrefix(r.URL.Path, "/assets/chat")
		default:
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, req)
	})
}
func fileHandler(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if immutable.MatchString(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		// WASM gzip/part files are downloaded as bytes and decompressed by the loader.
		// Do not set Content-Encoding, which would cause automatic browser decompression.
		if strings.HasSuffix(r.URL.Path, ".bin") {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(root, strings.TrimPrefix(r.URL.Path, "/"))
		if r.URL.Path != "/" && (err != nil || info.IsDir()) {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
