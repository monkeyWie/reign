package webui

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestEmbeddedFileHTTPBehavior(t *testing.T) {
	h := fileHandler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<title>RAS</title>")},
		"app.wasm":   &fstest.MapFile{Data: []byte("\x00asmfixture")},
	})
	r := httptest.NewRequest("GET", "http://ras/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "<title>RAS</title>" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	r = httptest.NewRequest("GET", "http://ras/app.wasm", nil)
	r.Header.Set("Range", "bytes=0-3")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "\x00asm" || w.Header().Get("Content-Type") != "application/wasm" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	r = httptest.NewRequest("GET", "http://ras/data/devices.json", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("served data outside embedded asset set", w.Code)
	}
}
