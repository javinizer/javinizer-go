package applyplan

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// Version1 .
const Version1 = 1

// VideoOperation .
type VideoOperation string

const (
	// VideoOperationOrganize .
	VideoOperationOrganize VideoOperation = "organize"
	// VideoOperationRenameInPlace .
	VideoOperationRenameInPlace VideoOperation = "rename-in-place"
	// VideoOperationRenameFile .
	VideoOperationRenameFile VideoOperation = "rename-file"
	// VideoOperationLeaveInPlace .
	VideoOperationLeaveInPlace VideoOperation = "leave-in-place"
	// VideoOperationMetadataArtwork .
	VideoOperationMetadataArtwork VideoOperation = "metadata-artwork"
)

// NFOOutput .
type NFOOutput string

const (
	// NFOOutputWrite .
	NFOOutputWrite NFOOutput = "write"
	// NFOOutputSkip .
	NFOOutputSkip NFOOutput = "skip"
)

// MediaPolicy .
type MediaPolicy string

const (
	// MediaPolicyMissing .
	MediaPolicyMissing MediaPolicy = "missing"
	// MediaPolicyReplace .
	MediaPolicyReplace MediaPolicy = "replace"
	// MediaPolicySkip .
	MediaPolicySkip MediaPolicy = "skip"
)

// ScalarStrategy .
type ScalarStrategy string

const (
	// ScalarPreferNFO .
	ScalarPreferNFO ScalarStrategy = "prefer-nfo"
	// ScalarPreferScraper .
	ScalarPreferScraper ScalarStrategy = "prefer-scraper"
	// ScalarPreserveExisting .
	ScalarPreserveExisting ScalarStrategy = "preserve-existing"
	// ScalarFillMissingOnly .
	ScalarFillMissingOnly ScalarStrategy = "fill-missing-only"
)

// ArrayStrategy .
type ArrayStrategy string

const (
	// ArrayMerge .
	ArrayMerge ArrayStrategy = "merge"
	// ArrayReplace .
	ArrayReplace ArrayStrategy = "replace"
)

// Preset .
type Preset string

const (
	// PresetConservative .
	PresetConservative Preset = "conservative"
	// PresetGapFill .
	PresetGapFill Preset = "gap-fill"
	// PresetAggressive .
	PresetAggressive Preset = "aggressive"
)

// MergePolicy .
type MergePolicy struct {
	ScalarStrategy ScalarStrategy `json:"scalar_strategy"`
	ArrayStrategy  ArrayStrategy  `json:"array_strategy"`
	SourcePreset   Preset         `json:"source_preset,omitempty"`
}

// Plan .
type Plan struct {
	Version        int            `json:"version"`
	VideoOperation VideoOperation `json:"video_operation"`
	Destination    string         `json:"destination,omitempty"`
	NFOOutput      NFOOutput      `json:"nfo_output"`
	MediaPolicy    MediaPolicy    `json:"media_policy"`
	Merge          *MergePolicy   `json:"merge,omitempty"`
}

// MergeOverride .
type MergeOverride string

const (
	// MergeOverrideNone .
	MergeOverrideNone MergeOverride = "none"
	// MergeOverrideForceOverwrite .
	MergeOverrideForceOverwrite MergeOverride = "force-overwrite"
	// MergeOverridePreserveNFO .
	MergeOverridePreserveNFO MergeOverride = "preserve-nfo"
)

// EffectivePlan .
type EffectivePlan struct {
	Plan          *Plan         `json:"plan"`
	MergeOverride MergeOverride `json:"merge_override"`
}

// Projection .
type Projection struct {
	Update                 bool
	OperationMode          operationmode.OperationMode
	Destination            string
	SkipNFO                bool
	SkipDownload           bool
	OverwriteExistingMedia bool
	ForceRenameFile        bool
	ForceNFO               bool
}

// Default .
func Default(operation VideoOperation, destination string) *Plan {
	plan := &Plan{
		Version:        Version1,
		VideoOperation: operation,
		Destination:    destination,
		NFOOutput:      NFOOutputWrite,
		MediaPolicy:    MediaPolicyMissing,
	}
	if operation == VideoOperationLeaveInPlace {
		plan.Merge = &MergePolicy{ScalarStrategy: ScalarPreferNFO, ArrayStrategy: ArrayMerge}
	}
	return plan
}

// PresetStrategies .
func PresetStrategies(preset Preset) (ScalarStrategy, ArrayStrategy, error) {
	switch preset {
	case PresetConservative:
		return ScalarPreserveExisting, ArrayMerge, nil
	case PresetGapFill:
		return ScalarFillMissingOnly, ArrayMerge, nil
	case PresetAggressive:
		return ScalarPreferScraper, ArrayReplace, nil
	default:
		return "", "", fmt.Errorf("unknown preset %q", preset)
	}
}

// Clone .
func Clone(plan *Plan) *Plan {
	if plan == nil {
		return nil
	}
	clone := *plan
	if plan.Merge != nil {
		merge := *plan.Merge
		clone.Merge = &merge
	}
	return &clone
}

// Normalize .
func Normalize(plan *Plan) (*Plan, error) {
	if plan == nil {
		return nil, nil
	}
	normalized := Clone(plan)
	normalized.Destination = strings.TrimSpace(normalized.Destination)
	if normalized.NFOOutput == "" {
		normalized.NFOOutput = NFOOutputWrite
	}
	if normalized.MediaPolicy == "" {
		normalized.MediaPolicy = MediaPolicyMissing
	}
	if normalized.VideoOperation != VideoOperationOrganize {
		normalized.Destination = ""
	}
	if normalized.VideoOperation == VideoOperationLeaveInPlace {
		if normalized.Merge == nil {
			normalized.Merge = &MergePolicy{ScalarStrategy: ScalarPreferNFO, ArrayStrategy: ArrayMerge}
		}
		if normalized.Merge.ScalarStrategy == "" {
			normalized.Merge.ScalarStrategy = ScalarPreferNFO
		}
		if normalized.Merge.ArrayStrategy == "" {
			normalized.Merge.ArrayStrategy = ArrayMerge
		}
		if normalized.Merge.SourcePreset != "" {
			scalar, array, err := PresetStrategies(normalized.Merge.SourcePreset)
			if err != nil {
				return nil, err
			}
			if scalar != normalized.Merge.ScalarStrategy || array != normalized.Merge.ArrayStrategy {
				return nil, fmt.Errorf("source preset %q does not match canonical strategies", normalized.Merge.SourcePreset)
			}
		}
	} else {
		normalized.Merge = nil
	}
	if err := Validate(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// Validate .
func Validate(plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("apply plan is required")
	}
	if plan.Version != Version1 {
		return fmt.Errorf("unsupported apply plan version %d", plan.Version)
	}
	switch plan.VideoOperation {
	case VideoOperationOrganize, VideoOperationRenameInPlace, VideoOperationRenameFile, VideoOperationLeaveInPlace, VideoOperationMetadataArtwork:
	default:
		return fmt.Errorf("invalid video operation %q", plan.VideoOperation)
	}
	if plan.VideoOperation == VideoOperationOrganize && strings.TrimSpace(plan.Destination) == "" {
		return fmt.Errorf("destination is required for organize")
	}
	if plan.VideoOperation != VideoOperationOrganize && strings.TrimSpace(plan.Destination) != "" {
		return fmt.Errorf("destination is not supported for %s", plan.VideoOperation)
	}
	if plan.NFOOutput != NFOOutputWrite && plan.NFOOutput != NFOOutputSkip {
		return fmt.Errorf("invalid NFO output %q", plan.NFOOutput)
	}
	switch plan.MediaPolicy {
	case MediaPolicyMissing, MediaPolicySkip:
	case MediaPolicyReplace:
		if plan.VideoOperation != VideoOperationLeaveInPlace {
			return fmt.Errorf("media replacement is only supported for leave-in-place")
		}
	default:
		return fmt.Errorf("invalid media policy %q", plan.MediaPolicy)
	}
	if (plan.VideoOperation == VideoOperationLeaveInPlace || plan.VideoOperation == VideoOperationMetadataArtwork) && plan.NFOOutput == NFOOutputSkip && plan.MediaPolicy == MediaPolicySkip {
		return fmt.Errorf("%s plan would perform no output", plan.VideoOperation)
	}
	if plan.VideoOperation == VideoOperationLeaveInPlace {
		if plan.Merge == nil {
			return fmt.Errorf("merge policy is required for leave-in-place")
		}
		switch plan.Merge.ScalarStrategy {
		case ScalarPreferNFO, ScalarPreferScraper, ScalarPreserveExisting, ScalarFillMissingOnly:
		default:
			return fmt.Errorf("invalid scalar strategy %q", plan.Merge.ScalarStrategy)
		}
		if plan.Merge.ArrayStrategy != ArrayMerge && plan.Merge.ArrayStrategy != ArrayReplace {
			return fmt.Errorf("invalid array strategy %q", plan.Merge.ArrayStrategy)
		}
	} else if plan.Merge != nil {
		return fmt.Errorf("merge policy is only supported for leave-in-place")
	}
	return nil
}

// Project .
func Project(plan *Plan) (Projection, error) {
	normalized, err := Normalize(plan)
	if err != nil {
		return Projection{}, err
	}
	projection := Projection{
		Destination:            normalized.Destination,
		SkipNFO:                normalized.NFOOutput == NFOOutputSkip,
		SkipDownload:           normalized.MediaPolicy == MediaPolicySkip,
		OverwriteExistingMedia: normalized.MediaPolicy == MediaPolicyReplace,
		ForceNFO:               normalized.NFOOutput == NFOOutputWrite,
	}
	switch normalized.VideoOperation {
	case VideoOperationOrganize:
		projection.OperationMode = operationmode.OperationModeOrganize
	case VideoOperationRenameInPlace:
		projection.OperationMode = operationmode.OperationModeInPlace
		projection.ForceRenameFile = true
	case VideoOperationRenameFile:
		projection.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
		projection.ForceRenameFile = true
	case VideoOperationLeaveInPlace:
		projection.Update = true
		projection.OperationMode = operationmode.OperationModeMetadataArtwork
	case VideoOperationMetadataArtwork:
		projection.OperationMode = operationmode.OperationModeMetadataArtwork
	}
	return projection, nil
}

// FromLegacy .
func FromLegacy(update bool, mode operationmode.OperationMode, destination string, scalar ScalarStrategy, array ArrayStrategy) (*Plan, error) {
	var operation VideoOperation
	if update {
		operation = VideoOperationLeaveInPlace
	} else {
		switch mode {
		case operationmode.OperationModeOrganize, "":
			operation = VideoOperationOrganize
		case operationmode.OperationModeInPlace:
			operation = VideoOperationRenameInPlace
		case operationmode.OperationModeInPlaceNoRenameFolder:
			operation = VideoOperationRenameFile
		case operationmode.OperationModeMetadataArtwork:
			operation = VideoOperationMetadataArtwork
		default:
			return nil, fmt.Errorf("legacy operation mode %q cannot be migrated", mode)
		}
	}
	plan := Default(operation, destination)
	if operation == VideoOperationLeaveInPlace && plan.Merge != nil {
		if scalar != "" {
			plan.Merge.ScalarStrategy = scalar
		}
		if array != "" {
			plan.Merge.ArrayStrategy = array
		}
	}
	return Normalize(plan)
}
