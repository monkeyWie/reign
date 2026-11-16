// Package transport defines the encrypted RPC framing shared by native and WASM clients.
package transport

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
)

const (
	Version    = 2
	ChunkSize  = 4096
	MaxPayload = 16 << 20
	Identity   = "identity"
	Gzip       = "gzip"
)

type Chunk struct {
	Request  string `json:"r"`
	ID       string `json:"id"`
	Index    int    `json:"i"`
	Total    int    `json:"n"`
	Encoding string `json:"e"`
	RawSize  int    `json:"s"`
	Data     []byte `json:"b"`
}

func (c Chunk) Valid() bool {
	return c.Request != "" && len(c.Request) <= 256 && c.ID != "" && c.Total >= 1 && c.Total <= MaxPayload/ChunkSize && c.Index >= 0 && c.Index < c.Total && len(c.Data) <= ChunkSize && c.RawSize > 0 && c.RawSize <= MaxPayload && (c.Encoding == Identity || c.Encoding == Gzip)
}

// Compress before encryption; incompressible files keep their original bytes.
// BestSpeed avoids spending scarce mobile CPU on marginal size reductions.
func Encode(raw []byte) (data []byte, encoding string, err error) {
	if len(raw) == 0 || len(raw) > MaxPayload {
		return nil, "", errors.New("RPC payload must be between 1 byte and 16 MiB")
	}
	if len(raw) < 16<<10 {
		return raw, Identity, nil
	}
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, gzip.BestSpeed)
	if _, err = w.Write(raw); err != nil {
		return nil, "", err
	}
	if err = w.Close(); err != nil {
		return nil, "", err
	}
	if b.Len()*8 > len(raw)*7 {
		return raw, Identity, nil
	}
	return b.Bytes(), Gzip, nil
}
func Decode(data []byte, encoding string, rawSize int) ([]byte, error) {
	if rawSize < 1 || rawSize > MaxPayload || len(data) > MaxPayload {
		return nil, errors.New("RPC payload exceeds limit")
	}
	if encoding == Identity {
		if len(data) != rawSize {
			return nil, errors.New("RPC payload length mismatch")
		}
		return data, nil
	}
	if encoding != Gzip {
		return nil, errors.New("unknown RPC encoding")
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, int64(rawSize)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) != rawSize {
		return nil, errors.New("RPC decompressed length mismatch")
	}
	return raw, nil
}
