// Package thumbs derives small preview images from stored assets. Thumbnails
// are tracked as derived assets, never untracked cache files (spec §20).
package thumbs

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
)

// DefaultMax is the bounding box for generated thumbnails.
const (
	DefaultMaxW = 480
	DefaultMaxH = 480
)

// IsImageMime reports whether a mime type can be thumbnailed with the stdlib.
func IsImageMime(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/jpg", "image/gif":
		return true
	}
	return false
}

// GenerateImage scales an image to fit within maxW x maxH and encodes it as PNG.
// Deterministic for a given input and size.
func GenerateImage(data []byte, maxW, maxH int) ([]byte, int, int, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("thumbs: decode: %w", err)
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return nil, 0, 0, fmt.Errorf("thumbs: empty image")
	}
	if maxW <= 0 {
		maxW = DefaultMaxW
	}
	if maxH <= 0 {
		maxH = DefaultMaxH
	}
	dw, dh := sw, sh
	if sw > maxW || sh > maxH {
		scale := float64(maxW) / float64(sw)
		if s := float64(maxH) / float64(sh); s < scale {
			scale = s
		}
		dw = int(float64(sw) * scale)
		dh = int(float64(sh) * scale)
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, 0, 0, fmt.Errorf("thumbs: encode: %w", err)
	}
	return buf.Bytes(), dw, dh, nil
}
