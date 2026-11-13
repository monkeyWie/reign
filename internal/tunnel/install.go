package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Pin official Cloudflare release artifacts; never execute a download before
// checking its digest. Version and checksums are upgraded together.
// macOS pins refer to the downloadable notarized tarballs (checked 2026-11-13),
// whose hashes differ from the earlier tarballs listed in the release notes.
const Version = "2026.10.4"

type artifact struct{ name, digest string }

var artifacts = map[string]artifact{
	"darwin/arm64":  {"cloudflared-darwin-arm64.tgz", "68b6bc4080ac1330234db2a9002d07f98b000537a1bd531a687b660d4160fa7f"},
	"darwin/amd64":  {"cloudflared-darwin-amd64.tgz", "f11fbef4207d715892331917dafd437f032cf4b7137ed64dea3008338c1c613d"},
	"linux/arm64":   {"cloudflared-linux-arm64", "2b7203b4fb0ba28a0c1e12f8855ce2c25f76d266a8d5edafac5540b6812acff6"},
	"linux/amd64":   {"cloudflared-linux-amd64", "ae5e8add7884b0531d66bbca4570d1de491ff6da51e4ebe7eb160cdf241d9e42"},
	"windows/amd64": {"cloudflared-windows-amd64.exe", "411b16d40c8d872243efeb52c550053d00af0a8a97fbb32e730fb8aba429eaa8"},
	"windows/arm64": {"cloudflared-windows-amd64.exe", "411b16d40c8d872243efeb52c550053d00af0a8a97fbb32e730fb8aba429eaa8"},
}
var installMu sync.Mutex

func Install(ctx context.Context, dir string) (string, error) {
	installMu.Lock()
	defer installMu.Unlock()
	a, ok := artifacts[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", errors.New("此平台不支持 cloudflared 被控端")
	}
	dir = filepath.Join(dir, "tools", "cloudflared", Version)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	// Cached executables have a separate digest because macOS releases are tarballs.
	if expected, e := os.ReadFile(dst + ".sha256"); e == nil {
		if actual, e := fileDigest(dst); e == nil && actual == string(expected) {
			return dst, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", "https://github.com/cloudflare/cloudflared/releases/download/"+Version+"/"+a.name, nil)
	if e != nil {
		return "", e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cloudflared 下载失败：HTTP %d", resp.StatusCode)
	}
	archive, e := os.CreateTemp(dir, ".download-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(archive.Name())
	n, e := io.Copy(archive, io.LimitReader(resp.Body, 160<<20+1))
	ce := archive.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	if n > 160<<20 {
		return "", errors.New("cloudflared 下载超过大小限制")
	}
	digest, e := fileDigest(archive.Name())
	if e != nil {
		return "", e
	}
	if digest != a.digest {
		return "", errors.New("cloudflared 校验失败，未执行下载文件")
	}
	src, e := os.Open(archive.Name())
	if e != nil {
		return "", e
	}
	defer src.Close()
	var reader io.Reader = src
	if filepath.Ext(a.name) == ".tgz" {
		gz, e := gzip.NewReader(src)
		if e != nil {
			return "", e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		found := false
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", e
			}
			if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "cloudflared" {
				reader = tr
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("cloudflared 压缩包缺少程序")
		}
	}
	f, e := os.CreateTemp(dir, ".binary-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	n, e = io.Copy(f, io.LimitReader(reader, 200<<20+1))
	if e == nil {
		e = f.Chmod(0755)
	}
	ce = f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	if n > 200<<20 {
		return "", errors.New("cloudflared 解压超过大小限制")
	}
	binaryDigest, e := fileDigest(f.Name())
	if e != nil {
		return "", e
	}
	if e = os.Rename(f.Name(), dst); e != nil {
		return "", e
	}
	if e = os.WriteFile(dst+".sha256", []byte(binaryDigest), 0600); e != nil {
		return "", e
	}
	return dst, nil
}
func fileDigest(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
