// Package assetidentity computes stable identities for bytes served by the
// review pipeline. The fingerprint is the authoritative content identity;
// Revision is a compact, deterministic token derived from the same digest so
// clients can cheaply detect that the measured asset changed.
package assetidentity

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	// RevisionHeader carries the compact JSON-safe asset revision token.
	RevisionHeader = "X-Poster-Revision"
	// FingerprintHeader carries the full SHA-256 content identity.
	FingerprintHeader = "X-Poster-Fingerprint"
	// FingerprintBytes is the raw SHA-256 digest size.
	FingerprintBytes = sha256.Size
)

// AssetRevision identifies the exact bytes measured for a poster asset.
// Fingerprint is a lower-case SHA-256 hex digest. Revision is derived from
// that digest and is therefore stable for identical bytes and different for
// overwhelmingly likely content changes.
type AssetRevision struct {
	Revision    uint64
	Fingerprint string
	Size        int64
}

// FromBytes returns the identity of body without retaining the body.
func FromBytes(body []byte) AssetRevision {
	digest := sha256.Sum256(body)
	return AssetRevision{
		Revision:    safeRevision(digest[:]),
		Fingerprint: hex.EncodeToString(digest[:]),
		Size:        int64(len(body)),
	}
}

// Measure hashes a file through the supplied afero filesystem.
func Measure(fs afero.Fs, path string) (AssetRevision, error) {
	if fs == nil {
		return AssetRevision{}, fmt.Errorf("asset identity: nil filesystem")
	}
	f, err := fs.Open(path)
	if err != nil {
		return AssetRevision{}, err
	}
	defer func() { _ = f.Close() }()
	return MeasureReader(f)
}

// MeasureReader hashes all bytes read from r.
func MeasureReader(r io.Reader) (AssetRevision, error) {
	if r == nil {
		return AssetRevision{}, fmt.Errorf("asset identity: nil reader")
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return AssetRevision{}, err
	}
	digest := h.Sum(nil)
	return AssetRevision{
		Revision:    safeRevision(digest),
		Fingerprint: hex.EncodeToString(digest),
		Size:        n,
	}, nil
}

// safeRevision keeps the compact token within JavaScript's exact integer
// range because the browser carries it as a JSON number. The fingerprint
// remains the authoritative full-width identity.
func safeRevision(digest []byte) uint64 {
	return binary.BigEndian.Uint64(digest[:8]) & ((uint64(1) << 53) - 1)
}

// ValidFingerprint accepts only the canonical 64-character SHA-256 form.
// Empty is intentionally treated as absent for legacy envelopes.
func ValidFingerprint(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// NormalizeFingerprint validates and canonicalizes a client-supplied digest.
func NormalizeFingerprint(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !ValidFingerprint(value) || value == "" {
		return "", fmt.Errorf("poster fingerprint must be a 64-character SHA-256 hex digest")
	}
	return value, nil
}

// SetHeaders writes the identity headers used by both browser image requests
// and the crop controller's HEAD probe.
func SetHeaders(w http.ResponseWriter, identity AssetRevision) {
	if w == nil || identity.Fingerprint == "" {
		return
	}
	w.Header().Set(RevisionHeader, strconv.FormatUint(identity.Revision, 10))
	w.Header().Set(FingerprintHeader, identity.Fingerprint)
}

// Matches reports whether an expected identity describes the measured bytes.
// A zero expected revision is allowed because the digest-derived token may
// legitimately be zero; callers decide whether the field was present.
func Matches(actual AssetRevision, expectedRevision uint64, expectedFingerprint string) bool {
	return actual.Revision == expectedRevision &&
		strings.EqualFold(actual.Fingerprint, strings.TrimSpace(expectedFingerprint))
}
