package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MergeResolutionTarget = "target"
	MergeResolutionSource = "source"
	resolutionTarget      = "target"
)

var (
	ErrActressMergeSameID           = errors.New("target_id and source_id must be different")
	ErrActressMergeInvalidID        = errors.New("target_id and source_id must be greater than 0")
	ErrActressMergeUniqueConstraint = errors.New("merge would violate unique constraints")
)

// ActressMergeConflict describes a single field conflict between two actresses being merged.
type ActressMergeConflict struct {
	Field             string `json:"field"`
	TargetValue       any    `json:"target_value,omitempty"`
	SourceValue       any    `json:"source_value,omitempty"`
	DefaultResolution string `json:"default_resolution"`
}

// ActressMergePreview holds the preview of a merge operation before execution.
type ActressMergePreview struct {
	Target             models.Actress                  `json:"target"`
	Source             models.Actress                  `json:"source"`
	ProposedMerged     models.Actress                  `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict          `json:"conflicts"`
	DefaultResolutions map[string]string               `json:"default_resolutions"`
	ConflictByField    map[string]ActressMergeConflict `json:"-"`
}

// ActressMergeResult holds the result of a completed merge operation.
type ActressMergeResult struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id"`
	UpdatedMovies     int            `json:"updated_movies"`
	ConflictsResolved int            `json:"conflicts_resolved"`
	AliasesAdded      int            `json:"aliases_added"`
}

// MergePlan captures the computed merge state: merged values, conflict resolutions,
// and alias candidates. It is produced by PlanMerge and consumed by ExecuteMerge,
// separating the "what to merge" decision from the "how to execute" side effect.
type MergePlan struct {
	TargetID           uint
	SourceID           uint
	Merged             models.Actress
	CanonicalName      string
	AliasesAdded       int
	SourceAliasUpserts []string
	ConflictsResolved  int
}

// actressMerger handles actress merge operations, extracted from ActressRepository
// to keep the repository focused on CRUD and the merge logic independently testable.
type actressMerger struct {
	repo ActressRepositoryInterface
}

// moveMovieAssociations moves movie associations from source actress to target actress.
// Returns count of updated movies. Uses the provided transaction.
func moveMovieAssociations(tx *gorm.DB, sourceID, targetID uint) (int, error) {
	// Use the join table to find only movies that reference the source actress,
	// avoiding a full-table scan that loads every movie into memory.
	var movieContentIDs []string
	if err := tx.Model(&models.Movie{}).
		Select("movies.content_id").
		Joins("JOIN movie_actresses ON movie_actresses.movie_content_id = movies.content_id").
		Where("movie_actresses.actress_id = ?", sourceID).
		Pluck("content_id", &movieContentIDs).Error; err != nil {
		return 0, err
	}
	if len(movieContentIDs) == 0 {
		return 0, nil
	}

	var movies []models.Movie
	if err := tx.Preload("Actresses").Where("content_id IN ?", movieContentIDs).Find(&movies).Error; err != nil {
		return 0, err
	}

	updatedMovies := 0
	for _, movie := range movies {
		hasSource := false
		hasTarget := false
		nextActresses := make([]models.Actress, 0, len(movie.Actresses)+1)

		for _, actress := range movie.Actresses {
			switch actress.ID {
			case sourceID:
				hasSource = true
				if !hasTarget {
					nextActresses = append(nextActresses, models.Actress{ID: targetID})
					hasTarget = true
				}
			case targetID:
				if !hasTarget {
					nextActresses = append(nextActresses, actress)
					hasTarget = true
				}
			default:
				nextActresses = append(nextActresses, actress)
			}
		}

		if !hasSource {
			continue
		}
		if !hasTarget {
			nextActresses = append(nextActresses, models.Actress{ID: targetID})
		}

		stub := models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(&stub).Association("Actresses").Replace(nextActresses); err != nil {
			return updatedMovies, err
		}
		updatedMovies++
	}

	return updatedMovies, nil
}

// upsertActressAliases creates or updates actress alias records.
// Uses ON CONFLICT to handle duplicates. Uses the provided transaction.
func upsertActressAliases(tx *gorm.DB, aliases []string, canonicalName string) error {
	canonicalName = strings.TrimSpace(canonicalName)
	canonicalKey := strings.ToLower(canonicalName)
	if canonicalName == "" {
		return nil
	}

	seen := make(map[string]bool)
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		key := strings.ToLower(alias)
		if alias == "" || key == canonicalKey || seen[key] {
			continue
		}
		seen[key] = true

		entry := models.ActressAlias{
			AliasName:     alias,
			CanonicalName: canonicalName,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "alias_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"canonical_name", "updated_at"}),
		}).Create(&entry).Error; err != nil {
			return err
		}
	}

	return nil
}

// loadPair loads and validates a pair of actresses for merge operations.
func (m *actressMerger) loadPair(ctx context.Context, targetID, sourceID uint) (*models.Actress, *models.Actress, error) {
	if targetID == 0 || sourceID == 0 {
		return nil, nil, ErrActressMergeInvalidID
	}
	if targetID == sourceID {
		return nil, nil, ErrActressMergeSameID
	}

	target, err := m.repo.FindByID(ctx, targetID)
	if err != nil {
		return nil, nil, err
	}
	source, err := m.repo.FindByID(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}
	return target, source, nil
}

// PreviewMerge previews a merge between two actresses without modifying the database.
func (m *actressMerger) PreviewMerge(ctx context.Context, targetID, sourceID uint) (*ActressMergePreview, error) {
	target, source, err := m.loadPair(ctx, targetID, sourceID)
	if err != nil {
		return nil, err
	}

	conflicts := buildActressMergeConflicts(target, source)
	defaultResolutions := defaultResolutionsFromConflicts(conflicts)
	merged, err := mergeActressValues(target, source, defaultResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	merged.Aliases, _, _ = mergeAliasValues(target.Aliases, collectActressAliasCandidates(source), canonicalName) // 3rd return (added aliases) not needed at call site

	byField := make(map[string]ActressMergeConflict, len(conflicts))
	for _, conflict := range conflicts {
		byField[conflict.Field] = conflict
	}

	return &ActressMergePreview{
		Target:             *target,
		Source:             *source,
		ProposedMerged:     merged,
		Conflicts:          conflicts,
		DefaultResolutions: defaultResolutions,
		ConflictByField:    byField,
	}, nil
}

// PlanMerge computes a MergePlan by resolving field conflicts and alias candidates
// without touching the database. It returns the plan ready for ExecuteMerge.
func (m *actressMerger) PlanMerge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string) (*MergePlan, error) {
	preview, err := m.PreviewMerge(ctx, targetID, sourceID)
	if err != nil {
		return nil, err
	}

	normalizedResolutions, err := normalizeMergeResolutions(resolutions)
	if err != nil {
		return nil, err
	}
	for _, conflict := range preview.Conflicts {
		if _, exists := normalizedResolutions[conflict.Field]; !exists {
			normalizedResolutions[conflict.Field] = resolutionTarget
		}
	}

	merged, err := mergeActressValues(&preview.Target, &preview.Source, normalizedResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	aliasesAdded := 0
	sourceCandidates := collectActressAliasCandidates(&preview.Source)
	merged.Aliases, aliasesAdded, _ = mergeAliasValues(
		preview.Target.Aliases,
		sourceCandidates,
		canonicalName,
	)
	sourceAliasUpserts := sourceAliasesForUpsert(sourceCandidates, canonicalName)

	return &MergePlan{
		TargetID:           targetID,
		SourceID:           sourceID,
		Merged:             merged,
		CanonicalName:      canonicalName,
		AliasesAdded:       aliasesAdded,
		SourceAliasUpserts: sourceAliasUpserts,
		ConflictsResolved:  len(preview.Conflicts),
	}, nil
}

// ExecuteMerge applies a precomputed MergePlan to the database within a transaction.
// It performs the actual row updates, association moves, alias upserts, and source deletion.
// The db parameter provides the database connection for the transaction boundary.
func (m *actressMerger) ExecuteMerge(ctx context.Context, plan *MergePlan, db *DB) (*ActressMergeResult, error) {
	targetID := plan.TargetID
	sourceID := plan.SourceID
	merged := plan.Merged

	updatedMovies := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if merged.DMMID > 0 {
			var existing models.Actress
			checkErr := tx.Where("dmm_id = ? AND id NOT IN ?", merged.DMMID, []uint{targetID, sourceID}).First(&existing).Error
			if checkErr == nil {
				return fmt.Errorf("%w: dmm_id %d is already used by actress #%d", ErrActressMergeUniqueConstraint, merged.DMMID, existing.ID)
			}
			if checkErr != nil && !errors.Is(checkErr, gorm.ErrRecordNotFound) {
				return wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d for merge", merged.DMMID), checkErr)
			}
		}

		// Load source to check whether DMMID swap is needed
		source, err := m.repo.FindByID(ctx, sourceID)
		if err != nil {
			return err
		}
		if merged.DMMID > 0 && merged.DMMID == source.DMMID {
			target, err := m.repo.FindByID(ctx, targetID)
			if err != nil {
				return err
			}
			if target.DMMID != source.DMMID {
				tempDMMID := -int(sourceID)
				if tempDMMID == 0 {
					tempDMMID = -1
				}
				if err := tx.Model(&models.Actress{}).Where("id = ?", sourceID).Update("dmm_id", tempDMMID).Error; err != nil {
					return wrapDBErr("update", fmt.Sprintf("merge actress %d temp dmm_id", sourceID), err)
				}
			}
		}

		if err := tx.Model(&models.Actress{}).Where("id = ?", targetID).Updates(map[string]any{
			"dmm_id":        merged.DMMID,
			"first_name":    merged.FirstName,
			"last_name":     merged.LastName,
			"japanese_name": merged.JapaneseName,
			"thumb_url":     merged.ThumbURL,
			"aliases":       merged.Aliases,
			"updated_at":    time.Now().UTC(),
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrActressMergeUniqueConstraint
			}
			return wrapDBErr("update", fmt.Sprintf("merge actress %d", targetID), err)
		}

		var moveErr error
		updatedMovies, moveErr = moveMovieAssociations(tx, sourceID, targetID)
		if moveErr != nil {
			return wrapDBErr("merge", fmt.Sprintf("actress movie associations from %d to %d", sourceID, targetID), moveErr)
		}

		if err := upsertActressAliases(tx, plan.SourceAliasUpserts, plan.CanonicalName); err != nil {
			return wrapDBErr("merge", fmt.Sprintf("actress aliases for %s", plan.CanonicalName), err)
		}

		if err := tx.Delete(&models.Actress{}, sourceID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("merge source actress %d", sourceID), err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	mergedRecord, err := m.repo.FindByID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	return &ActressMergeResult{
		MergedActress:     *mergedRecord,
		MergedFromID:      sourceID,
		UpdatedMovies:     updatedMovies,
		ConflictsResolved: plan.ConflictsResolved,
		AliasesAdded:      plan.AliasesAdded,
	}, nil
}

// Merge computes a merge plan and executes it in one call.
// For finer control, use PlanMerge + ExecuteMerge separately.
func (m *actressMerger) Merge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string, db *DB) (*ActressMergeResult, error) {
	plan, err := m.PlanMerge(ctx, targetID, sourceID, resolutions)
	if err != nil {
		return nil, err
	}
	return m.ExecuteMerge(ctx, plan, db)
}
