package poster

import (
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/spf13/afero"
)

// AssetRevision is the identity of the exact poster bytes measured by the
// poster manager. It is aliased from the shared primitive so downloader and
// API layers compare the same representation without an import cycle.
type AssetRevision = assetidentity.AssetRevision

const (
	// RevisionHeader is the poster asset revision response header.
	RevisionHeader = assetidentity.RevisionHeader
	// FingerprintHeader is the poster asset SHA-256 response header.
	FingerprintHeader = assetidentity.FingerprintHeader
)

// MeasureAsset returns the SHA-256-backed identity of a poster file.
func MeasureAsset(fs afero.Fs, path string) (AssetRevision, error) {
	return assetidentity.Measure(fs, path)
}

// ValidFingerprint reports whether value is absent or a canonical SHA-256
// digest. Empty remains valid for legacy persisted crop envelopes.
func ValidFingerprint(value string) bool {
	return assetidentity.ValidFingerprint(value)
}
