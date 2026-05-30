package storage

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

// ImageVariants holds the three WebP-encoded byte slices produced during upload.
type ImageVariants struct {
	Thumb  []byte
	Medium []byte
	Large  []byte
}

// ThumbWidth is the target pixel width for the thumbnail variant.
// Medium and Large are 2× and 4× thumb respectively.
const (
	ThumbWidth  = 200
	MediumWidth = 400
	LargeWidth  = 800
)

// ProcessImage decodes r (JPEG, PNG, GIF, WebP accepted), resizes to three
// standard widths, and encodes every variant as WebP at quality 85.
// The original format is discarded — only WebP variants are returned.
func ProcessImage(r io.Reader) (*ImageVariants, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	thumb, err := encodeWebP(imaging.Resize(src, ThumbWidth, 0, imaging.Lanczos))
	if err != nil {
		return nil, fmt.Errorf("encode thumb: %w", err)
	}
	medium, err := encodeWebP(imaging.Resize(src, MediumWidth, 0, imaging.Lanczos))
	if err != nil {
		return nil, fmt.Errorf("encode medium: %w", err)
	}
	large, err := encodeWebP(imaging.Resize(src, LargeWidth, 0, imaging.Lanczos))
	if err != nil {
		return nil, fmt.Errorf("encode large: %w", err)
	}

	return &ImageVariants{Thumb: thumb, Medium: medium, Large: large}, nil
}

func encodeWebP(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, &webp.Options{Lossless: false, Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
