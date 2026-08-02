package actresscache

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngHeaderWithDimensions builds a PNG stream whose IHDR declares the given
// dimensions but contains no pixel data. DecodeConfig accepts it, which is
// exactly how a decompression bomb reaches image.Decode.
func pngHeaderWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	sig := []byte{0x89, 'P', 'N', 'G', '\r', 0x0a, 0x1a, 0x0a}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type: truecolor with alpha
	var body bytes.Buffer
	body.Write(sig)
	require.NoError(t, binary.Write(&body, binary.BigEndian, uint32(len(ihdr))))
	body.WriteString("IHDR")
	body.Write(ihdr)
	crcInput := append([]byte("IHDR"), ihdr...)
	require.NoError(t, binary.Write(&body, binary.BigEndian, crc32.ChecksumIEEE(crcInput)))
	return body.Bytes()
}

func TestValidateThumbnailRejectsDecompressionBomb(t *testing.T) {
	body := pngHeaderWithDimensions(t, 1_000_000, 1_000_000)
	_, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "image/png", body, nil), "https://example.test/bomb.png", 0, 1<<20)
	require.Error(t, err)
	var rejected *ThumbnailRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Contains(t, rejected.Reason, "pixel decoder limit")
}

func TestValidateThumbnailAcceptsLargeButBoundedImage(t *testing.T) {
	if MaxThumbnailPixels < 64*64 {
		t.Skip("pixel cap below minimum test image")
	}
	body := makeJPEG(t, 80, 90)
	validation, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "image/jpeg", body, nil), "https://example.test/photo.jpg", 64, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, 80, validation.Width)
}
