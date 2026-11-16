// Package assets preserves session attachments and transfers them over the
// existing authenticated, encrypted control connection.
package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/pkg/attachmentmime"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxFileSize int64 = 50 << 20
const ChunkSize = 48 << 10
const MaxStoreSize int64 = 1 << 30

type Store struct {
	Root string
	mu   sync.Mutex
}

func New(root string) *Store         { return &Store{Root: root} }
func Scope(p, session string) string { return p + "\x00" + session }
func digest(s string) string         { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func validID(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && len(s) == 64
}
func safeName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "attachment"
	}
	if len(s) > 180 {
		ext := filepath.Ext(s)
		if len(ext) > 20 {
			ext = ""
		}
		return "attachment-" + digest(s)[:12] + ext
	}
	return s
}

type record struct {
	Scope         string              `json:"scope"`
	Meta          provider.Attachment `json:"meta"`
	ThumbnailHash string              `json:"thumbnailHash,omitempty"`
	ThumbnailSize int64               `json:"thumbnailSize,omitempty"`
}
type upload struct {
	Scope, Name, SHA256 string
	Size                int64
	Created             int64
}
type Params struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Offset    int64  `json:"offset"`
	Data      string `json:"data"`
	Thumbnail bool   `json:"thumbnail"`
}
type UploadState struct {
	ID         string               `json:"id"`
	Offset     int64                `json:"offset"`
	Attachment *provider.Attachment `json:"attachment,omitempty"`
}
type Chunk struct {
	Data   string `json:"data"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	EOF    bool   `json:"eof"`
}

func (s *Store) init() error {
	for _, dir := range []string{"records", "objects", "uploads", "inputs"} {
		if e := os.MkdirAll(filepath.Join(s.Root, dir), 0700); e != nil {
			return e
		}
	}
	return nil
}
func writeJSON(path string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
func (s *Store) load(scope, id string) (record, error) {
	var r record
	if !validID(id) {
		return r, fmt.Errorf("附件不存在或不属于当前会话")
	}
	b, e := os.ReadFile(filepath.Join(s.Root, "records", id+".json"))
	if e != nil {
		return r, fmt.Errorf("附件不存在或已失效")
	}
	if json.Unmarshal(b, &r) != nil || r.Scope != scope {
		return record{}, fmt.Errorf("附件不存在或不属于当前会话")
	}
	return r, nil
}
func (s *Store) object(hash string) string { return filepath.Join(s.Root, "objects", hash) }
func (s *Store) usage() int64 {
	var size int64
	for _, dir := range []string{"objects", "uploads", "inputs"} {
		_ = filepath.WalkDir(filepath.Join(s.Root, dir), func(path string, ent os.DirEntry, err error) error {
			if err == nil && !ent.IsDir() && !strings.HasPrefix(ent.Name(), ".import-") {
				if info, e := ent.Info(); e == nil {
					size += info.Size()
				}
			}
			return nil
		})
	}
	return size
}
func (s *Store) preserve(ctx context.Context, scope, id, name string, in io.Reader) (record, error) {
	r := record{Scope: scope, Meta: provider.Attachment{ID: id, Name: safeName(name), Kind: "file", State: "ready"}}
	if e := ctx.Err(); e != nil {
		return r, e
	}
	f, e := os.CreateTemp(filepath.Join(s.Root, "uploads"), ".import-*")
	if e != nil {
		return r, e
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(in, MaxFileSize+1))
	ce := f.Close()
	if e != nil {
		return r, e
	}
	if ce != nil {
		return r, ce
	}
	if n > MaxFileSize {
		return r, fmt.Errorf("附件不能超过 50 MB")
	}
	if e = ctx.Err(); e != nil {
		return r, e
	}
	hash := hex.EncodeToString(h.Sum(nil))
	dest := s.object(hash)
	if _, e = os.Stat(dest); os.IsNotExist(e) {
		if s.usage()+n > MaxStoreSize {
			return r, fmt.Errorf("被控端附件存储已满（1 GB）")
		}
		if e = os.Rename(f.Name(), dest); e != nil {
			return r, e
		}
	}
	r.Meta.SHA256, r.Meta.Size = hash, n
	file, e := os.Open(dest)
	if e != nil {
		return r, e
	}
	defer file.Close()
	head := make([]byte, 512)
	used, _ := file.Read(head)
	r.Meta.MIME = attachmentmime.ForFile(r.Meta.Name, http.DetectContentType(head[:used]))
	if strings.HasPrefix(r.Meta.MIME, "image/") && r.Meta.MIME != "image/svg+xml" {
		r.Meta.Kind = "image"
		_, _ = file.Seek(0, io.SeekStart)
		if cfg, _, err := image.DecodeConfig(file); err == nil {
			r.Meta.Width, r.Meta.Height = cfg.Width, cfg.Height
			// Bound decompression before allocating a bitmap. Unsupported or very
			// large images remain downloadable without decoding on the host.
			if cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 24_000_000 {
				_, _ = file.Seek(0, io.SeekStart)
				if img, _, err := image.Decode(file); err == nil {
					w, height := cfg.Width, cfg.Height
					if w > 640 {
						height = max(1, height*640/w)
						w = 640
					}
					if height > 480 {
						w = max(1, w*480/height)
						height = 480
					}
					thumb := image.NewRGBA(image.Rect(0, 0, w, height))
					draw.Draw(thumb, thumb.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
					draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), img, img.Bounds(), draw.Over, nil)
					var out bytes.Buffer
					if jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 80}) == nil {
						sum := sha256.Sum256(out.Bytes())
						r.ThumbnailHash = hex.EncodeToString(sum[:])
						r.ThumbnailSize = int64(out.Len())
						if os.WriteFile(s.object(r.ThumbnailHash), out.Bytes(), 0600) == nil {
							r.Meta.Thumbnail = true
						}
					}
				}
			}
		}
	}
	return r, writeJSON(filepath.Join(s.Root, "records", id+".json"), r)
}

// Import snapshots only an attachment identified by a Provider's actual message.
// ref is stable per item; subsequent polls use the preserved copy even when the
// source was a clipboard temporary file that no longer exists.
func (s *Store) Import(ctx context.Context, scope, ref, source string, inline bool) provider.Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := digest(scope + "\x00" + ref)
	if r, e := s.load(scope, id); e == nil {
		if !strings.HasPrefix(source, "data:") {
			r.Meta.Source = source
		}
		r.Meta.Inline = inline
		return r.Meta
	}
	missing := provider.Attachment{ID: id, Name: safeName(source), Kind: "file", State: "missing", Source: source, Inline: inline}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(strings.ToLower(source), ext) {
			missing.Kind = "image"
		}
	}
	if e := s.init(); e != nil {
		missing.State = "unavailable"
		return missing
	}
	var in io.ReadCloser
	if strings.HasPrefix(source, "data:image/") {
		missing.Source = ""
		parts := strings.SplitN(source, ",", 2)
		missing.Name = "image"
		missing.Kind = "image"
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") || len(parts[1]) > int(MaxFileSize)*4/3+4 {
			return missing
		}
		in = io.NopCloser(base64.NewDecoder(base64.StdEncoding, strings.NewReader(parts[1])))
		missing.Source = ""
	} else {
		f, e := os.Open(source)
		if e != nil {
			return missing
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return missing
		}
		if info.Size() > MaxFileSize {
			f.Close()
			missing.State = "tooLarge"
			return missing
		}
		in = f
	}
	defer in.Close()
	r, e := s.preserve(ctx, scope, id, missing.Name, in)
	if e != nil {
		missing.State = "unavailable"
		return missing
	}
	r.Meta.Source, r.Meta.Inline = missing.Source, inline
	return r.Meta
}

func (s *Store) Handle(ctx context.Context, scope, method string, p Params) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope == "" || strings.HasSuffix(scope, "\x00") {
		return nil, fmt.Errorf("请选择会话")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := s.init(); e != nil {
		return nil, e
	}
	if method == "attachment.begin" {
		if p.Size < 0 || p.Size > MaxFileSize || !validID(p.SHA256) {
			return nil, fmt.Errorf("文件大小或校验值无效，最大 50 MB")
		}
		entries, _ := os.ReadDir(filepath.Join(s.Root, "uploads"))
		count := 0
		for _, ent := range entries {
			info, e := ent.Info()
			if e != nil {
				continue
			}
			if time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(s.Root, "uploads", ent.Name()))
			} else if strings.HasSuffix(ent.Name(), ".json") {
				count++
			}
		}
		if count >= 8 || s.usage()+p.Size > MaxStoreSize {
			return nil, fmt.Errorf("上传空间不足或同时上传数量过多")
		}
		var random [32]byte
		if _, e := rand.Read(random[:]); e != nil {
			return nil, e
		}
		id := hex.EncodeToString(random[:])
		u := upload{Scope: scope, Name: safeName(p.Name), Size: p.Size, SHA256: p.SHA256, Created: time.Now().Unix()}
		if e := writeJSON(filepath.Join(s.Root, "uploads", id+".json"), u); e != nil {
			return nil, e
		}
		if e := os.WriteFile(filepath.Join(s.Root, "uploads", id+".part"), nil, 0600); e != nil {
			return nil, e
		}
		return UploadState{ID: id}, nil
	}
	if !validID(p.ID) {
		return nil, fmt.Errorf("无效附件 ID")
	}
	if method == "attachment.read" {
		r, e := s.load(scope, p.ID)
		if e != nil {
			return nil, e
		}
		hash, size := r.Meta.SHA256, r.Meta.Size
		if p.Thumbnail && r.Meta.Thumbnail {
			hash, size = r.ThumbnailHash, r.ThumbnailSize
		}
		if p.Offset < 0 || p.Offset > size || !validID(hash) {
			return nil, fmt.Errorf("无效附件偏移")
		}
		f, e := os.Open(s.object(hash))
		if e != nil {
			return nil, fmt.Errorf("源文件已失效")
		}
		defer f.Close()
		b := make([]byte, min(int64(ChunkSize), size-p.Offset))
		n, e := f.ReadAt(b, p.Offset)
		if e != nil && e != io.EOF {
			return nil, e
		}
		return Chunk{Data: base64.StdEncoding.EncodeToString(b[:n]), Offset: p.Offset, Size: size, SHA256: hash, EOF: p.Offset+int64(n) >= size}, nil
	}
	if method == "attachment.finish" || method == "attachment.status" {
		if r, e := s.load(scope, p.ID); e == nil {
			return UploadState{ID: p.ID, Offset: r.Meta.Size, Attachment: &r.Meta}, nil
		}
	}
	var u upload
	metaPath, partPath := filepath.Join(s.Root, "uploads", p.ID+".json"), filepath.Join(s.Root, "uploads", p.ID+".part")
	b, e := os.ReadFile(metaPath)
	if e != nil || json.Unmarshal(b, &u) != nil || u.Scope != scope {
		return nil, fmt.Errorf("上传不存在、已过期或不属于当前会话")
	}
	info, e := os.Stat(partPath)
	if e != nil {
		return nil, e
	}
	switch method {
	case "attachment.status":
		return UploadState{ID: p.ID, Offset: info.Size()}, nil
	case "attachment.abort":
		_ = os.Remove(partPath)
		_ = os.Remove(metaPath)
		return nil, nil
	case "attachment.write":
		if len(p.Data) > (ChunkSize*4/3)+4 {
			return nil, fmt.Errorf("附件分块过大")
		}
		data, e := base64.StdEncoding.DecodeString(p.Data)
		if e != nil || len(data) == 0 || len(data) > ChunkSize || p.Offset < 0 || p.Offset+int64(len(data)) > u.Size || p.Offset > info.Size() {
			return nil, fmt.Errorf("无效上传分块或偏移")
		}
		f, e := os.OpenFile(partPath, os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		if p.Offset < info.Size() {
			previous := make([]byte, len(data))
			n, _ := f.ReadAt(previous, p.Offset)
			if n != len(data) || !bytes.Equal(previous, data) {
				return nil, fmt.Errorf("重试分块与已收到的内容不一致")
			}
		} else {
			if s.usage()+int64(len(data)) > MaxStoreSize {
				return nil, fmt.Errorf("被控端附件存储已满（1 GB）")
			}
			if _, e := f.WriteAt(data, p.Offset); e != nil {
				return nil, e
			}
		}
		return UploadState{ID: p.ID, Offset: max(info.Size(), p.Offset+int64(len(data)))}, nil
	case "attachment.finish":
		if info.Size() != u.Size {
			return nil, fmt.Errorf("文件尚未上传完成")
		}
		f, e := os.Open(partPath)
		if e != nil {
			return nil, e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return nil, e
		}
		if hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
			return nil, fmt.Errorf("附件校验失败，请重新上传")
		}
		f, e = os.Open(partPath)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		r, e := s.preserve(ctx, scope, p.ID, u.Name, f)
		if e != nil {
			return nil, e
		}
		_ = os.Remove(metaPath)
		_ = os.Remove(partPath)
		return UploadState{ID: p.ID, Offset: r.Meta.Size, Attachment: &r.Meta}, nil
	}
	return nil, fmt.Errorf("不支持的附件操作")
}

// Input resolves only a completed attachment in this exact Provider/session.
func (s *Store) Input(scope, id string) (provider.Attachment, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.load(scope, id)
	if e != nil {
		return r.Meta, "", e
	}
	if !validID(r.Meta.SHA256) {
		return r.Meta, "", errors.New("附件不可用")
	}
	dir := filepath.Join(s.Root, "inputs", id)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return r.Meta, "", e
	}
	path := filepath.Join(dir, safeName(r.Meta.Name))
	if _, e = os.Stat(path); os.IsNotExist(e) {
		if s.usage()+r.Meta.Size > MaxStoreSize {
			return r.Meta, "", fmt.Errorf("被控端附件存储已满（1 GB）")
		}
		in, err := os.Open(s.object(r.Meta.SHA256))
		if err != nil {
			return r.Meta, "", err
		}
		defer in.Close()
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return r.Meta, "", err
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e != nil {
			_ = os.Remove(path)
			return r.Meta, "", e
		}
		if ce != nil {
			return r.Meta, "", ce
		}
	}
	return r.Meta, path, nil
}
