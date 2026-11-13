//go:build !js

package hubui

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func readDroppedFile(uri string) (string, []byte, error) {
	name := uri
	if strings.HasPrefix(uri, "file:") {
		u, e := url.Parse(uri)
		if e != nil {
			return "", nil, e
		}
		name = u.Path
	}
	f, e := os.Open(name)
	if e != nil {
		return "", nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return "", nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 50<<20 {
		return "", nil, fmt.Errorf("仅支持不超过 50 MB 的文件")
	}
	b, e := io.ReadAll(io.LimitReader(f, (50<<20)+1))
	return filepath.Base(name), b, e
}
