package hubui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui/chat"
)

type Upload struct {
	Name, MIME, ID, Status, Error string
	Data                          []byte
	Sent                          int64
	Target, Provider, Session     string
	cancelled                     atomic.Bool
}

func hashBytes(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func (s *State) chooseFiles(cx *core.Scope) {
	owner := s.key()
	cx.PickFiles(core.PickFilesOptions{Multiple: true, Limit: 16}, func(files []core.File, err error) {
		if err != nil {
			s.error = err.Error()
			s.changed()
			return
		}
		for _, f := range files {
			go func() {
				reader, err := f.Open()
				var b []byte
				if err == nil {
					b, err = io.ReadAll(io.LimitReader(reader, (50<<20)+1))
					reader.Close()
				}
				cx.Post(func() {
					if err != nil {
						s.error = err.Error()
						s.changed()
						return
					}
					if s.key() == owner {
						s.addUpload(f.Name, f.MIME, b)
					}
				})
			}()
		}
	})
}
func (s *State) paste(cx *core.Scope, e core.PasteEvent) {
	for _, f := range e.Files {
		name := f.Name
		if name == "" {
			name = "clipboard.png"
		}
		s.addUpload(name, f.MIME, f.Data)
	}
	s.drop(cx, e.URIs)
}
func (s *State) drop(cx *core.Scope, uris []string) {
	owner := s.key()
	for _, uri := range uris {
		uri := uri
		go func() {
			name, data, err := readDroppedFile(uri)
			cx.Post(func() {
				if err != nil {
					s.error = err.Error()
					s.changed()
					return
				}
				if s.key() == owner {
					s.addUpload(name, mime.TypeByExtension(path.Ext(name)), data)
				}
			})
		}()
	}
}
func (s *State) addUpload(name, mime string, data []byte) {
	if s.selected == nil || !s.connected() {
		return
	}
	total := 0
	for _, u := range s.uploads {
		total += len(u.Data)
	}
	if len(data) > 50<<20 || total+len(data) > 100<<20 || len(s.uploads) >= 16 {
		s.error = "每条消息最多 16 个附件，单个不超过 50 MB，总量不超过 100 MB"
		s.changed()
		return
	}
	u := &Upload{Name: name, MIME: mime, Data: data, Status: "uploading", Target: s.target, Provider: str(s.selected, "providerId"), Session: str(s.selected, "id")}
	s.uploads = append(s.uploads, u)
	s.changed()
	s.upload(u)
}
func (s *State) upload(u *Upload) {
	u.Status = "uploading"
	u.Error = ""
	id := u.ID
	s.changed()
	go func() {
		call := func(method string, p Object) (Object, error) {
			v, e := rpc(s.ctx, s.api, u.Target, u.Provider, u.Session, method, Object{"params": p})
			return object(v), e
		}
		sum := hashBytes(u.Data)
		offset := int64(0)
		var err error
		if id == "" {
			var r Object
			r, err = call("attachment.begin", Object{"name": u.Name, "size": len(u.Data), "sha256": sum})
			id = str(r, "id")
		} else {
			var r Object
			r, err = call("attachment.status", Object{"id": id})
			offset = number(r, "offset")
		}
		for err == nil && offset < int64(len(u.Data)) && !u.cancelled.Load() {
			end := min(offset+(48<<10), int64(len(u.Data)))
			var r Object
			r, err = call("attachment.write", Object{"id": id, "offset": offset, "data": base64.StdEncoding.EncodeToString(u.Data[offset:end])})
			if err != nil {
				break
			}
			next := number(r, "offset")
			if next < end || next > int64(len(u.Data)) {
				err = fmt.Errorf("上传偏移无效")
				break
			}
			offset = next
			progress := offset
			progressID := id
			s.cx.Post(func() { u.ID = progressID; u.Sent = progress; s.changed() })
		}
		if u.cancelled.Load() {
			_, _ = call("attachment.abort", Object{"id": id})
			return
		}
		if err == nil {
			var r Object
			r, err = call("attachment.finish", Object{"id": id})
			a := object(r["attachment"])
			if err == nil && (str(a, "sha256") != sum || number(a, "size") != int64(len(u.Data))) {
				err = fmt.Errorf("上传结果校验失败")
			}
			if err == nil {
				id = str(a, "id")
			}
		}
		s.cx.Post(func() {
			u.ID = id
			u.Sent = offset
			if err != nil {
				u.Status = "failed"
				u.Error = err.Error()
			} else {
				u.Status = "ready"
			}
			s.changed()
		})
	}()
}
func (s *State) uploadPreviews(cx *core.Scope) core.Node {
	rows := core.Each(func() []*Upload { s.read(); return s.uploads }, func(u *Upload) *Upload { return u }, func(cx *core.Scope, get func() *Upload, _ func() int) core.Node {
		u := get()
		remove := func() {
			u.cancelled.Store(true)
			for i, v := range s.uploads {
				if v == u {
					s.uploads = append(s.uploads[:i], s.uploads[i+1:]...)
					break
				}
			}
			s.changed()
		}
		// Bytes and metadata are immutable for this keyed upload. Only progress
		// changes; remounting the image on every poll restarts native decoding.
		var tile core.Node = chat.Attachment{Label: u.Name, Detail: fmt.Sprintf("%.1f KB", float64(len(u.Data))/1024), RemoveLabel: "移除附件", OnRemove: remove}
		if strings.HasPrefix(u.MIME, "image/") || strings.HasSuffix(strings.ToLower(u.Name), ".png") || strings.HasSuffix(strings.ToLower(u.Name), ".jpg") {
			tile = chat.AttachmentPreview{Label: u.Name, Image: core.ImageSource{Data: u.Data}, RemoveLabel: "移除附件", OnRemove: remove}
		}
		status := core.Show{When: func() bool { s.read(); return u.Status != "ready" }, Then: func(cx *core.Scope) core.Node {
			progress := dynamicText(cx, func() string {
				s.read()
				if u.Status == "failed" {
					return "上传中断"
				}
				return fmt.Sprintf("上传 %d%%", u.Sent*100/max(1, int64(len(u.Data))))
			}, 10, true)
			progress.TestID = "upload-progress-" + u.Name
			retry := core.Show{When: func() bool { s.read(); return u.Status == "failed" }, Then: func(cx *core.Scope) core.Node {
				return button(cx, "retry-upload", "重试", nil, "ghost", func() { s.upload(u) })
			}}
			return core.View{Children: []core.Node{progress, retry}}
		}}
		return core.View{TestID: "upload-" + u.Name, Style: core.Style{Gap: 4}, Children: []core.Node{tile, status}}
	})
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8}, Children: []core.Node{rows}}
}

func (s *State) attachmentBytes(ctx context.Context, target, p, session string, a Object, thumbnail bool) ([]byte, error) {
	key := p + "|" + session + "|" + str(a, "id") + fmt.Sprint("|", thumbnail)
	cached, _ := s.api.Call(ctx, "client.assetCache.get", Object{"key": key}, target, nil)
	if b, e := base64.StdEncoding.DecodeString(str(object(cached), "data")); e == nil && len(b) > 0 && (thumbnail || hashBytes(b) == str(a, "sha256")) {
		return b, nil
	}
	if str(a, "state") != "ready" {
		return nil, fmt.Errorf("源文件已失效")
	}
	var out []byte
	var expected string
	var size int64
	for {
		var v any
		var e error
		for attempt := 0; attempt < 3; attempt++ {
			v, e = rpc(ctx, s.api, target, p, session, "attachment.read", Object{"params": Object{"id": a["id"], "offset": len(out), "thumbnail": thumbnail}})
			if e == nil {
				break
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		if e != nil {
			return nil, e
		}
		r := object(v)
		if len(out) == 0 {
			size = number(r, "size")
			expected = str(r, "sha256")
			if size < 0 || size > 50<<20 {
				return nil, fmt.Errorf("附件大小无效")
			}
		}
		if number(r, "offset") != int64(len(out)) || number(r, "size") != size || str(r, "sha256") != expected {
			return nil, fmt.Errorf("附件传输发生变化")
		}
		chunk, e := base64.StdEncoding.DecodeString(str(r, "data"))
		if e != nil {
			return nil, e
		}
		if len(chunk) > 48<<10 || int64(len(out)+len(chunk)) > size {
			return nil, fmt.Errorf("附件分片无效")
		}
		out = append(out, chunk...)
		if flag(r, "eof") {
			if int64(len(out)) != size || hashBytes(out) != expected || !thumbnail && expected != str(a, "sha256") {
				return nil, fmt.Errorf("附件完整性校验失败")
			}
			break
		}
		if len(chunk) == 0 {
			return nil, fmt.Errorf("附件传输未继续")
		}
	}
	_, _ = s.api.Call(ctx, "client.assetCache.put", Object{"key": key, "data": base64.StdEncoding.EncodeToString(out)}, target, nil)
	return out, nil
}
func (s *State) loadImage(a Object, thumbnail bool) {
	key := s.key() + "|" + str(a, "id") + fmt.Sprint("|", thumbnail)
	if s.images[key] != nil || s.loadingImages[key] {
		return
	}
	s.loadingImages[key] = true
	target, p, id := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	go func() {
		b, e := s.attachmentBytes(s.ctx, target, p, id, a, thumbnail)
		s.cx.Post(func() {
			delete(s.loadingImages, key)
			if e == nil {
				s.images[key] = b
			}
			s.changed()
		})
	}()
}
func (s *State) imageTile(cx *core.Scope, message func() Message, get func() Object) core.Node {
	a := core.Untracked(cx, get)
	thumb := flag(a, "thumbnail")
	s.loadImage(a, thumb)
	key := s.key() + "|" + str(a, "id") + fmt.Sprint("|", thumb)
	return core.View{TestID: "image-" + str(a, "id"), Label: "查看图片", Role: core.RoleButton, OnPress: func(core.PressEvent) {
		images := []Object{}
		for _, v := range message().Attachments {
			if str(v, "kind") == "image" {
				if str(v, "id") == str(a, "id") {
					s.previewIndex.Set(len(images))
				}
				images = append(images, v)
			}
		}
		s.preview = images
		s.dialog.Set("preview")
	}, Style: core.Style{Width: core.Pt(96), Height: core.Pt(80), CornerRadius: 10, Overflow: core.Hidden, BorderWidth: 1}, StyleFunc: func() core.Style { return core.Style{BorderColor: palette(cx).border, Background: palette(cx).muted} }, Children: []core.Node{core.Image{SourceFunc: func() core.ImageSource { s.read(); return core.ImageSource{Data: s.images[key]} }, ResizeMode: core.Cover, Style: core.Style{Width: core.Pt(96), Height: core.Pt(80)}}}}
}
func (s *State) fileTile(cx *core.Scope, a Object) core.Node {
	return core.View{Style: core.Style{Padding: core.Edges{Top: 8}}, Children: []core.Node{button(cx, "download-"+str(a, "id"), str(a, "name"), lucide.FileDown, "outline", func() { s.download(cx, a) })}}
}
func (s *State) download(cx *core.Scope, a Object) {
	target, p, id := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.notice = "正在读取附件…"
	s.changed()
	go func() {
		b, e := s.attachmentBytes(s.ctx, target, p, id, a, false)
		cx.Post(func() {
			if e != nil {
				s.error = e.Error()
				s.notice = ""
				s.changed()
				return
			}
			cx.SaveFile(core.SaveOptions{Name: str(a, "name"), MIME: str(a, "mime"), Data: b}, func(saved bool, _ string, err error) {
				s.notice = ""
				if err != nil {
					s.error = err.Error()
				} else if saved {
					s.notice = "文件已保存"
				}
				s.changed()
			})
		})
	}()
}
