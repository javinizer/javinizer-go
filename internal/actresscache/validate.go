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
	"github.com/javinizer/javinizer-go/internal/ssrf"
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
// buffer, so the header dimensions are checked before decoding. 20 MP covers
// real actress photos (≈4600×4600 or 5472×3648) comfortably while keeping a
// decoded RGBA frame ≈80MB, safe with parallel validation workers.
const MaxThumbnailPixels = 20_000_000

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
	// Advertise exactly the formats with registered decoders: no image/*
	// wildcard (it re-advertises AVIF at full quality), no SVG (no decoder).
	// Anything else a server serves fails q-value negotiation or DecodeConfig.
	body, headers, err := fetcher.Get(ctx, rawURL, "image/webp,image/apng,image/png,image/jpeg,image/gif", maxBytes)
	if err != nil {
		// statusErr ...
		var statusErr *HTTPError
		if errors.As(err, &statusErr) {
			if statusErr.IsTransient() {
				return ThumbnailValidation{}, err
			}
			// Keep the typed HTTPError in the chain: classification inspects
			// the status code (e.g. r18dev fabricated-thumb 404s are transient).
			return ThumbnailValidation{}, fmt.Errorf("%w: %w", &ThumbnailRejectedError{Reason: statusErr.Error()}, err)
		}
		var unverifiable *ssrf.UnverifiableHostError
		if errors.As(err, &unverifiable) {
			// Transient: DNS could not prove the host public. Record "failed",
			// never a permanent "rejected".
			return ThumbnailValidation{}, err
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
	// Overflow-safe: declared dimensions on wide containers can reach int32
	// scale, and multiplying pairs of them could push past int64 if a decoder
	// ever reports broader ranges; compare by division instead.
	if int64(header.Width) > MaxThumbnailPixels/int64(header.Height) {
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
