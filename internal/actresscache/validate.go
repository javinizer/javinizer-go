package actresscache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	_ "golang.org/x/image/webp"
)

// ThumbnailRejectedError ...
type ThumbnailRejectedError struct {
	Reason string
}

// Error ...
func (e *ThumbnailRejectedError) Error() string {
	return "thumbnail rejected: " + e.Reason
}

// MaxThumbnailPixels bounds decoded thumbnail dimensions. The response byte
// limit only caps the compressed payload; a tiny, highly compressed image can
// declare enormous dimensions and make image.Decode allocate the full pixel
// buffer, so the header dimensions are checked before decoding.
const MaxThumbnailPixels = 100_000_000

// ValidateThumbnail ...
func ValidateThumbnail(ctx context.Context, fetcher *Fetcher, rawURL string, minDimension int, maxBytes int64) (ThumbnailValidation, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "thumbnail URL is empty"}
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "thumbnail URL is invalid"}
	}
	if models.IsKnownInvalidDMMActressThumbnail(rawURL) {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "known DMM placeholder URL"}
	}
	if isKnownSourcePlaceholder(u) {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "known source placeholder URL"}
	}
	body, headers, err := fetcher.Get(ctx, rawURL, "image/avif,image/webp,image/apng,image/*,*/*;q=0.8", maxBytes)
	if err != nil {
		// statusErr ...
		var statusErr *HTTPError
		if errors.As(err, &statusErr) {
			if statusErr.IsTransient() {
				return ThumbnailValidation{}, err
			}
			return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: statusErr.Error()}
		}
		var blockedErr *BlockedFetchError
		if errors.As(err, &blockedErr) {
			return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: blockedErr.Error()}
		}
		return ThumbnailValidation{}, err
	}
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("content type is %q", headers.Get("Content-Type"))}
	}
	header, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("image decode failed: %v", err)}
	}
	if header.Width <= 0 || header.Height <= 0 {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("image reports invalid dimensions %dx%d", header.Width, header.Height)}
	}
	if int64(header.Width)*int64(header.Height) > MaxThumbnailPixels {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("dimensions %dx%d exceed the %d pixel decoder limit", header.Width, header.Height, MaxThumbnailPixels)}
	}
	if minDimension > 0 && (header.Width < minDimension || header.Height < minDimension) {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("dimensions are %dx%d, minimum is %d", header.Width, header.Height, minDimension)}
	}
	decoded, format, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: fmt.Sprintf("image decode failed: %v", err)}
	}
	bounds := decoded.Bounds()
	digest := sha256.Sum256(body)
	return ThumbnailValidation{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		SHA256:    hex.EncodeToString(digest[:]),
		Bytes:     len(body),
		Width:     bounds.Dx(),
		Height:    bounds.Dy(),
		Format:    format,
	}, nil
}

// isKnownSourcePlaceholder ...
func isKnownSourcePlaceholder(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	base := strings.ToLower(path.Base(u.Path))
	if (host == "www.minnano-av.com" || host == "minnano-av.com") && base == "np.gif" {
		return true
	}
	return false
}
