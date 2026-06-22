package workflow

import (
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// downloadToggles controls which media types to include in the preview.
// Decomposed from the former previewConfig flat struct — each toggle is an
// explicit field so callers see exactly what the preview orchestrator needs.
type downloadToggles struct {
	Poster      bool
	Cover       bool
	Extrafanart bool
	Trailer     bool
}

// PreviewPathConfig holds only the organizer fields the preview orchestrator
// reads for path computation. Per DEEP-5: narrows the preview orchestrator's
// interface surface from the full 30+ field organizer.Config to the ~10 fields
// preview actually reads. Organizer config additions that don't affect preview
// path computation stop at the factory boundary.
type PreviewPathConfig struct {
	organizer.MediaFormatConfig
}

// StrategyResolverFunc creates an OperationStrategy for the given operation mode.
// The preview orchestrator delegates strategy creation to this function instead
// of carrying the full *organizer.Config and calling ResolveStrategy directly.
// This decouples preview from organizer config changes that don't affect path
// computation.
type StrategyResolverFunc func(operationMode operationmode.OperationMode) organizer.OperationStrategy

// PreviewConfig groups the workflow-level configuration fields that the
// preview orchestrator consumes. Extracted from the flat fields of
// workflowFactoryConfig per W3-A: this reduces the parameter count of
// newPreviewOrchestrator from 12 to 6 and makes the relationship between
// fields explicit — callers see one coherent config block instead of
// seven scattered booleans, strings, and ints.
//
// Per DEEP-5: OrganizeCfg was replaced by PathCfg (PreviewPathConfig) and
// ResolveStrategy (StrategyResolverFunc). PathCfg contains only the media
// format fields the preview orchestrator reads for path computation. The
// strategy resolver function decouples preview from the full organizer.Config
// — organizer additions that don't affect preview stop at the factory boundary.
type PreviewConfig struct {
	PathCfg         PreviewPathConfig
	ResolveStrategy StrategyResolverFunc
	NFOEnabled      bool
	NFOPerFile      bool
	DisplayTitle    string
	OpMode          operationmode.OperationMode
	MaxPathLength   int
	Downloads       downloadToggles
}

// ApplyConfig groups the workflow-level configuration fields that the
// apply orchestrator consumes. Extracted from workflowFactoryConfig per
// W3-A: this reduces the parameter count of newApplyOrchestrator from
// 10 to 8 by bundling the config-like fields, and keeps NFO naming
// concerns together.
type ApplyConfig struct {
	NFONameCfg   nfo.NFONameConfig
	DisplayTitle string
}
