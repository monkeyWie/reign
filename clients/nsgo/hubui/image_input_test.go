package hubui

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"image/png"
	"testing"
)

// A synthetic CF_DIB-style BMP: external RGB masks, bottom-up rows, and
// unused zero high bytes. No real clipboard or user image is read by tests.
func clipboardBitmapFixture() []byte {
	b := make([]byte, 66+16)
	copy(b, "BM")
	put := func(at int, value uint32) { binary.LittleEndian.PutUint32(b[at:], value) }
	put(2, uint32(len(b)))
	put(10, 66)
	put(14, 40)
	put(18, 2)
	put(22, 2)
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 32)
	put(30, 3)
	put(34, 16)
	put(54, 0xff0000)
	put(58, 0xff00)
	put(62, 0xff)
	copy(b[66:], []byte{255, 0, 0, 0, 255, 255, 255, 0, 0, 0, 255, 0, 0, 255, 0, 0})
	return b
}

func TestBitmapInputBecomesOpaquePNGWithoutChangingSource(t *testing.T) {
	data := clipboardBitmapFixture()
	original := bytes.Clone(data)
	encoded, err := bitmapPNG(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		x, y int
		c    color.NRGBA
	}{
		{0, 0, color.NRGBA{R: 255, A: 255}}, {1, 0, color.NRGBA{G: 255, A: 255}},
		{0, 1, color.NRGBA{B: 255, A: 255}}, {1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	} {
		if got := color.NRGBAModel.Convert(img.At(p.x, p.y)); got != p.c {
			t.Fatalf("pixel (%d,%d): %v, want %v", p.x, p.y, got, p.c)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("normalization changed immutable draft bytes")
	}
}

func TestBitmapInputRejectsInvalidSizeAndRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bitmapPNG(ctx, clipboardBitmapFixture()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, kind := range []string{"truncated", "oversized", "unsupported-mask"} {
		t.Run(kind, func(t *testing.T) {
			b := clipboardBitmapFixture()
			switch kind {
			case "truncated":
				b = b[:70]
			case "oversized":
				binary.LittleEndian.PutUint32(b[18:], 100000)
				binary.LittleEndian.PutUint32(b[22:], 100000)
			case "unsupported-mask":
				binary.LittleEndian.PutUint32(b[54:], 0xff)
			}
			if _, err := bitmapPNG(context.Background(), b); err == nil {
				t.Fatal("invalid bitmap accepted")
			}
		})
	}
}
