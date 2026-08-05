package actresscache

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngHeaderOnly builds a valid PNG signature+IHDR without pixel data; wide
// dimension headers must be rejected by the pixel cap before decoding.
func pngHeaderOnly(t *testing.T, width, height uint32) []byte {
	t.Helper()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit truecolor
	out := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(ihdr)))
	out = append(out, lb[:]...)
	out = append(out, []byte("IHDR")...)
	out = append(out, ihdr...)
	binary.BigEndian.PutUint32(lb[:], crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	return append(out, lb[:]...)
}

func TestValidateThumbnailRejectsOverflowingPixelCount(t *testing.T) {
	// 100000x300000 = 3e10 pixels; far above MaxThumbnailPixels. The cap
	// comparison must stay sound for int32-scale headers (the multiplication
	// form risks wraparound against decoders reporting broader ranges), so
	// rejection happens before any pixel-buffer allocation.
	fetcher := testFetcher(200, "image/png", pngHeaderOnly(t, 100000, 300000), nil)
	_, err := ValidateThumbnail(context.Background(), fetcher, "https://cdn.test/huge.png", 1, 1<<30)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceed the 20000000 pixel decoder limit")
}
