// Package operationmode defines the OperationMode type used across the
// application to classify file-organization strategies.  It is a leaf
// package with no internal dependencies so that any package can import
// it without creating cycles.
package operationmode

import (
	"fmt"
	"strings"

	"database/sql/driver"
	"github.com/javinizer/javinizer-go/internal/enumutil"
)

// OperationMode classifies how files should be organized.
type OperationMode string

// OperationMode values for the supported file-organization strategies.
const (
	OperationModeOrganize              OperationMode = "organize"
	OperationModeInPlace               OperationMode = "in-place"
	OperationModeInPlaceNoRenameFolder OperationMode = "in-place-norenamefolder"
	OperationModeMetadataArtwork       OperationMode = "metadata-artwork"
	OperationModePreview               OperationMode = "preview"
)

func (m OperationMode) String() string { return string(m) }

// MarshalJSON implements json.Marshaler for OperationMode.
func (m OperationMode) MarshalJSON() ([]byte, error) { return enumutil.MarshalStringEnum(string(m)) }

// UnmarshalJSON implements json.Unmarshaler for OperationMode.
func (m *OperationMode) UnmarshalJSON(b []byte) error {
	return enumutil.UnmarshalStringEnum((*string)(m), b)
}

// Scan implements sql.Scanner for OperationMode.
func (m *OperationMode) Scan(value any) error { return enumutil.ScanStringEnum((*string)(m), value) }

// Value implements driver.Valuer for OperationMode.
func (m OperationMode) Value() (driver.Value, error) { return enumutil.StringEnumValue(string(m)) }

// ParseOperationMode parses a string into an OperationMode.
func ParseOperationMode(raw string) (OperationMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", string(OperationModeOrganize):
		return OperationModeOrganize, nil
	case string(OperationModeInPlace):
		return OperationModeInPlace, nil
	case string(OperationModeInPlaceNoRenameFolder):
		return OperationModeInPlaceNoRenameFolder, nil
	case string(OperationModeMetadataArtwork):
		return OperationModeMetadataArtwork, nil
	case string(OperationModePreview):
		return OperationModePreview, nil
	default:
		return OperationMode(""), fmt.Errorf("invalid operation mode %q (expected one of: organize, in-place, in-place-norenamefolder, metadata-artwork, preview)", raw)
	}
}

// RequiresOrganize reports whether the mode performs file operations
// (move/copy/link/rename) and so needs the organize step to run.
// in-place and in-place-norenamefolder rename the file in place and MUST run
// organize; metadata-artwork does no file ops and preview is non-mutating.
func (m OperationMode) RequiresOrganize() bool {
	switch m {
	case OperationModeOrganize, OperationModeInPlace, OperationModeInPlaceNoRenameFolder:
		return true
	case OperationModeMetadataArtwork, OperationModePreview:
		return false
	default:
		return false
	}
}

// IsValid reports whether the OperationMode is a known value.
func (m OperationMode) IsValid() bool {
	switch m {
	case OperationModeOrganize, OperationModeInPlace, OperationModeInPlaceNoRenameFolder, OperationModeMetadataArtwork, OperationModePreview:
		return true
	default:
		return false
	}
}
