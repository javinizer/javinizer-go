package javlibrary

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// posterFixture generates image metadata locally; no CDN content is needed.
func posterFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode poster fixture: %v", err)
	}
	return buf.Bytes()
}
