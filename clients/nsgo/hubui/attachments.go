package hubui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui/chat"
)

type Upload struct {
	Name, MIME, ID, Status, Error string
	SHA256                        string
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
	capabilities := s.agentCapabilities()
	if capabilities != nil {
		image := strings.HasPrefix(mime, "image/") || strings.HasPrefix(http.DetectContentType(data), "image/")
		if image && !flag(capabilities, "imageInput") || !image && !flag(capabilities, "fileInput") {
			s.error = "此 Agent 不支持这种附件类型"
			s.changed()
			return
		}
		if limit := number(capabilities, "maxAttachmentBytes"); limit > 0 && int64(len(data)) > limit {
			s.error = fmt.Sprintf("此 Agent 的单个附件上限为 %d MiB", limit/(1<<20))
			s.changed()
			return
		}
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
				u.SHA256 = sum
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
	return s.attachmentBytesProgress(ctx, target, p, session, a, thumbnail, nil)
}

// Read immutable chunks with bounded concurrency, checking every offset and the
// final digest. Progress counts bytes actually received, never queued requests.
func (s *State) attachmentBytesProgress(ctx context.Context, target, p, session string, a Object, thumbnail bool, progress func(int64, int64)) ([]byte, error) {
	key := p + "|" + session + "|" + str(a, "id") + fmt.Sprint("|", thumbnail)
	cached, _ := s.api.Call(ctx, "client.assetCache.get", Object{"key": key}, target, nil)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if b, e := base64.StdEncoding.DecodeString(str(object(cached), "data")); e == nil && len(b) > 0 && (thumbnail || hashBytes(b) == str(a, "sha256")) {
		if progress != nil {
			progress(int64(len(b)), int64(len(b)))
		}
		return b, nil
	}
	if str(a, "state") != "ready" {
		return nil, fmt.Errorf("源文件已失效")
	}
	const chunkSize = 48 << 10
	read := func(ctx context.Context, offset int64) (Object, []byte, error) {
		var v any
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			v, err = rpc(ctx, s.api, target, p, session, "attachment.read", Object{"params": Object{"id": a["id"], "offset": offset, "thumbnail": thumbnail}})
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, nil, err
		}
		r := object(v)
		chunk, err := base64.StdEncoding.DecodeString(str(r, "data"))
		if err != nil {
			return nil, nil, err
		}
		return r, chunk, nil
	}
	first, chunk, err := read(ctx, 0)
	if err != nil {
		return nil, err
	}
	size, expected := number(first, "size"), str(first, "sha256")
	if size < 0 || size > 50<<20 {
		return nil, fmt.Errorf("附件大小无效")
	}
	if !thumbnail && expected != str(a, "sha256") {
		return nil, fmt.Errorf("附件完整性校验失败")
	}
	out := make([]byte, int(size))
	accept := func(r Object, chunk []byte, offset int64) error {
		if number(r, "offset") != offset || number(r, "size") != size || str(r, "sha256") != expected {
			return fmt.Errorf("附件传输发生变化")
		}
		end := offset + int64(len(chunk))
		if int64(len(chunk)) != min(int64(chunkSize), size-offset) || end > size || flag(r, "eof") != (end == size) {
			return fmt.Errorf("附件分片无效")
		}
		copy(out[offset:end], chunk)
		return nil
	}
	if err = accept(first, chunk, 0); err != nil {
		return nil, err
	}
	var progressMu sync.Mutex
	received := int64(len(chunk))
	if progress != nil {
		progress(received, size)
	}
	group, downloadCtx := errgroup.WithContext(ctx)
	// One metadata chunk, then at most four in-flight reads. Keep capacity for
	// session watch and foreground commands on the host's eight RPC slots.
	group.SetLimit(4)
	for offset := int64(len(chunk)); offset < size; offset += chunkSize {
		if downloadCtx.Err() != nil {
			break
		}
		group.Go(func() error {
			r, b, err := read(downloadCtx, offset)
			if err != nil {
				return err
			}
			if err = accept(r, b, offset); err != nil {
				return err
			}
			progressMu.Lock()
			received += int64(len(b))
			if progress != nil {
				progress(received, size)
			}
			progressMu.Unlock()
			return nil
		})
	}
	if err = group.Wait(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if hashBytes(out) != expected {
		return nil, fmt.Errorf("附件完整性校验失败")
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
	image := core.Image{Label: str(a, "name"), SourceFunc: func() core.ImageSource { s.read(); return core.ImageSource{Data: s.images[key]} }, ResizeMode: core.Cover, Style: core.Style{Width: core.Pct(100), Height: core.Pt(128)}}
	return s.attachmentCard(cx, a, func() { s.previewAttachment(message(), a) }, image)
}
func (s *State) fileTile(cx *core.Scope, a Object) core.Node {
	// Unsupported previews are read-only content, not a misleading button.
	return s.attachmentCard(cx, a, nil)
}

func (s *State) attachmentCard(cx *core.Scope, a Object, preview func(), content ...core.Node) core.Node {
	name := str(a, "name")
	if name == "" {
		name = "附件"
	}
	download := labelledIconButton(cx, "download-"+str(a, "id"), "下载 "+name, lucide.Download, func() { s.download(cx, a) }).(core.View)
	download.Style.Position, download.Style.Right, download.Style.Bottom = core.Absolute, core.Pt(6), core.Pt(6)
	caption := singleLine(cx, func() string { return name }, 12, false)
	captionView := core.View{TestID: "attachment-name-" + str(a, "id"), Tooltip: label(cx, name, 12, false), Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{caption}}
	footer := []core.Node{}
	if str(a, "kind") != "image" {
		footer = append(footer, glyph(cx, lucide.FileText, 18))
	}
	footer = append(footer, captionView)
	content = append(content, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, MinWidth: core.Pt(0), MinHeight: core.Pt(48), Padding: core.Edges{Top: 6, Bottom: 6, Left: 10, Right: 52}, Gap: 6}, Children: footer})
	body := core.View{Style: core.Style{MinWidth: core.Pt(0)}, Children: content}
	if preview != nil {
		body.TestID, body.Role, body.Label = "image-"+str(a, "id"), core.RoleButton, "预览图片 "+name
		body.OnPress = func(core.PressEvent) { preview() }
		body.StyleFunc = func() core.Style { return pressStyle(cx) }
	}
	return core.View{TestID: "attachment-" + str(a, "id"), Style: core.Style{MinWidth: core.Pt(0), Width: core.Pct(100), AlignSelf: core.AlignStart, CornerRadius: 10, Overflow: core.Hidden, BorderWidth: 1}, StyleFunc: func() core.Style {
		return core.Style{BorderColor: palette(cx).border, Background: palette(cx).muted}
	}, Children: []core.Node{body, download}}
}

func (s *State) previewAttachment(message Message, a Object) {
	if str(a, "kind") != "image" {
		return
	}
	images := []Object{}
	index := 0
	for _, v := range message.Attachments {
		if str(v, "kind") == "image" {
			if str(v, "id") == str(a, "id") {
				index = len(images)
			}
			images = append(images, v)
		}
	}
	s.preview = images
	s.previewIndex.Set(index)
	s.dialog.Set("preview")
}

const attachmentCardMinimum core.Points = 180
const attachmentCardGap core.Points = 12

func (s *State) attachmentGallery(cx *core.Scope, get func() Message) core.Node {
	attachments := core.NewMemoFunc(cx, func() []Object {
		out := []Object{}
		for _, a := range get().Attachments {
			// Inline images render at their Markdown location. Inline file
			// links still need an explicit download action in this collection.
			if !flag(a, "inline") || str(a, "kind") != "image" {
				out = append(out, a)
			}
		}
		return out
	}, nil)
	return core.Show{When: func() bool { return len(attachments.Get()) > 0 }, Then: func(cx *core.Scope) core.Node {
		grid := cardGrid(cx, "attachments-"+get().ID, attachmentCardMinimum, attachmentCardGap,
			core.Each(attachments.Get, func(a Object) string { return str(a, "id") }, func(cx *core.Scope, a func() Object, _ func() int) core.Node {
				if str(a(), "kind") == "image" {
					return s.imageTile(cx, get, a)
				}
				return s.fileTile(cx, a())
			}))
		return core.View{Style: core.Style{Width: core.Pct(100), MinWidth: core.Pt(0), Padding: core.Edges{Bottom: 12}}, StyleFunc: func() core.Style {
			// Keep sparse galleries compact while dense collections fill the
			// same message column. User attachments retain right alignment.
			count := len(attachments.Get())
			limit := choose(get().Role == "user", core.Points(680), core.Points(820))
			return core.Style{MaxWidth: core.Pt(min(limit, core.Points(count)*attachmentCardMinimum+core.Points(max(0, count-1))*attachmentCardGap))}
		}, Children: []core.Node{grid}}
	}}
}
