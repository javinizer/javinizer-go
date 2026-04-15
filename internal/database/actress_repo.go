package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type ActressRepository struct {
	db *DB
}

var (
	ErrActressMergeSameID           = errors.New("target_id and source_id must be different")
	ErrActressMergeInvalidID        = errors.New("target_id and source_id must be greater than 0")
	ErrActressMergeInvalidField     = errors.New("invalid merge field")
	ErrActressMergeInvalidDecision  = errors.New("invalid merge resolution")
	ErrActressMergeUniqueConstraint = errors.New("merge would violate unique constraints")
)

type ActressMergeConflict struct {
	Field             string      `json:"field"`
	TargetValue       interface{} `json:"target_value,omitempty"`
	SourceValue       interface{} `json:"source_value,omitempty"`
	DefaultResolution string      `json:"default_resolution"`
}

type ActressMergePreview struct {
	Target             models.Actress                  `json:"target"`
	Source             models.Actress                  `json:"source"`
	ProposedMerged     models.Actress                  `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict          `json:"conflicts"`
	DefaultResolutions map[string]string               `json:"default_resolutions"`
	ConflictByField    map[string]ActressMergeConflict `json:"-"`
}

type ActressMergeResult struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id"`
	UpdatedMovies     int            `json:"updated_movies"`
	ConflictsResolved int            `json:"conflicts_resolved"`
	AliasesAdded      int            `json:"aliases_added"`
}

func NewActressRepository(db *DB) *ActressRepository {
	return &ActressRepository{db: db}
}

func (r *ActressRepository) Create(actress *models.Actress) error {
	if err := r.db.Create(actress).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
}

func (r *ActressRepository) Update(actress *models.Actress) error {
	if err := r.db.Save(actress).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
}

func (r *ActressRepository) FindByID(id uint) (*models.Actress, error) {
	var actress models.Actress
	err := r.db.First(&actress, id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %d", id), err)
	}
	return &actress, nil
}

func (r *ActressRepository) Delete(id uint) error {
	if err := r.db.Delete(&models.Actress{}, id).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("actress %d", id), err)
	}
	return nil
}

func (r *ActressRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.Actress{}).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "actresses", err)
	}
	return count, nil
}

func (r *ActressRepository) FindByJapaneseName(name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.db.First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s", name), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindOrCreate(actress *models.Actress) error {
	if actress.JapaneseName != "" {
		existing, err := r.FindByJapaneseName(actress.JapaneseName)
		if err == nil {
			*actress = *existing
			return nil
		}
	}

	return r.Create(actress)
}

func (r *ActressRepository) List(limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress
	err := r.db.Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) ListSorted(limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	dbq := r.db.DB
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) SearchPaged(query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) SearchPagedSorted(query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"

	dbq := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) CountSearch(query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.db.Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}

func (r *ActressRepository) Search(query string) ([]models.Actress, error) {
	var actresses []models.Actress

	if query == "" {
		err := r.db.Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		if err != nil {
			return nil, wrapDBErr("find", "actresses", err)
		}
		return actresses, nil
	}

	searchPattern := "%" + query + "%"
	err := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(20).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) loadPair(targetID, sourceID uint) (*models.Actress, *models.Actress, error) {
	if targetID == 0 || sourceID == 0 {
		return nil, nil, ErrActressMergeInvalidID
	}
	if targetID == sourceID {
		return nil, nil, ErrActressMergeSameID
	}

	target, err := r.FindByID(targetID)
	if err != nil {
		return nil, nil, err
	}
	source, err := r.FindByID(sourceID)
	if err != nil {
		return nil, nil, err
	}
	return target, source, nil
}

func (r *ActressRepository) PreviewMerge(targetID, sourceID uint) (*ActressMergePreview, error) {
	target, source, err := r.loadPair(targetID, sourceID)
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
	merged.Aliases, _, _ = mergeAliasValues(target.Aliases, collectActressAliasCandidates(source), canonicalName)

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

func (r *ActressRepository) Merge(targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	preview, err := r.PreviewMerge(targetID, sourceID)
	if err != nil {
		return nil, err
	}

	normalizedResolutions, err := normalizeMergeResolutions(resolutions)
	if err != nil {
		return nil, err
	}
	for _, conflict := range preview.Conflicts {
		if _, exists := normalizedResolutions[conflict.Field]; !exists {
			normalizedResolutions[conflict.Field] = "target"
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

	updatedMovies := 0
	conflictsResolved := len(preview.Conflicts)
	err = r.db.Transaction(func(tx *gorm.DB) error {
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

		if merged.DMMID > 0 && merged.DMMID == preview.Source.DMMID && preview.Target.DMMID != preview.Source.DMMID {
			tempDMMID := -int(sourceID)
			if tempDMMID == 0 {
				tempDMMID = -1
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", sourceID).Update("dmm_id", tempDMMID).Error; err != nil {
				return wrapDBErr("update", fmt.Sprintf("merge actress %d temp dmm_id", sourceID), err)
			}
		}

		if err := tx.Model(&models.Actress{}).Where("id = ?", targetID).Updates(map[string]interface{}{
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

		if err := upsertActressAliases(tx, sourceAliasUpserts, canonicalName); err != nil {
			return wrapDBErr("merge", fmt.Sprintf("actress aliases for %s", canonicalName), err)
		}

		if err := tx.Delete(&models.Actress{}, sourceID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("merge source actress %d", sourceID), err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	mergedRecord, err := r.FindByID(targetID)
	if err != nil {
		return nil, err
	}

	return &ActressMergeResult{
		MergedActress:     *mergedRecord,
		MergedFromID:      sourceID,
		UpdatedMovies:     updatedMovies,
		ConflictsResolved: conflictsResolved,
		AliasesAdded:      aliasesAdded,
	}, nil
}
