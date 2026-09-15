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
	if err := r.GetDB().WithContext(ctx).Save(actress).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
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
	if err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("rename", fmt.Sprintf("actress %d", id), err)
	}
	r.markCreditingMoviesDirty(ctx, id)
	return nil
}

// FindByID loads an actress by its primary key.
func (r *ActressRepository) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// Delete removes the actress with the given primary key.
func (r *ActressRepository) Delete(ctx context.Context, id uint) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deleteCreditReassignmentsTx(tx, "source_actress_id = ? OR target_actress_id = ?", fmt.Sprintf("actress %d", id), id, id); err != nil {
			return err
		}
		if err := deleteCreditRecordsTx(tx, "credit_id IN (SELECT id FROM movie_credits WHERE actress_id = ?)", "actress_id = ?", id, fmt.Sprintf("actress %d", id)); err != nil {
			return err
		}
		if err := tx.Delete(&models.Actress{}, id).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("actress %d", id), err)
		}
		return nil
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
	var alias models.ActressAlias
	err := r.GetDB().WithContext(ctx).First(&alias, "alias_name = ?", aliasName).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find alias %s: %w", aliasName, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("alias %s", aliasName), err)
	}
	key := models.NormalizeActressNameKey(alias.CanonicalName)
	if key == "" {
		return nil, fmt.Errorf("find alias %s: %w", aliasName, ErrNotFound)
	}
	var found models.Actress
	err = r.GetDB().WithContext(ctx).Where(
		"verified = ? AND (LOWER(TRIM(japanese_name)) = ? OR LOWER(TRIM(last_name || ' ' || first_name)) = ? OR LOWER(TRIM(first_name || ' ' || last_name)) = ?)",
		true, key, key, key,
	).First(&found).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find verified actress for alias %s: %w", aliasName, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("verified actress for alias %s", aliasName), err)
	}
	return &found, nil
}

// FindVerifiedByExactName returns verified identities matching the given
// name exactly (normalized). Multiple results indicate a homonym ambiguity.
func (r *ActressRepository) FindVerifiedByExactName(ctx context.Context, japaneseName, firstName, lastName string) ([]models.Actress, error) {
	key := ""
	if ja := models.NormalizeActressNameKey(japaneseName); ja != "" {
		key = ja
	} else if firstName != "" || lastName != "" {
		key = models.NormalizeActressNameKey(lastName + " " + firstName)
	}
	if key == "" {
		return nil, nil
	}
	var found []models.Actress
	err := r.GetDB().WithContext(ctx).Where(
		"verified = ? AND (LOWER(TRIM(japanese_name)) = ? OR LOWER(TRIM(last_name || ' ' || first_name)) = ? OR LOWER(TRIM(first_name || ' ' || last_name)) = ?)",
		true, key, key, key,
	).Find(&found).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("verified actresses by name %s", key), err)
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
	updates := map[string]interface{}{
		"verified":      true,
		"origin":        ActressOriginUser,
		colFirstName:    firstName,
		colLastName:     lastName,
		colJapaneseName: japaneseName,
		"thumb_url":     thumbURL,
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate models.Actress
		if err := tx.First(&candidate, id).Error; err != nil {
			return wrapDBErr("promote", fmt.Sprintf("candidate %d", id), err)
		}
		previousName := canonicalActressName(&candidate)
		promotedName := canonicalActressName(&models.Actress{
			FirstName:    firstName,
			LastName:     lastName,
			JapaneseName: japaneseName,
		})

		if err := tx.Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return wrapDBErr("promote", fmt.Sprintf("candidate %d", id), err)
		}
		if previousName != "" && !strings.EqualFold(previousName, promotedName) {
			if err := upsertActressAliases(tx, collectActressAliasCandidates(&candidate), promotedName); err != nil {
				return wrapDBErr("promote", fmt.Sprintf("aliases for candidate %d", id), err)
			}
		}
		if err := resolveCandidateIdentityCollisionsTx(tx, id); err != nil {
			return err
		}
		return restoreActressProjectionTx(tx, id)
	})
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
		colFirstName:    firstName,
		colLastName:     lastName,
		colJapaneseName: japaneseName,
		"thumb_url":     thumbURL,
		"origin":        ActressOriginUser,
		"verified":      true,
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("update canonical", fmt.Sprintf("actress %d", id), err)
	}
	r.markCreditingMoviesDirty(ctx, id)
	return nil
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
	incoming.CreatedAt = existing.CreatedAt
	incoming.Verified = true
	incoming.Origin = ActressOriginImport
	if incoming.DMMID == 0 && existing.DMMID > 0 {
		incoming.DMMID = existing.DMMID
	}
	promotingCandidate := !existing.Verified
	if existing.Verified && (existing.Origin == ActressOriginUser || existing.Origin == ActressOriginImport) {
		incoming.Verified = existing.Verified
		incoming.Origin = existing.Origin
		incoming.DMMID = existing.DMMID
		fillEmptyActressFields(existing, incoming)
		incoming.FirstName = existing.FirstName
		incoming.LastName = existing.LastName
		incoming.JapaneseName = existing.JapaneseName
		incoming.ThumbURL = existing.ThumbURL
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(incoming).Error; err != nil {
			return wrapDBErr("save", fmt.Sprintf("imported actress %s", incoming.FullName()), err)
		}
		if !promotingCandidate {
			return nil
		}
		if err := resolveCandidateIdentityCollisionsTx(tx, incoming.ID); err != nil {
			return err
		}
		return restoreActressProjectionTx(tx, incoming.ID)
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
	matches, err := r.FindVerifiedByExactName(ctx, incoming.JapaneseName, incoming.FirstName, incoming.LastName)
	if err != nil {
		return nil, err
	}
	if incoming.DMMID > 0 {
		dmmLessMatches := make([]models.Actress, 0, len(matches))
		for i := range matches {
			if matches[i].DMMID <= 0 {
				dmmLessMatches = append(dmmLessMatches, matches[i])
			}
		}
		matches = dmmLessMatches
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous import match for %s: %d verified identities share the exact name", incoming.FullName(), len(matches))
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	candidate, candidateErr := findCandidateByNameKeyTx(r.GetDB().WithContext(ctx), actressNameKey(incoming))
	if candidateErr == nil {
		return candidate, nil
	}
	if !errors.Is(candidateErr, gorm.ErrRecordNotFound) {
		return nil, wrapDBErr("find", fmt.Sprintf("import candidate %s", incoming.FullName()), candidateErr)
	}
	if actressNameKey(incoming) == "" {
		return nil, nil
	}
	candidate, candidateErr = r.findDMMCandidateByExactName(ctx, incoming)
	if candidateErr != nil {
		return nil, candidateErr
	}
	return candidate, nil
}

func (r *ActressRepository) findDMMCandidateByExactName(ctx context.Context, incoming *models.Actress) (*models.Actress, error) {
	var candidates []models.Actress
	if err := r.GetDB().WithContext(ctx).
		Where("verified = ? AND dmm_id > ?", false, 0).
		Find(&candidates).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("DMM import candidate %s", incoming.FullName()), err)
	}
	matches := make([]models.Actress, 0, len(candidates))
	for i := range candidates {
		if exactActressNamesMatch(incoming, &candidates[i]) {
			matches = append(matches, candidates[i])
		}
	}
	if len(matches) != 1 {
		return nil, nil
	}
	return &matches[0], nil
}

func exactActressNamesMatch(left, right *models.Actress) bool {
	if left == nil || right == nil {
		return false
	}
	if japanese := models.NormalizeActressNameKey(left.JapaneseName); japanese != "" && japanese == models.NormalizeActressNameKey(right.JapaneseName) {
		return true
	}
	leftFirst, leftLast := strings.TrimSpace(left.FirstName), strings.TrimSpace(left.LastName)
	rightFirst, rightLast := strings.TrimSpace(right.FirstName), strings.TrimSpace(right.LastName)
	if leftFirst != "" && leftLast != "" && rightFirst != "" && rightLast != "" {
		return models.NormalizeActressNameKey(leftLast+" "+leftFirst) == models.NormalizeActressNameKey(rightLast+" "+rightFirst) ||
			models.NormalizeActressNameKey(leftFirst+" "+leftLast) == models.NormalizeActressNameKey(rightFirst+" "+rightLast)
	}
	if leftFirst != "" && leftLast == "" {
		return rightLast == "" && models.NormalizeActressNameKey(leftFirst) == models.NormalizeActressNameKey(rightFirst)
	}
	if leftLast != "" && leftFirst == "" {
		return rightFirst == "" && models.NormalizeActressNameKey(leftLast) == models.NormalizeActressNameKey(rightLast)
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
	res := r.GetDB().WithContext(ctx).Exec(`
DELETE FROM actresses
WHERE verified = 0
  AND updated_at < ?
  AND id NOT IN (SELECT DISTINCT actress_id FROM movie_credits)
  AND NOT EXISTS (
      SELECT 1 FROM actress_aliases al
      WHERE al.canonical_name = actresses.japanese_name
         OR al.canonical_name = (actresses.last_name || ' ' || actresses.first_name)
         OR al.canonical_name = (actresses.first_name || ' ' || actresses.last_name)
  )
  AND NOT EXISTS (
      SELECT 1 FROM movie_credit_reassignments mr
      WHERE mr.source_actress_id = actresses.id
  )
`, olderThan)
	if res.Error != nil {
		return 0, wrapDBErr("delete", "stale candidates", res.Error)
	}
	return res.RowsAffected, nil
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
	if err := tx.Exec(
		"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id IN (SELECT movie_content_id FROM movie_credits WHERE actress_id = ?)",
		actressID,
	).Error; err != nil {
		return wrapDBErr("mark dirty", fmt.Sprintf("movies for actress %d", actressID), err)
	}
	return nil
}

func (r *ActressRepository) markCreditingMoviesDirty(ctx context.Context, actressID uint) {
	if r.GetDB() == nil || actressID == 0 {
		return
	}
	if err := r.GetDB().WithContext(ctx).Exec(
		"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id IN (SELECT movie_content_id FROM movie_credits WHERE actress_id = ?)",
		actressID,
	).Error; err != nil {
		logging.Warnf("dirty-mark crediting movies for actress %d failed: %v", actressID, err)
	}
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
