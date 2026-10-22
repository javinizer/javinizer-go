package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Merge policies affect only the five resolvable scalar fields, not identity,
// aliases, credits, provenance, verification, quarantine or associations.
const (
	MergePolicyFillEmpty      = "fill_empty"
	MergePolicyPreserveTarget = "preserve_target"
)

var ErrActressMergeInvalidPolicy = errors.New("invalid merge policy")

// ActressMergeOptions leaves omitted fields to Policy. Explicit decisions win,
// including empty strings and zero identifiers. Empty Policy is legacy fill_empty.
type ActressMergeOptions struct {
	Policy      string
	Resolutions map[string]string
}

// ActressMergeField includes blanks and the actual proposed scalar result.
// DefaultResolution describes the policy alone, before explicit overrides.
type ActressMergeField struct {
	Field             string `json:"field"`
	TargetValue       any    `json:"target_value"`
	SourceValue       any    `json:"source_value"`
	DefaultResolution string `json:"default_resolution"`
	ResultValue       any    `json:"result_value"`
}

func normalizeActressMergeOptions(options ActressMergeOptions) (mergeDecisions, error) {
	policy := strings.ToLower(strings.TrimSpace(options.Policy))
	if policy == "" {
		policy = MergePolicyFillEmpty
	}
	if policy != MergePolicyFillEmpty && policy != MergePolicyPreserveTarget {
		return mergeDecisions{}, fmt.Errorf("%w: %s", ErrActressMergeInvalidPolicy, options.Policy)
	}
	resolutions, err := normalizeMergeResolutions(options.Resolutions)
	if err != nil {
		return mergeDecisions{}, err
	}
	decisions := mergeDecisionsFromNormalized(resolutions)
	decisions.policy = policy
	return decisions, nil
}

func actressMergeFields(target, source, merged *models.Actress, policy string) []ActressMergeField {
	fields := []ActressMergeField{
		{Field: colDMMID, TargetValue: target.DMMID, SourceValue: source.DMMID, ResultValue: merged.DMMID},
		{Field: colFirstName, TargetValue: target.FirstName, SourceValue: source.FirstName, ResultValue: merged.FirstName},
		{Field: colLastName, TargetValue: target.LastName, SourceValue: source.LastName, ResultValue: merged.LastName},
		{Field: colJapaneseName, TargetValue: target.JapaneseName, SourceValue: source.JapaneseName, ResultValue: merged.JapaneseName},
		{Field: "thumb_url", TargetValue: target.ThumbURL, SourceValue: source.ThumbURL, ResultValue: merged.ThumbURL},
	}
	for i := range fields {
		field := &fields[i]
		field.DefaultResolution = MergeResolutionTarget
		if policy == MergePolicyPreserveTarget {
			continue
		}
		if field.Field == colDMMID {
			if target.DMMID == 0 && source.DMMID > 0 {
				field.DefaultResolution = MergeResolutionSource
			}
		} else if !nonEmptyString(field.TargetValue.(string)) && nonEmptyString(field.SourceValue.(string)) {
			field.DefaultResolution = MergeResolutionSource
		}
	}
	return fields
}

// PreviewMergeWithOptions previews the same options execution will recompute
// against transaction-current rows. It never persists the preview's values.
func (r *ActressRepository) PreviewMergeWithOptions(ctx context.Context, targetID, sourceID uint, options ActressMergeOptions) (*ActressMergePreview, error) {
	return r.merger.previewWithOptions(ctx, targetID, sourceID, options)
}

// MergeWithOptions preserves the existing atomic transaction and identity guards.
func (r *ActressRepository) MergeWithOptions(ctx context.Context, targetID, sourceID uint, options ActressMergeOptions) (*ActressMergeResult, error) {
	plan, err := r.merger.planWithOptions(ctx, targetID, sourceID, options)
	if err != nil {
		return nil, err
	}
	return r.merger.ExecuteMerge(ctx, plan, r.GetDB())
}

type actressMergeOptionsRepository interface {
	PreviewMergeWithOptions(context.Context, uint, uint, ActressMergeOptions) (*ActressMergePreview, error)
	MergeWithOptions(context.Context, uint, uint, ActressMergeOptions) (*ActressMergeResult, error)
}

// PreviewActressMerge adapts repositories without breaking legacy implementers.
func PreviewActressMerge(ctx context.Context, repo ActressRepositoryInterface, targetID, sourceID uint, options ActressMergeOptions) (*ActressMergePreview, error) {
	if _, err := normalizeActressMergeOptions(options); err != nil {
		return nil, err
	}
	if extended, ok := repo.(actressMergeOptionsRepository); ok {
		return extended.PreviewMergeWithOptions(ctx, targetID, sourceID, options)
	}
	if options.Policy != "" || len(options.Resolutions) != 0 {
		return nil, fmt.Errorf("repository does not support merge options")
	}
	return repo.PreviewMerge(ctx, targetID, sourceID)
}

// ApplyActressMerge adapts repositories without silently ignoring explicit options.
func ApplyActressMerge(ctx context.Context, repo ActressRepositoryInterface, targetID, sourceID uint, options ActressMergeOptions) (*ActressMergeResult, error) {
	if _, err := normalizeActressMergeOptions(options); err != nil {
		return nil, err
	}
	if extended, ok := repo.(actressMergeOptionsRepository); ok {
		return extended.MergeWithOptions(ctx, targetID, sourceID, options)
	}
	if options.Policy != "" {
		return nil, fmt.Errorf("repository does not support merge options")
	}
	return repo.Merge(ctx, targetID, sourceID, options.Resolutions)
}
