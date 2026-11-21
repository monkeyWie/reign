package hubui

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image/png"
	"io"
	"path"
	"strings"

	"golang.org/x/image/bmp"
)

// Clipboard bitmaps are a file format, not a platform API. Normalize them on
// every controller, including restored drafts, before preview and transmission.
func bitmapInput(data []byte) bool { return len(data) >= 2 && data[0] == 'B' && data[1] == 'M' }

var bitmapPreparationSlots = make(chan struct{}, 2)

func (s *State) prepareBitmapUpload(u *Upload) {
	ctx, cancel := context.WithCancel(s.ctx)
	u.cancel, u.active, u.Status, u.Error = cancel, true, "preparing", ""
	s.changed()
	go func() {
		select {
		case bitmapPreparationSlots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		data, err := bitmapPNG(ctx, u.Data)
		<-bitmapPreparationSlots
		s.cx.Post(func() {
			cancel()
			u.active, u.cancel = false, nil
			if s.ctx.Err() != nil || u.cancelled.Load() {
				return
			}
			if err != nil {
				u.Status, u.Error = "failed", "无法读取图片："+err.Error()
				s.changed()
				return
			}
			// Draft persistence caches immutable bytes by upload identity. Replace
			// the object, so a saved BMP can never be reused as the new PNG.
			next := &Upload{Name: strings.TrimSuffix(u.Name, path.Ext(u.Name)) + ".png", MIME: "image/png", Data: data, Status: "queued", Target: u.Target, Provider: u.Provider, Session: u.Session}
			key := u.Target + "|" + u.Provider + "|" + u.Session
			d := s.drafts[key]
			files := d.Uploads
			if s.key() == key {
				files = s.uploads
			}
			for i, old := range files {
				if old != u {
					continue
				}
				files[i] = next
				if s.key() == key {
					s.persistDraft()
				} else if s.draftWriter != nil {
					s.writeDraft(u.Target, u.Provider, u.Session, d)
				}
				s.upload(next)
				return
			}
		})
	}()
}

// x/image reads ordinary BMP and V4/V5 bitfields. Windows CF_DIB also emits
// a 40-byte header with three external RGB masks. For the standard BGR masks,
// expose the equivalent uncompressed header to the existing decoder. RGBX's
// unused high byte stays opaque; it is not an alpha channel.
func bitmapReader(data []byte) io.Reader {
	if len(data) >= 66 && binary.LittleEndian.Uint32(data[14:]) == 40 &&
		binary.LittleEndian.Uint32(data[30:]) == 3 && binary.LittleEndian.Uint32(data[10:]) == 66 &&
		(binary.LittleEndian.Uint16(data[28:]) == 24 || binary.LittleEndian.Uint16(data[28:]) == 32) &&
		binary.LittleEndian.Uint32(data[54:]) == 0xff0000 && binary.LittleEndian.Uint32(data[58:]) == 0xff00 && binary.LittleEndian.Uint32(data[62:]) == 0xff {
		header := bytes.Clone(data[:54])
		binary.LittleEndian.PutUint32(header[2:], uint32(len(data)-12))
		binary.LittleEndian.PutUint32(header[10:], 54)
		binary.LittleEndian.PutUint32(header[30:], 0)
		return io.MultiReader(bytes.NewReader(header), bytes.NewReader(data[66:]))
	}
	return bytes.NewReader(data)
}

func bitmapPNG(ctx context.Context, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, err := bmp.DecodeConfig(bitmapReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return nil, fmt.Errorf("图片尺寸超出支持范围")
	}
	img, err := bmp.Decode(&imageInputReader{ctx: ctx, Reader: bitmapReader(data)})
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&imageInputWriter{ctx: ctx, out: &out}, img); err != nil {
		return nil, err
	}
	return out.Bytes(), ctx.Err()
}

type imageInputReader struct {
	ctx context.Context
	io.Reader
}

func (r *imageInputReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(b)
}

type imageInputWriter struct {
	ctx context.Context
	out *bytes.Buffer
}

func (w *imageInputWriter) Write(b []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.out.Len()+len(b) > 50<<20 {
		return 0, fmt.Errorf("图片超过 50 MB")
	}
	return w.out.Write(b)
}
