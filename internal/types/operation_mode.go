package types

import (
	"fmt"
	"strings"
)

type OperationMode string

const (
	OperationModeOrganize              OperationMode = "organize"
	OperationModeInPlace               OperationMode = "in-place"
	OperationModeInPlaceNoRenameFolder OperationMode = "in-place-norenamefolder"
	OperationModeMetadataArtwork       OperationMode = "metadata-artwork"
	OperationModePreview               OperationMode = "preview"
)

func ParseOperationMode(raw string) (OperationMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case string(OperationModeOrganize):
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

func (m OperationMode) IsValid() bool {
	switch m {
	case OperationModeOrganize, OperationModeInPlace, OperationModeInPlaceNoRenameFolder, OperationModeMetadataArtwork, OperationModePreview:
		return true
	default:
		return false
	}
}

func IsValidOperationMode(mode string) bool {
	switch mode {
	case string(OperationModeOrganize), string(OperationModeInPlace), string(OperationModeInPlaceNoRenameFolder), string(OperationModeMetadataArtwork), string(OperationModePreview):
		return true
	default:
		return false
	}
}
