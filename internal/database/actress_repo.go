package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressRepository persists and queries actress records, providing CRUD,
// lookup, search, and merge operations on top of BaseRepository.
type ActressRepository struct {
	*BaseRepository[models.Actress, uint]
	merger *actressMerger
}

// NewActressRepository constructs an ActressRepository backed by the given DB
// with the default sort order for listing actresses.
func NewActressRepository(db *DB) *ActressRepository {
	repo := &ActressRepository{
		BaseRepository: NewBaseRepository[models.Actress, uint](
			db, "actress",
			func(a models.Actress) string { return fmt.Sprintf("%d", a.ID) },
			withDefaultOrder[models.Actress, uint]("japanese_name ASC, last_name ASC, first_name ASC, id ASC"),
			WithNewEntity[models.Actress, uint](func() models.Actress { return models.Actress{} }),
		),
	}
	repo.merger = &actressMerger{repo: repo}
	return repo
}

// Create inserts a new actress record.
func (r *ActressRepository) Create(ctx context.Context, actress *models.Actress) error {
	if actress.Origin == "" {
		actress.Verified = true
		actress.Origin = ActressOriginUser
	}
	return r.BaseRepository.Create(ctx, actress)
}

// Update saves all fields of the given actress record.
func (r *ActressRepository) Update(ctx context.Context, actress *models.Actress) error {
	if actress == nil {
		return wrapDBErr("update", "nil actress", ErrInvalidLookup)
	}
	if actress.ID == 0 {
		if err := r.GetDB().WithContext(ctx).Save(actress).Error; err != nil {
			return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
		}
		return nil
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentIDs, err := movieContentIDsForActressesTx(tx, actress.ID)
		if err != nil {
			return err
		}
		before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
		if err != nil {
			return err
		}
		var current models.Actress
		if err := tx.First(&current, actress.ID).Error; err != nil {
			return wrapDBErr("update", fmt.Sprintf("actress %d", actress.ID), err)
		}
		identityChanged := current.FirstName != actress.FirstName ||
			current.LastName != actress.LastName ||
			current.JapaneseName != actress.JapaneseName ||
			current.ThumbURL != actress.ThumbURL
		catalogChanged := identityChanged || current.DMMID != actress.DMMID ||
			current.Aliases != actress.Aliases || current.Verified != actress.Verified ||
			current.Origin != actress.Origin || current.NameKey != actress.NameKey
		if !catalogChanged {
			*actress = current
			return nil
		}
		if err := tx.Save(actress).Error; err != nil {
			return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
		}
		if identityChanged {
			if err := transitionActressCanonicalNamesTx(tx, actress.ID, &current); err != nil {
				return err
			}
			if err := reconcileActressCollisionsTx(tx, actress.ID); err != nil {
				return err
			}
		}
		return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
	})
}

// RenameNameFields updates only the editable name columns (first_name,
// last_name, japanese_name) of the actress identified by id. It is used by the
// review-page edit path to apply an explicit actress rename without clobbering
// other columns (created_at, dmm_id, thumb_url, aliases) the way a full-row
// Save would. Callers should gate on a name-field change to avoid bumping
// updated_at for unedited actresses.
func (r *ActressRepository) RenameNameFields(ctx context.Context, id uint, firstName, lastName, japaneseName string) error {
	if id == 0 {
		return wrapDBErr("rename", "actress id 0", ErrInvalidLookup)
	}
	updates := map[string]interface{}{
		colFirstName:    firstName,
		colLastName:     lastName,
		colJapaneseName: japaneseName,
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentIDs, err := movieContentIDsForActressesTx(tx, id)
		if err != nil {
			return err
		}
		before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
		if err != nil {
			return err
		}
		var current models.Actress
		if err := tx.First(&current, id).Error; err != nil {
			return wrapDBErr("rename", fmt.Sprintf("actress %d", id), err)
		}
		if err := tx.Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return wrapDBErr("rename", fmt.Sprintf("actress %d", id), err)
		}
		if err := transitionActressCanonicalNamesTx(tx, id, &current); err != nil {
			return err
		}
		if err := reconcileActressCollisionsTx(tx, id); err != nil {
			return err
		}
		return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
	})
}

// FindByID loads an actress by its primary key.
func (r *ActressRepository) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// Delete removes the actress with the given primary key.
func (r *ActressRepository) Delete(ctx context.Context, id uint) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentIDs, err := movieContentIDsForActressesTx(tx, id)
		if err != nil {
			return err
		}
		before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
		if err != nil {
			return err
		}
		if err := deleteCreditReassignmentsTx(tx, "source_actress_id = ? OR target_actress_id = ?", fmt.Sprintf("actress %d", id), id, id); err != nil {
			return err
		}
		if err := deleteCreditRecordsTx(tx, "credit_id IN (SELECT id FROM movie_credits WHERE actress_id = ?)", "actress_id = ?", id, fmt.Sprintf("actress %d", id)); err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM movie_actresses WHERE actress_id = ?", id).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("legacy actress associations for %d", id), err)
		}
		if err := tx.Where("actress_id = ?", id).Delete(&models.ActressTranslation{}).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("translations for actress %d", id), err)
		}
		if err := tx.Delete(&models.Actress{}, id).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("actress %d", id), err)
		}
		return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
	})
}

// Count returns the total number of actress records.
func (r *ActressRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.catalogQuery(ctx).Model(&models.Actress{}).Count(&count).Error; err != nil {
		return 0, wrapDBErr("count", "actresses", err)
	}
	return count, nil
}

// FindByDMMID loads the actress with the given DMM identifier, returning
// ErrNotFound when the id is zero and ErrInvalidLookup when negative.
func (r *ActressRepository) FindByDMMID(ctx context.Context, dmmID int) (*models.Actress, error) {
	if dmmID < 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrInvalidLookup)
	}
	if dmmID == 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrNotFound)
	}
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).First(&actress, "dmm_id = ?", dmmID).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), err)
	}
	return &actress, nil
}

// FindByJapaneseName loads the first actress matching the given Japanese name,
// preferring higher DMM ids when duplicates exist.
func (r *ActressRepository) FindByJapaneseName(ctx context.Context, name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s", name), err)
	}
	return &actress, nil
}

// FindByFirstNameLastName loads the first actress matching the given first and
// last name, preferring higher DMM ids when duplicates exist.
func (r *ActressRepository) FindByFirstNameLastName(ctx context.Context, firstName, lastName string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "first_name = ? AND last_name = ?", firstName, lastName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s %s", lastName, firstName), err)
	}
	return &actress, nil
}

// FindByJapaneseNameAndDMMID loads an actress by Japanese name and DMM id,
// falling back to whichever identifier is provided when only one is set.
func (r *ActressRepository) FindByJapaneseNameAndDMMID(ctx context.Context, name string, dmmID int) (*models.Actress, error) {
	var actress models.Actress
	if name != "" && dmmID > 0 {
		err := r.GetDB().WithContext(ctx).First(&actress, "japanese_name = ? AND dmm_id = ?", name, dmmID).Error
		if err != nil {
			return nil, wrapDBErr("find", fmt.Sprintf("actress %s dmm_id %d", name, dmmID), err)
		}
		return &actress, nil
	} else if name != "" {
		return r.FindByJapaneseName(ctx, name)
	} else if dmmID > 0 {
		return r.FindByDMMID(ctx, dmmID)
	}
	return nil, wrapDBErr("find", "actress by japanese_name and dmm_id", ErrInvalidLookup)
}

// ListAll returns every actress record in the default sort order.
func (r *ActressRepository) ListAll(ctx context.Context) ([]models.Actress, error) {
	var actresses []models.Actress
	if err := r.catalogQuery(ctx).Order(r.defaultOrder).Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("list", "actresses", err)
	}
	return actresses, nil
}

// FindOrCreate returns the existing actress with the given Japanese name, or
// creates a new record when none is found.
func (r *ActressRepository) FindOrCreate(ctx context.Context, actress *models.Actress) error {
	if actress.JapaneseName != "" {
		existing, err := r.FindByJapaneseName(ctx, actress.JapaneseName)
		if err == nil {
			*actress = *existing
			return nil
		}
	}

	return r.Create(ctx, actress)
}

// List returns a page of actresses limited by limit and offset.
func (r *ActressRepository) List(ctx context.Context, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress
	query := r.catalogQuery(ctx).Order(r.defaultOrder)
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("list", "actresses", err)
	}
	return actresses, nil
}

// ListSorted returns a page of actresses ordered by the validated sortBy and
// sortOrder columns.
func (r *ActressRepository) ListSorted(ctx context.Context, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	dbq := r.catalogQuery(ctx)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

// SearchPaged returns a page of actresses whose names match the query, ordered
// by the default sort.
func (r *ActressRepository) SearchPaged(ctx context.Context, query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.catalogQuery(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
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

// SearchPagedSorted returns a page of actresses matching the query, ordered by
// the validated sortBy and sortOrder columns.
func (r *ActressRepository) SearchPagedSorted(ctx context.Context, query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"

	dbq := r.catalogQuery(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
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

// CountSearch returns the number of actresses whose names match the query.
func (r *ActressRepository) CountSearch(ctx context.Context, query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.catalogQuery(ctx).Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}

// Search returns up to 50 actresses matching the query, or up to 100 when
// the query is empty.
func (r *ActressRepository) Search(ctx context.Context, query string) ([]models.Actress, error) {
	var actresses []models.Actress

	if query == "" {
		err := r.catalogQuery(ctx).Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		if err != nil {
			return nil, wrapDBErr("find", "actresses", err)
		}
		return actresses, nil
	}

	searchPattern := "%" + query + "%"
	err := r.catalogQuery(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(50).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

// PreviewMerge computes a non-persistent preview of merging the source
// actress into the target actress.
func (r *ActressRepository) PreviewMerge(ctx context.Context, targetID, sourceID uint) (*ActressMergePreview, error) {
	return r.merger.PreviewMerge(ctx, targetID, sourceID)
}

// Merge computes a merge plan for the source actress into the target and
// executes it within a transaction.
func (r *ActressRepository) Merge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	return r.merger.Merge(ctx, targetID, sourceID, resolutions, r.GetDB())
}

// Actress origin values mark who owns an identity row.
const (
	ActressOriginUser   = "user"   // Created or edited by the user
	ActressOriginImport = "import" // Created by a curated import
	ActressOriginScrape = "scrape" // Created by a scrape resolution miss
)

// FindVerifiedByDMMID loads a verified identity by its DMM ID.
func (r *ActressRepository) FindVerifiedByDMMID(ctx context.Context, dmmID int) (*models.Actress, error) {
	if dmmID <= 0 {
		return nil, fmt.Errorf("find verified actress by dmm %d: %w", dmmID, ErrNotFound)
	}
	var found models.Actress
	err := r.GetDB().WithContext(ctx).First(&found, "dmm_id = ? AND verified = ?", dmmID, true).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find verified actress by dmm %d: %w", dmmID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("verified actress dmm %d", dmmID), err)
	}
	return &found, nil
}

// FindVerifiedByAlias resolves an alias to its verified identity via the
// alias table and canonical-name matching.
func (r *ActressRepository) FindVerifiedByAlias(ctx context.Context, aliasName string) (*models.Actress, error) {
	aliases, err := normalizedActressAliasesTx(r.GetDB().WithContext(ctx), aliasName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find alias %s: %w", aliasName, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("alias %s", aliasName), err)
	}
	found, err := r.FindVerifiedByExactName(ctx, aliases[0].CanonicalName, "", "")
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("find verified actress for alias %s: %w", aliasName, ErrNotFound)
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("find verified actress for alias %s: %w", aliasName, ErrActressAliasAmbiguous)
	}
	return &found[0], nil
}

// FindVerifiedByExactName returns verified identities matching the given
// name exactly (normalized). Multiple results indicate a homonym ambiguity.
func (r *ActressRepository) FindVerifiedByExactName(ctx context.Context, japaneseName, firstName, lastName string) ([]models.Actress, error) {
	incoming := &models.Actress{JapaneseName: japaneseName, FirstName: firstName, LastName: lastName}
	keys := make(map[string]struct{})
	for _, representation := range canonicalActressRepresentations(incoming) {
		if key := models.NormalizeActressNameKey(representation); key != "" {
			keys[key] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}

	var verified []models.Actress
	if err := r.GetDB().WithContext(ctx).Where("verified = ?", true).Order("id ASC").Find(&verified).Error; err != nil {
		return nil, wrapDBErr("find", "verified actresses by canonical name", err)
	}
	found := make([]models.Actress, 0)
	seen := make(map[uint]struct{})
	for i := range verified {
		for _, representation := range canonicalActressRepresentations(&verified[i]) {
			key := models.NormalizeActressNameKey(representation)
			if _, matches := keys[key]; key == "" || !matches {
				continue
			}
			if _, duplicate := seen[verified[i].ID]; !duplicate {
				seen[verified[i].ID] = struct{}{}
				found = append(found, verified[i])
			}
			break
		}
	}
	return found, nil
}

// ListCandidates returns quarantined (unverified) identities, newest first.
func (r *ActressRepository) ListCandidates(ctx context.Context, limit, offset int) ([]models.Actress, error) {
	if limit <= 0 {
		limit = 50
	}
	var candidates []models.Actress
	err := r.GetDB().WithContext(ctx).
		Where("verified = ?", false).
		Order("updated_at DESC, id ASC").
		Limit(limit).Offset(offset).
		Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("list", "candidate actresses", err)
	}
	return candidates, nil
}

// CountCandidates returns the number of quarantined candidate identities.
func (r *ActressRepository) CountCandidates(ctx context.Context) (int64, error) {
	var count int64
	if err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where("verified = ?", false).Count(&count).Error; err != nil {
		return 0, wrapDBErr("count", "candidate actresses", err)
	}
	return count, nil
}

// PromoteCandidate marks a quarantined candidate as a verified user-owned
// identity with the user-confirmed canonical fields.
func (r *ActressRepository) PromoteCandidate(ctx context.Context, id uint, firstName, lastName, japaneseName, thumbURL string) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentIDs, err := movieContentIDsForActressesTx(tx, id)
		if err != nil {
			return err
		}
		return mutateMovieRenderInputsTx(tx, contentIDs, func() error {
			return promoteCandidateTx(tx, id, firstName, lastName, japaneseName, thumbURL)
		})
	})
}

func promoteCandidateTx(tx *gorm.DB, id uint, firstName, lastName, japaneseName, thumbURL string) error {
	updates := map[string]interface{}{
		"verified":              true,
		"origin":                ActressOriginUser,
		colAmbiguityQuarantined: false,
		colFirstName:            firstName,
		colLastName:             lastName,
		colJapaneseName:         japaneseName,
		"thumb_url":             thumbURL,
	}
	var candidate models.Actress
	if err := tx.First(&candidate, id).Error; err != nil {
		return wrapDBErr("promote", fmt.Sprintf("candidate %d", id), err)
	}
	if err := tx.Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("promote", fmt.Sprintf("candidate %d", id), err)
	}
	if err := transitionActressCanonicalNamesTx(tx, id, &candidate); err != nil {
		return wrapDBErr("promote", fmt.Sprintf("aliases for candidate %d", id), err)
	}
	if err := resolveCandidateIdentityCollisionsTx(tx, id); err != nil {
		return err
	}
	return restoreActressProjectionTx(tx, id)
}

// SetUserOwned marks an identity as user-owned so curated imports cannot
// overwrite user corrections.
func (r *ActressRepository) SetUserOwned(ctx context.Context, id uint) error {
	if err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where("id = ?", id).
		Update("origin", ActressOriginUser).Error; err != nil {
		return wrapDBErr("set user owned", fmt.Sprintf("actress %d", id), err)
	}
	return nil
}

// UpdateCanonicalFields applies a user edit to the identity canonical
// fields, marks the row user-owned, and dirties crediting movies.
func (r *ActressRepository) UpdateCanonicalFields(ctx context.Context, id uint, firstName, lastName, japaneseName, thumbURL string) error {
	updates := map[string]interface{}{
		colFirstName: firstName, colLastName: lastName, colJapaneseName: japaneseName,
		"thumb_url": thumbURL, "origin": ActressOriginUser, "verified": true, colAmbiguityQuarantined: false,
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentIDs, err := movieContentIDsForActressesTx(tx, id)
		if err != nil {
			return err
		}
		return mutateMovieRenderInputsTx(tx, contentIDs, func() error {
			var current models.Actress
			if err := tx.First(&current, id).Error; err != nil {
				return wrapDBErr("update canonical", fmt.Sprintf("actress %d", id), err)
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return wrapDBErr("update canonical", fmt.Sprintf("actress %d", id), err)
			}
			if err := transitionActressCanonicalNamesTx(tx, id, &current); err != nil {
				return err
			}
			if err := reconcileActressCollisionsTx(tx, id); err != nil {
				return err
			}
			return restoreActressProjectionTx(tx, id)
		})
	})
}

// ImportUpsert upserts a curated-import actress, skipping protected
// canonical fields on rows that are user-owned or verified.
func (r *ActressRepository) ImportUpsert(ctx context.Context, incoming *models.Actress) error {
	if incoming == nil {
		return fmt.Errorf("import upsert: incoming actress must not be nil")
	}
	existing, err := r.findImportMatch(ctx, incoming)
	if err != nil {
		return err
	}
	if existing == nil {
		incoming.Verified = true
		incoming.Origin = ActressOriginImport
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(incoming).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("imported actress %s", incoming.FullName()), err)
			}
			return nil
		})
	}
	incoming.ID = existing.ID
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previousIdentity models.Actress
		if err := tx.First(&previousIdentity, incoming.ID).Error; err != nil {
			return wrapDBErr("load", fmt.Sprintf("imported actress %d", incoming.ID), err)
		}
		incoming.CreatedAt = previousIdentity.CreatedAt
		incoming.Verified = true
		incoming.Origin = ActressOriginImport
		if incoming.DMMID == 0 && previousIdentity.DMMID > 0 {
			incoming.DMMID = previousIdentity.DMMID
		}
		promotingCandidate := !previousIdentity.Verified
		if previousIdentity.Verified && (previousIdentity.Origin == ActressOriginUser || previousIdentity.Origin == ActressOriginImport) {
			incoming.Verified = previousIdentity.Verified
			incoming.Origin = previousIdentity.Origin
			incoming.DMMID = previousIdentity.DMMID
			fillEmptyActressFields(&previousIdentity, incoming)
			incoming.FirstName = previousIdentity.FirstName
			incoming.LastName = previousIdentity.LastName
			incoming.JapaneseName = previousIdentity.JapaneseName
			incoming.ThumbURL = previousIdentity.ThumbURL
		}
		contentIDs, err := movieContentIDsForActressesTx(tx, incoming.ID)
		if err != nil {
			return err
		}
		before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
		if err != nil {
			return err
		}
		if err := tx.Save(incoming).Error; err != nil {
			return wrapDBErr("save", fmt.Sprintf("imported actress %s", incoming.FullName()), err)
		}
		if !promotingCandidate {
			return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
		}
		if err := transitionActressCanonicalNamesTx(tx, incoming.ID, &previousIdentity); err != nil {
			return err
		}
		if err := resolveCandidateIdentityCollisionsTx(tx, incoming.ID); err != nil {
			return err
		}
		if err := restoreActressProjectionTx(tx, incoming.ID); err != nil {
			return err
		}
		return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
	})
}

func (r *ActressRepository) findImportMatch(ctx context.Context, incoming *models.Actress) (*models.Actress, error) {
	if incoming.ID > 0 {
		var found models.Actress
		err := r.GetDB().WithContext(ctx).First(&found, "id = ?", incoming.ID).Error
		if err == nil {
			return &found, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wrapDBErr("find", fmt.Sprintf("import match id %d", incoming.ID), err)
		}
	}
	if incoming.DMMID > 0 {
		var found models.Actress
		err := r.GetDB().WithContext(ctx).First(&found, "dmm_id = ?", incoming.DMMID).Error
		if err == nil {
			return &found, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wrapDBErr("find", fmt.Sprintf("import match dmm %d", incoming.DMMID), err)
		}
	}
	matches, err := r.findImportMatchesByCanonicalUnion(ctx, incoming)
	if err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous import match for %s: %d identities share a canonical representation", incoming.FullName(), len(matches))
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	return nil, nil
}

func (r *ActressRepository) findImportMatchesByCanonicalUnion(ctx context.Context, incoming *models.Actress) ([]models.Actress, error) {
	incomingKeys := canonicalActressRepresentationKeys(incoming)
	if len(incomingKeys) == 0 {
		return nil, nil
	}
	query := r.GetDB().WithContext(ctx)
	// Exact positive-DMM ownership is handled before this fallback. A different
	// positive DMM identity must never be claimed by a name-only match.
	if incoming.DMMID > 0 {
		query = query.Where("dmm_id = ?", 0)
	}
	var actresses []models.Actress
	if err := query.Find(&actresses).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("import canonical union %s", incoming.FullName()), err)
	}
	matches := make([]models.Actress, 0, 2)
	for i := range actresses {
		if exactActressNamesMatch(incoming, &actresses[i]) {
			matches = append(matches, actresses[i])
		}
	}
	return matches, nil
}

func canonicalActressRepresentationKeys(actress *models.Actress) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, representation := range canonicalActressRepresentations(actress) {
		if key := models.NormalizeActressNameKey(representation); key != "" {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func exactActressNamesMatch(left, right *models.Actress) bool {
	leftKeys := canonicalActressRepresentationKeys(left)
	if len(leftKeys) == 0 {
		return false
	}
	for key := range canonicalActressRepresentationKeys(right) {
		if _, ok := leftKeys[key]; ok {
			return true
		}
	}
	return false
}

func fillEmptyActressFields(existing, incoming *models.Actress) {
	if existing.FirstName == "" {
		existing.FirstName = incoming.FirstName
	}
	if existing.LastName == "" {
		existing.LastName = incoming.LastName
	}
	if existing.JapaneseName == "" {
		existing.JapaneseName = incoming.JapaneseName
	}
	if existing.ThumbURL == "" {
		existing.ThumbURL = incoming.ThumbURL
	}
}

// DeleteStaleCandidates prunes unverified candidates older than the given
// time that hold no credits and no aliases. Returns the pruned count.
func (r *ActressRepository) DeleteStaleCandidates(ctx context.Context, olderThan time.Time) (int64, error) {
	var pruned int64
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []models.Actress
		if err := tx.Where("verified = ? AND updated_at < ?", false, olderThan).Order("id").Find(&candidates).Error; err != nil {
			return wrapDBErr("list", "stale candidates", err)
		}
		for i := range candidates {
			if err := tx.Exec(`
DELETE FROM movie_actresses
WHERE actress_id = ?
  AND (
      movie_content_id IS NULL
      OR NOT EXISTS (
          SELECT 1 FROM movies m
          WHERE m.content_id = movie_actresses.movie_content_id
      )
  )`, candidates[i].ID).Error; err != nil {
				return wrapDBErr("delete", fmt.Sprintf("non-projectable legacy associations for stale candidate %d", candidates[i].ID), err)
			}
			keys := make([]string, 0, 3)
			for key := range canonicalActressRepresentationKeys(&candidates[i]) {
				keys = append(keys, key)
			}
			query := `
DELETE FROM actresses
WHERE id = ?
  AND verified = 0
  AND updated_at < ?
  AND id NOT IN (SELECT DISTINCT actress_id FROM movie_credits)
  AND NOT EXISTS (
      SELECT 1
      FROM movie_actresses ma
      JOIN movies m ON m.content_id = ma.movie_content_id
      WHERE ma.actress_id = actresses.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM movie_credit_reassignments mr
      WHERE mr.source_actress_id = actresses.id
         OR mr.target_actress_id = actresses.id
  )`
			args := []interface{}{candidates[i].ID, olderThan}
			if len(keys) > 0 {
				query += `
  AND NOT EXISTS (
      SELECT 1 FROM actress_aliases al
      WHERE al.canonical_name_key IN ?
  )`
				args = append(args, keys)
			}
			res := tx.Exec(query, args...)
			if res.Error != nil {
				return wrapDBErr("delete", fmt.Sprintf("stale candidate %d", candidates[i].ID), res.Error)
			}
			if res.RowsAffected == 0 {
				continue
			}
			if err := tx.Where("actress_id = ?", candidates[i].ID).Delete(&models.ActressTranslation{}).Error; err != nil {
				return wrapDBErr("delete", fmt.Sprintf("translations for stale candidate %d", candidates[i].ID), err)
			}
			pruned += res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pruned, nil
}

func resolveCandidateIdentityCollisionsTx(tx *gorm.DB, actressID uint) error {
	if err := tx.Model(&models.CreditCollision{}).
		Where("credit_id IN (SELECT id FROM movie_credits WHERE actress_id = ?) AND field = ? AND status = ?", actressID, models.CreditFieldIdentityLink, models.CollisionStatusOpen).
		Updates(map[string]interface{}{
			colStatus:     models.CollisionStatusResolved,
			colResolution: models.CollisionResolutionKeepIdentity,
			colUpdatedAt:  time.Now().UTC(),
		}).Error; err != nil {
		return wrapDBErr("resolve", fmt.Sprintf("identity collisions for actress %d", actressID), err)
	}
	return nil
}

func restoreActressProjectionTx(tx *gorm.DB, actressID uint) error {
	if err := tx.Exec(`
		INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id)
		SELECT movie_content_id, actress_id
		FROM movie_credits
		WHERE actress_id = ? AND suppressed = ?`, actressID, false).Error; err != nil {
		return wrapDBErr("restore", fmt.Sprintf("legacy actress associations for %d", actressID), err)
	}
	return nil
}

func (r *ActressRepository) catalogQuery(ctx context.Context) *gorm.DB {
	return r.GetDB().WithContext(ctx).Where("verified = ?", true)
}

// FreshTranslationsByActress returns translations whose source name still
// matches the current canonical name, omitting stale rows.
func (r *ActressRepository) FreshTranslationsByActress(ctx context.Context, actressID uint) ([]models.ActressTranslation, error) {
	var actress models.Actress
	if err := r.GetDB().WithContext(ctx).First(&actress, actressID).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %d", actressID), err)
	}
	translationRepo := newActressTranslationRepository(r.GetDB())
	translations, err := translationRepo.FindAllByActress(ctx, actressID)
	if err != nil {
		return nil, err
	}
	canonical := models.NormalizeActressNameKey(actress.FullName())
	fresh := make([]models.ActressTranslation, 0, len(translations))
	for _, t := range translations {
		if models.NormalizeActressNameKey(t.SourceName) != canonical && strings.TrimSpace(t.SourceName) != "" {
			logging.Debugf("stale actress translation %d (source %q != canonical %q) — flagged for re-translation", t.ID, t.SourceName, actress.FullName())
			continue
		}
		fresh = append(fresh, t)
	}
	return fresh, nil
}
