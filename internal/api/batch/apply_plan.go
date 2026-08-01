package batch

import (
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ErrApplyEndpointConflict .
var ErrApplyEndpointConflict = errors.New("apply endpoint conflicts with persisted plan")

type planMirrors struct {
	destinationPresent bool
	operationPresent   bool
	presetPresent      bool
	scalarPresent      bool
	arrayPresent       bool
	updatePresent      bool
}

func normalizeScrapePlan(input StartScrapeInput) (*applyplan.Plan, applyplan.Projection, error) {
	if input.ApplyPlan == nil {
		return nil, applyplan.Projection{}, nil
	}
	plan, err := applyplan.Normalize(input.ApplyPlan)
	if err != nil {
		return nil, applyplan.Projection{}, err
	}
	projection, _ := applyplan.Project(plan)
	m := input.MirrorPresence
	if m.updatePresent && input.Update != nil && *input.Update != projection.Update {
		return nil, applyplan.Projection{}, fmt.Errorf("update contradicts apply plan")
	}
	if m.destinationPresent && plan.VideoOperation == applyplan.VideoOperationOrganize && strings.TrimSpace(input.Destination) != projection.Destination {
		return nil, applyplan.Projection{}, fmt.Errorf("destination contradicts apply plan")
	}
	if m.operationPresent {
		resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{OperationMode: input.OperationMode})
		if resolveErr != nil {
			return nil, applyplan.Projection{}, resolveErr
		}
		if resolved.OperationMode != projection.OperationMode {
			return nil, applyplan.Projection{}, fmt.Errorf("operation_mode contradicts apply plan")
		}
	}
	if plan.Merge != nil {
		if m.presetPresent {
			resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{Preset: input.Preset})
			if resolveErr != nil {
				return nil, applyplan.Projection{}, resolveErr
			}
			if string(resolved.ScalarStrategy) != string(plan.Merge.ScalarStrategy) || arrayName(resolved.ArrayStrategy) != string(plan.Merge.ArrayStrategy) {
				return nil, applyplan.Projection{}, fmt.Errorf("preset contradicts apply plan")
			}
		} else {
			if m.scalarPresent {
				resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{ScalarStrategy: input.ScalarStrategy})
				if resolveErr != nil {
					return nil, applyplan.Projection{}, resolveErr
				}
				if string(resolved.ScalarStrategy) != string(plan.Merge.ScalarStrategy) {
					return nil, applyplan.Projection{}, fmt.Errorf("scalar_strategy contradicts apply plan")
				}
			}
			if m.arrayPresent {
				resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{ArrayStrategy: input.ArrayStrategy})
				if resolveErr != nil {
					return nil, applyplan.Projection{}, resolveErr
				}
				if arrayName(resolved.ArrayStrategy) != string(plan.Merge.ArrayStrategy) {
					return nil, applyplan.Projection{}, fmt.Errorf("array_strategy contradicts apply plan")
				}
			}
		}
	}
	return plan, projection, nil
}

func arrayName(merge bool) string {
	if merge {
		return string(applyplan.ArrayMerge)
	}
	return string(applyplan.ArrayReplace)
}

func legacyMergeOptions(plan *applyplan.Plan) workflow.MergeOptions {
	if plan == nil || plan.Merge == nil {
		return workflow.MergeOptions{}
	}
	return workflow.MergeOptions{
		ScalarStrategy: nfo.MergeStrategy(plan.Merge.ScalarStrategy),
		ArrayStrategy:  plan.Merge.ArrayStrategy == applyplan.ArrayMerge,
	}
}

func cloneReviewOverrides(value *contracts.ReviewApplyOverrides) *contracts.ReviewApplyOverrides {
	if value == nil {
		return &contracts.ReviewApplyOverrides{}
	}
	clone := *value
	return &clone
}

func mergeStringOverride(field string, target **string, present bool, value string) error {
	if !present {
		return nil
	}
	if *target != nil && **target != value {
		return fmt.Errorf("%s conflicts with overrides.%s", field, field)
	}
	copyValue := value
	*target = &copyValue
	return nil
}

func mergeBoolOverride(field string, target **bool, present bool, value bool) error {
	if !present {
		return nil
	}
	if *target != nil && **target != value {
		return fmt.Errorf("%s conflicts with overrides.%s", field, field)
	}
	copyValue := value
	*target = &copyValue
	return nil
}

func reviewOverridesForUpdate(req contracts.UpdateRequest) (*contracts.ReviewApplyOverrides, error) {
	result := cloneReviewOverrides(req.Overrides)
	checks := []error{
		mergeBoolOverride("force_overwrite", &result.ForceOverwrite, req.Has("force_overwrite"), req.ForceOverwrite),
		mergeBoolOverride("overwrite_existing_media", &result.OverwriteExistingMedia, req.Has("overwrite_existing_media"), req.OverwriteExistingMedia),
		mergeBoolOverride("preserve_nfo", &result.PreserveNFO, req.Has("preserve_nfo"), req.PreserveNFO),
		mergeStringOverride("preset", &result.Preset, req.Has("preset"), req.Preset),
		mergeStringOverride("scalar_strategy", &result.ScalarStrategy, req.Has("scalar_strategy"), req.ScalarStrategy),
		mergeStringOverride("array_strategy", &result.ArrayStrategy, req.Has("array_strategy"), req.ArrayStrategy),
		mergeBoolOverride("skip_nfo", &result.SkipNFO, req.Has("skip_nfo"), req.SkipNFO),
		mergeBoolOverride("skip_download", &result.SkipDownload, req.Has("skip_download"), req.SkipDownload),
	}
	for _, err := range checks {
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func reviewOverridesForOrganize(req contracts.OrganizeRequest) (*contracts.ReviewApplyOverrides, error) {
	result := cloneReviewOverrides(req.Overrides)
	checks := []error{
		mergeStringOverride("operation_mode", &result.OperationMode, req.Has("operation_mode"), req.OperationMode),
		mergeStringOverride("destination", &result.Destination, req.Has("destination"), req.Destination),
		mergeBoolOverride("skip_nfo", &result.SkipNFO, req.Has("skip_nfo"), req.SkipNFO),
		mergeBoolOverride("skip_download", &result.SkipDownload, req.Has("skip_download"), req.SkipDownload),
	}
	for _, err := range checks {
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func reviewOverridesForPreview(req contracts.OrganizePreviewRequest) (*contracts.ReviewApplyOverrides, error) {
	result := cloneReviewOverrides(req.Overrides)
	checks := []error{
		mergeStringOverride("operation_mode", &result.OperationMode, req.Has("operation_mode"), req.OperationMode),
		mergeStringOverride("destination", &result.Destination, req.Has("destination"), req.Destination),
		mergeBoolOverride("skip_nfo", &result.SkipNFO, req.Has("skip_nfo"), req.SkipNFO),
		mergeBoolOverride("skip_download", &result.SkipDownload, req.Has("skip_download"), req.SkipDownload),
	}
	for _, err := range checks {
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
func effectiveFromOverrides(base *applyplan.Plan, overrides *contracts.ReviewApplyOverrides) (*applyplan.EffectivePlan, error) {
	plan := applyplan.Clone(base)
	if plan == nil {
		return nil, nil
	}
	mergeOverride := applyplan.MergeOverrideNone
	if overrides == nil {
		return &applyplan.EffectivePlan{Plan: plan, MergeOverride: mergeOverride}, nil
	}
	if plan.VideoOperation != applyplan.VideoOperationLeaveInPlace {
		force := overrides.ForceOverwrite != nil && *overrides.ForceOverwrite
		preserve := overrides.PreserveNFO != nil && *overrides.PreserveNFO
		if overrides.Preset != nil || overrides.ScalarStrategy != nil || overrides.ArrayStrategy != nil || force || preserve {
			return nil, fmt.Errorf("merge overrides are supported only for leave-in-place")
		}
	}
	projection, err := applyplan.Project(plan)
	if err != nil {
		return nil, err
	}
	if overrides.OperationMode != nil {
		resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{OperationMode: *overrides.OperationMode})
		if resolveErr != nil {
			return nil, resolveErr
		}
		if resolved.OperationMode != projection.OperationMode {
			return nil, fmt.Errorf("operation_mode contradicts persisted apply plan")
		}
	}
	if overrides.Destination != nil {
		if plan.VideoOperation == applyplan.VideoOperationOrganize {
			plan.Destination = strings.TrimSpace(*overrides.Destination)
		} else {
			plan.Destination = ""
		}
	}
	if overrides.SkipNFO != nil {
		if *overrides.SkipNFO {
			plan.NFOOutput = applyplan.NFOOutputSkip
		} else {
			plan.NFOOutput = applyplan.NFOOutputWrite
		}
	}
	skipDownload := projection.SkipDownload
	overwrite := projection.OverwriteExistingMedia
	if overrides.SkipDownload != nil {
		skipDownload = *overrides.SkipDownload
	}
	if overrides.OverwriteExistingMedia != nil {
		overwrite = *overrides.OverwriteExistingMedia
	}
	if skipDownload && overwrite {
		return nil, fmt.Errorf("skip_download and overwrite_existing_media cannot both be true")
	}
	switch {
	case skipDownload:
		plan.MediaPolicy = applyplan.MediaPolicySkip
	case overwrite:
		plan.MediaPolicy = applyplan.MediaPolicyReplace
	default:
		plan.MediaPolicy = applyplan.MediaPolicyMissing
	}
	if plan.Merge != nil {
		presetPresent := overrides.Preset != nil && strings.TrimSpace(*overrides.Preset) != ""
		if overrides.Preset != nil && !presetPresent {
			plan.Merge.ScalarStrategy = applyplan.ScalarPreferNFO
			plan.Merge.ArrayStrategy = applyplan.ArrayMerge
			plan.Merge.SourcePreset = ""
		}
		if presetPresent {
			preset := applyplan.Preset(strings.TrimSpace(*overrides.Preset))
			scalar, array, presetErr := applyplan.PresetStrategies(preset)
			if presetErr != nil {
				return nil, presetErr
			}
			if overrides.ScalarStrategy != nil && applyplan.ScalarStrategy(*overrides.ScalarStrategy) != scalar {
				return nil, fmt.Errorf("preset contradicts scalar_strategy")
			}
			if overrides.ArrayStrategy != nil && applyplan.ArrayStrategy(*overrides.ArrayStrategy) != array {
				return nil, fmt.Errorf("preset contradicts array_strategy")
			}
			plan.Merge.ScalarStrategy = scalar
			plan.Merge.ArrayStrategy = array
			plan.Merge.SourcePreset = preset
		} else if overrides.ScalarStrategy != nil || overrides.ArrayStrategy != nil {
			if overrides.ScalarStrategy != nil {
				plan.Merge.ScalarStrategy = applyplan.ScalarStrategy(*overrides.ScalarStrategy)
			}
			if overrides.ArrayStrategy != nil {
				plan.Merge.ArrayStrategy = applyplan.ArrayStrategy(*overrides.ArrayStrategy)
			}
			plan.Merge.SourcePreset = ""
		}
	}
	force := overrides.ForceOverwrite != nil && *overrides.ForceOverwrite
	preserve := overrides.PreserveNFO != nil && *overrides.PreserveNFO
	if force && preserve {
		return nil, fmt.Errorf("force_overwrite and preserve_nfo cannot both be true")
	}
	if force {
		mergeOverride = applyplan.MergeOverrideForceOverwrite
	} else if preserve {
		mergeOverride = applyplan.MergeOverridePreserveNFO
	}
	normalized, err := applyplan.Normalize(plan)
	if err != nil {
		return nil, err
	}
	return &applyplan.EffectivePlan{Plan: normalized, MergeOverride: mergeOverride}, nil
}
