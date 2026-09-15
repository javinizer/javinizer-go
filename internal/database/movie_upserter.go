package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// MovieUpserter handles the complex upsert pipeline for movies, including
// association resolution, genre/actress deduplication, and translation persistence.
// Extracted from MovieRepository to keep the repository focused on simple CRUD.
type MovieUpserter struct {
	repo *MovieRepository
}

// NewMovieUpserter creates a MovieUpserter that delegates database access
// through the given MovieRepository.
func NewMovieUpserter(repo *MovieRepository) *MovieUpserter {
	return &MovieUpserter{repo: repo}
}

// Upsert inserts or updates a movie and all its associations.
func (u *MovieUpserter) Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error) {
	return u.UpsertWithTranslations(ctx, movie, nil, nil)
}

// UpsertWithTranslations inserts or updates a movie along with genre and actress translations.
func (u *MovieUpserter) UpsertWithTranslations(ctx context.Context, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*models.Movie, error) {
	var result *models.Movie
	movie.Actresses = filterIdentifiableActresses(movie.Actresses)
	savedTranslations := make([]models.MovieTranslation, len(movie.Translations))
	copy(savedTranslations, movie.Translations)
	savedActresses := make([]models.Actress, len(movie.Actresses))
	copy(savedActresses, movie.Actresses)
	savedGenres := make([]models.Genre, len(movie.Genres))
	copy(savedGenres, movie.Genres)
	savedContentID := movie.ContentID
	savedCreatedAt := movie.CreatedAt
	var savedCredits []models.MovieCredit
	if movie.Credits != nil {
		savedCredits = make([]models.MovieCredit, len(movie.Credits))
		copy(savedCredits, movie.Credits)
	}
	err := retryOnLocked(func() error {
		movie.Translations = make([]models.MovieTranslation, len(savedTranslations))
		copy(movie.Translations, savedTranslations)
		movie.Actresses = make([]models.Actress, len(savedActresses))
		copy(movie.Actresses, savedActresses)
		movie.Genres = make([]models.Genre, len(savedGenres))
		copy(movie.Genres, savedGenres)
		movie.ContentID = savedContentID
		movie.CreatedAt = savedCreatedAt
		if savedCredits != nil {
			movie.Credits = make([]models.MovieCredit, len(savedCredits))
			copy(movie.Credits, savedCredits)
			for i := range movie.Credits {
				movie.Credits[i].ID = 0
			}
		} else {
			movie.Credits = nil
		}
		return u.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Step 1: Resolve ContentID
			if err := u.resolveContentID(tx, movie); err != nil {
				return err
			}

			// Step 2: Find existing movie or create new
			existingFound, err := u.findExistingMovieTx(tx, movie)
			if err != nil {
				return err
			}
			if !existingFound {
				if err := u.insertOrHandleDuplicateTx(tx, movie, &result); err != nil {
					return err
				}
			}

			// Step 3: Upsert genres (ensure genre records exist before association)
			if err := u.upsertGenresTx(tx, movie); err != nil {
				return err
			}

			// Step 4: Upsert actresses — credit pipeline when credits are present,
			// legacy ensure-exist path otherwise.
			if movie.Credits != nil {
				if err := u.persistCreditsTx(tx, movie); err != nil {
					return err
				}
			} else if err := u.upsertActressesTx(tx, movie); err != nil {
				return err
			}

			// Step 5: Upsert translations (core movie record + translations)
			var actressTranslationIDs map[int]uint
			if !movie.SkipCreditReconcile {
				actressTranslationIDs = actressTranslationIDsForCredits(savedActresses, movie.Credits)
			}
			if err := u.upsertTranslationsTx(tx, movie, savedTranslations, genreTranslations, actressTranslations, actressTranslationIDs); err != nil {
				return err
			}
			if movie.Credits == nil {
				if err := u.reconcileLegacyActressEditsTx(tx, movie); err != nil {
					return err
				}
			}

			// Step 6: Reload with associations
			var loaded models.Movie
			if err := tx.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&loaded, "content_id = ?", movie.ContentID).Error; err != nil {
				return wrapDBErr("reload", fmt.Sprintf("movie %s", movie.ContentID), err)
			}
			loadCreditsIntoTx(tx, &loaded)
			result = &loaded
			return nil
		})
	})
	return result, err
}

// resolveContentID ensures the movie has a ContentID set. If empty, it derives
// one from the movie ID. Returns an error if neither ContentID nor ID is set.

func loadCreditsIntoTx(tx *gorm.DB, m *models.Movie) {
	if m == nil {
		return
	}
	var credits []models.MovieCredit
	if err := tx.Preload("Actress").Where("movie_content_id = ?", m.ContentID).Order("order_index ASC, id ASC").Find(&credits).Error; err == nil {
		m.Credits = credits
	}
}

func (u *MovieUpserter) resolveContentID(_ *gorm.DB, movie *models.Movie) error {
	if strings.TrimSpace(movie.ContentID) == "" {
		if strings.TrimSpace(movie.ID) == "" {
			return fmt.Errorf("content_id is required when using ContentID as primary key")
		}
		movie.ContentID = strings.ToLower(strings.ReplaceAll(movie.ID, "-", ""))
	}
	return nil
}

// findExistingMovieTx looks up an existing movie by ContentID or ID.
// If found, it sets movie.ContentID and movie.CreatedAt from the existing record.
// Returns whether an existing movie was found.
func (u *MovieUpserter) findExistingMovieTx(tx *gorm.DB, movie *models.Movie) (bool, error) {
	var existing models.Movie
	var existingFound bool

	if movie.ContentID != "" {
		err := tx.Select("content_id", "created_at").First(&existing, "content_id = ?", movie.ContentID).Error
		if err == nil {
			existingFound = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, wrapDBErr("find", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
	}

	if !existingFound && movie.ID != "" {
		err := tx.Select("content_id", "created_at").First(&existing, "id = ?", movie.ID).Error
		if err == nil {
			existingFound = true
			movie.ContentID = existing.ContentID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, wrapDBErr("find", fmt.Sprintf("movie %s", movie.ID), err)
		}
	}

	if existingFound {
		movie.CreatedAt = existing.CreatedAt
	}

	return existingFound, nil
}

// insertOrHandleDuplicateTx attempts to create a new movie record. If the insert
// hits a duplicate-key error (concurrent create), it falls back to
// saveMovieWithAssociations and loads the result. Sets result if the
// duplicate-key path was taken; leaves result nil for the normal create path.
func (u *MovieUpserter) insertOrHandleDuplicateTx(tx *gorm.DB, movie *models.Movie, result **models.Movie) error {
	if err := tx.Omit("Actresses", "Genres", "Translations").Create(movie).Error; err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return wrapDBErr("create", fmt.Sprintf("movie %s", movie.ContentID), err)
		}

		// Duplicate-key: another transaction created the movie first.
		var existingMovie models.Movie
		loadErr := tx.Select("created_at").First(&existingMovie, "content_id = ?", movie.ContentID).Error
		if loadErr != nil {
			if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return wrapDBErr("find duplicate", fmt.Sprintf("movie %s", movie.ContentID), loadErr)
			}
		} else {
			movie.CreatedAt = existingMovie.CreatedAt
		}
		if err := u.saveMovieWithAssociations(tx, movie); err != nil {
			return wrapDBErr("save duplicate", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		var loaded models.Movie
		if err := tx.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&loaded, "content_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("reload", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		*result = &loaded
	}
	return nil
}

// upsertGenresTx ensures all genre records exist in the database before
// the movie's genre associations are persisted.
func (u *MovieUpserter) upsertGenresTx(tx *gorm.DB, movie *models.Movie) error {
	if err := u.ensureGenresExistTx(tx, movie.Genres); err != nil {
		return wrapDBErr("ensure genres", fmt.Sprintf("for movie %s", movie.ContentID), err)
	}
	return nil
}

// upsertActressesTx ensures all actress records exist in the database before
// the movie's actress associations are persisted.
func (u *MovieUpserter) upsertActressesTx(tx *gorm.DB, movie *models.Movie) error {
	if err := u.ensureActressesExistTx(tx, movie.Actresses); err != nil {
		return wrapDBErr("ensure actresses", fmt.Sprintf("for movie %s", movie.ContentID), err)
	}
	return nil
}

func (u *MovieUpserter) reconcileLegacyActressEditsTx(tx *gorm.DB, movie *models.Movie) error {
	creditRepo := NewMovieCreditRepository(u.repo.GetDB())
	existing, err := creditRepo.ListByMovieTx(tx, movie.ContentID)
	if err != nil {
		return err
	}
	existingByActress := make(map[uint]models.MovieCredit, len(existing))
	for _, credit := range existing {
		existingByActress[credit.ActressID] = credit
	}
	incoming := make(map[uint]bool, len(movie.Actresses))
	for i := range movie.Actresses {
		actress := &movie.Actresses[i]
		incoming[actress.ID] = true
		if credit, ok := existingByActress[actress.ID]; ok {
			if credit.Suppressed {
				if err := setCreditSuppressedTx(tx, credit.ID, false); err != nil {
					return err
				}
			}
			continue
		}
		credit := &models.MovieCredit{
			MovieContentID:       movie.ContentID,
			ActressID:            actress.ID,
			CreditedName:         actress.FullName(),
			CreditedJapaneseName: actress.JapaneseName,
			ReportedThumbURL:     actress.ThumbURL,
			Origin:               string(models.CreditOriginUser),
			OrderIndex:           i,
			OrderPinned:          true,
		}
		if err := creditRepo.UpsertTx(tx, credit); err != nil {
			return err
		}
		if err := tx.Exec(
			"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id = ?",
			movie.ContentID,
		).Error; err != nil {
			return err
		}
	}
	for _, credit := range existing {
		if !incoming[credit.ActressID] && !credit.Suppressed {
			if err := setCreditSuppressedTx(tx, credit.ID, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// upsertTranslationsTx saves the core movie record (without translation slice)
// and persists all translations (movie, genre, actress).
func (u *MovieUpserter) upsertTranslationsTx(tx *gorm.DB, movie *models.Movie, translations []models.MovieTranslation, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData, actressTranslationIDs map[int]uint) error {
	movie.Translations = nil
	if err := upsertMovieCoreWithActressTranslationIDs(tx, u.repo.GetDB(), movie, translations, genreTranslations, actressTranslations, actressTranslationIDs); err != nil {
		return wrapDBErr("save", fmt.Sprintf("movie %s", movie.ContentID), err)
	}
	return nil
}

func actressTranslationIDsForCredits(actresses []models.Actress, credits []models.MovieCredit) map[int]uint {
	limit := len(actresses)
	if len(credits) < limit {
		limit = len(credits)
	}
	if limit == 0 {
		return nil
	}
	ids := make(map[int]uint, limit)
	for i := 0; i < limit; i++ {
		ids[i] = credits[i].ActressID
	}
	return ids
}

func (u *MovieUpserter) saveMovieWithAssociations(tx *gorm.DB, movie *models.Movie) error {
	if err := u.ensureGenresExistTx(tx, movie.Genres); err != nil {
		return fmt.Errorf("save associations for movie %s: ensure genres: %w", movie.ContentID, err)
	}
	if err := u.ensureActressesExistTx(tx, movie.Actresses); err != nil {
		return fmt.Errorf("save associations for movie %s: ensure actresses: %w", movie.ContentID, err)
	}

	translations := movie.Translations
	movie.Translations = nil
	if err := upsertMovieCore(tx, u.repo.GetDB(), movie, translations, nil, nil); err != nil {
		return fmt.Errorf("save associations for movie %s: upsert core: %w", movie.ContentID, err)
	}
	return nil
}

func (u *MovieUpserter) ensureGenresExistTx(tx *gorm.DB, genres []models.Genre) error {
	if len(genres) == 0 {
		return nil
	}

	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}

	var existingGenres []models.Genre
	if err := tx.Where("name IN ?", names).Find(&existingGenres).Error; err != nil {
		return err
	}

	existingByName := make(map[string]models.Genre, len(existingGenres))
	for _, g := range existingGenres {
		existingByName[g.Name] = g
	}

	for i := range genres {
		if found, ok := existingByName[genres[i].Name]; ok {
			genres[i] = found
			continue
		}

		if err := raceRetryCreate(tx, &genres[i], func(tx *gorm.DB) error {
			var found models.Genre
			if err := tx.Where("name = ?", genres[i].Name).First(&found).Error; err != nil {
				return err
			}
			genres[i] = found
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func (u *MovieUpserter) mergeActressData(existing *models.Actress, new models.Actress) bool {
	needsUpdate := false

	if new.ThumbURL != "" && existing.ThumbURL == "" {
		existing.ThumbURL = new.ThumbURL
		needsUpdate = true
	}

	if new.FirstName != "" && existing.FirstName == "" {
		existing.FirstName = new.FirstName
		needsUpdate = true
	}
	if new.LastName != "" && existing.LastName == "" {
		existing.LastName = new.LastName
		needsUpdate = true
	}

	return needsUpdate
}

// actressGroupEntry pairs an actress pointer with its original slice index
// so that resolved actresses can be written back to the correct position.
type actressGroupEntry struct {
	index int
	act   *models.Actress
}

// actressLookupFunc finds an existing actress by the group's primary key field.
// It returns the found actress and whether a match was found.
type actressLookupFunc func(tx *gorm.DB, act *models.Actress) (models.Actress, bool, error)

// resolveActressGroup resolves a group of actresses that share the same primary lookup
// strategy. For each actress in the group, it checks if an existing record is found
// via the lookup function. If found, it merges data and saves; if not, it creates a new
// record with race-retry semantics for concurrent create conflicts.
func (u *MovieUpserter) resolveActressGroup(tx *gorm.DB, actresses []models.Actress, group []actressGroupEntry, lookupFn actressLookupFunc) error {
	for _, g := range group {
		existing, found, err := lookupFn(tx, g.act)
		if err != nil {
			return err
		}
		if found {
			if u.mergeActressData(&existing, *g.act) {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			}
			actresses[g.index] = existing
		} else {
			if g.act.Origin == "" {
				g.act.Verified = true
				g.act.Origin = ActressOriginUser
			}
			// A missing ID-keyed entry (idGroup) references a stale/deleted primary
			// key; create a genuinely new record with an auto-assigned id instead of
			// re-inserting with the stale PK (which could resurrect the row or merge
			// onto a reused id). For DMM/JP/Name groups the id is already 0.
			g.act.ID = 0
			if err := raceRetryCreate(tx, g.act, func(tx *gorm.DB) error {
				found, ok, findErr := lookupFn(tx, g.act)
				if !ok {
					return findErr
				}
				if u.mergeActressData(&found, *g.act) {
					if err := tx.Save(&found).Error; err != nil {
						return err
					}
				}
				actresses[g.index] = found
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// lookupActressByID finds an actress by its primary key. It is used for the
// review-page edit path (actresses carrying a DB id) so a rename that produces
// a name collision resolves to the renamed record by id rather than to the
// colliding record by name.
func lookupActressByID(tx *gorm.DB, act *models.Actress) (models.Actress, bool, error) {
	var found models.Actress
	if err := tx.First(&found, act.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return found, false, nil
		}
		return found, false, err
	}
	return found, true, nil
}

// lookupActressByDMMID finds an actress by DMM ID.
func lookupActressByDMMID(tx *gorm.DB, act *models.Actress) (models.Actress, bool, error) {
	var found models.Actress
	if err := tx.Where("dmm_id = ?", act.DMMID).First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return found, false, nil
		}
		return found, false, err
	}
	return found, true, nil
}

// lookupActressByJapaneseName finds an actress by Japanese name.
func lookupActressByJapaneseName(tx *gorm.DB, act *models.Actress) (models.Actress, bool, error) {
	var found models.Actress
	if err := tx.Where("japanese_name = ?", act.JapaneseName).First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return found, false, nil
		}
		return found, false, err
	}
	return found, true, nil
}

// lookupActressByName finds an actress by first/last name combination.
// It tries DMM ID and Japanese name first (in case the actress has those set),
// then falls back to first_name/last_name matching.
func lookupActressByName(tx *gorm.DB, act *models.Actress) (models.Actress, bool, error) {
	var found models.Actress
	var err error

	if act.DMMID != 0 {
		err = tx.Where("dmm_id = ?", act.DMMID).First(&found).Error
	} else if act.JapaneseName != "" {
		err = tx.Where("japanese_name = ?", act.JapaneseName).First(&found).Error
	} else if act.FirstName != "" && act.LastName != "" {
		err = tx.Where("first_name = ? AND last_name = ?", act.FirstName, act.LastName).First(&found).Error
	} else if act.FirstName != "" {
		err = tx.Where("first_name = ?", act.FirstName).First(&found).Error
	} else {
		err = tx.Where("last_name = ?", act.LastName).First(&found).Error
	}

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return found, false, nil
		}
		return found, false, err
	}
	return found, true, nil
}

func (u *MovieUpserter) ensureActressesExistTx(tx *gorm.DB, actresses []models.Actress) error {
	if len(actresses) == 0 {
		return nil
	}

	var idGroup []actressGroupEntry
	var dmmGroup []actressGroupEntry
	var jpGroup []actressGroupEntry
	var nameGroup []actressGroupEntry

	for i := range actresses {
		a := &actresses[i]
		if a.ID != 0 {
			idGroup = append(idGroup, actressGroupEntry{index: i, act: a})
		} else if a.DMMID != 0 {
			dmmGroup = append(dmmGroup, actressGroupEntry{index: i, act: a})
		} else if a.JapaneseName != "" {
			jpGroup = append(jpGroup, actressGroupEntry{index: i, act: a})
		} else if a.FirstName != "" || a.LastName != "" {
			nameGroup = append(nameGroup, actressGroupEntry{index: i, act: a})
		}
	}

	if len(idGroup) > 0 {
		if err := u.resolveActressGroup(tx, actresses, idGroup, lookupActressByID); err != nil {
			return err
		}
	}

	if len(dmmGroup) > 0 {
		if err := u.resolveActressGroup(tx, actresses, dmmGroup, lookupActressByDMMID); err != nil {
			return err
		}
	}

	if len(jpGroup) > 0 {
		if err := u.resolveActressGroup(tx, actresses, jpGroup, lookupActressByJapaneseName); err != nil {
			return err
		}
	}

	if len(nameGroup) > 0 {
		if err := u.resolveActressGroup(tx, actresses, nameGroup, lookupActressByName); err != nil {
			return err
		}
	}

	return nil
}

func (u *MovieUpserter) persistCreditsTx(tx *gorm.DB, movie *models.Movie) error {
	if movie.SkipCreditReconcile {
		return nil
	}
	policy := NormalizeCollisionPolicy(movie.CreditPolicy)
	trusted := make(map[string]bool, len(movie.TrustedCollisionSources))
	for _, s := range movie.TrustedCollisionSources {
		trusted[strings.TrimSpace(s)] = true
	}
	creditRepo := &MovieCreditRepository{BaseRepository: NewBaseRepository[models.MovieCredit, uint](
		u.repo.GetDB(), "movie credit",
		movieCreditLabel,
	)}
	collisionRepo := &CreditCollisionRepository{BaseRepository: NewBaseRepository[models.CreditCollision, uint](
		u.repo.GetDB(), "credit collision",
		creditCollisionLabel,
	)}
	aliasRepo := NewActressAliasRepository(u.repo.GetDB())

	existing, err := creditRepo.ListByMovieTx(tx, movie.ContentID)
	if err != nil {
		return err
	}
	openCollisions, err := collisionRepo.ListOpenByMovieTx(tx, movie.ContentID)
	if err != nil {
		return err
	}
	reassignments, err := loadCreditReassignmentsTx(tx, movie.ContentID)
	if err != nil {
		return err
	}
	existingByActress := make(map[uint]models.MovieCredit, len(existing))
	for _, ex := range existing {
		existingByActress[ex.ActressID] = ex
	}

	seen := make(map[uint]bool, len(movie.Credits))
	order := 0
	for i := range movie.Credits {
		credit := &movie.Credits[i]
		credit.MovieContentID = movie.ContentID
		if ex, ok := existingByActress[credit.ActressID]; ok && ex.OrderPinned {
			credit.OrderIndex = ex.OrderIndex
		} else {
			credit.OrderIndex = order
		}
		order++

		scraped := credit.Scraped
		if scraped.JapaneseName == "" && scraped.FirstName == "" && scraped.LastName == "" && scraped.DMMID == 0 {
			if credit.Actress != nil {
				scraped = *credit.Actress
			} else {
				scraped = models.Actress{
					DMMID:        resolvedDMMIDFromCredit(credit),
					FirstName:    scrapedFirstName(credit),
					LastName:     scrapedLastName(credit),
					JapaneseName: credit.CreditedJapaneseName,
				}
			}
		}

		resolved, outcome, err := ResolveActressIdentityTx(tx, &scraped)
		if err != nil {
			return err
		}
		sourceActressID := resolved.ID
		if targetActressID, ok := reassignments[sourceActressID]; ok && targetActressID != sourceActressID {
			var target models.Actress
			if err := tx.Where("id = ? AND verified = ?", targetActressID, true).First(&target).Error; err != nil {
				return err
			}
			resolved = &target
			outcome = ResolutionMatched
		}
		credit.ActressID = resolved.ID
		if ex, ok := existingByActress[credit.ActressID]; ok && ex.OrderPinned {
			credit.OrderIndex = ex.OrderIndex
		}
		if err := creditRepo.UpsertTx(tx, credit); err != nil {
			return err
		}
		seen[resolved.ID] = true

		if credit.Suppressed {
			continue
		}

		identityReported := ""
		if outcome == ResolutionAmbiguous {
			identityReported = scraped.FullName()
		}
		if err := collisionRepo.resolveSupersededOpenTx(tx, credit.ID, models.CreditFieldIdentityLink, identityReported); err != nil {
			return err
		}
		if outcome == ResolutionAmbiguous {
			collision := &models.CreditCollision{
				CreditID:       credit.ID,
				MovieContentID: movie.ContentID,
				Field:          models.CreditFieldIdentityLink,
				ReportedValue:  scraped.FullName(),
				CanonicalValue: resolved.FullName(),
			}
			if err := collisionRepo.RecordTx(tx, collision, credit.Source); err != nil {
				return err
			}
			continue
		}

		if resolved.Verified {
			if err := u.recordFieldCollisionsTx(tx, collisionRepo, aliasRepo, credit, resolved, policy, trusted); err != nil {
				return err
			}
		}
	}

	for _, ex := range existing {
		if seen[ex.ActressID] {
			continue
		}
		if ex.EffectiveOrigin() == string(models.CreditOriginUser) || ex.UserOverride || ex.Suppressed || ex.LegacyInferred {
			continue
		}
		pinned := false
		for _, oc := range openCollisions {
			if oc.CreditID == ex.ID {
				pinned = true
				break
			}
		}
		if pinned {
			continue
		}
		if err := collisionRepo.CloseByCreditTx(tx, ex.ID, models.CollisionResolutionByRemoval); err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM credit_collisions WHERE credit_id = ?", ex.ID).Error; err != nil {
			return wrapDBErr("delete collisions", fmt.Sprintf("credit %d", ex.ID), err)
		}
		if err := creditRepo.DeleteTx(tx, movie.ContentID, ex.ActressID); err != nil {
			return err
		}
	}

	surviving, err := creditRepo.ListByMovieTx(tx, movie.ContentID)
	if err != nil {
		return err
	}
	projections := make([]models.Actress, 0, len(surviving))
	for _, credit := range surviving {
		if credit.Suppressed || credit.Actress == nil || !credit.Actress.Verified {
			continue
		}
		projections = append(projections, *credit.Actress)
	}
	movie.Actresses = projections
	if len(projections) > 0 {
		if err := u.ensureActressesExistTx(tx, projections); err != nil {
			return err
		}
	}
	if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
		return err
	}
	return nil
}

func scrapedFirstName(credit *models.MovieCredit) string {
	parts := strings.SplitN(strings.TrimSpace(credit.CreditedName), " ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(credit.CreditedName)
}

func scrapedLastName(credit *models.MovieCredit) string {
	parts := strings.SplitN(strings.TrimSpace(credit.CreditedName), " ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0])
	}
	return ""
}

func resolvedDMMIDFromCredit(credit *models.MovieCredit) int {
	return credit.Scraped.DMMID
}

func aliasMatchesCanonicalTx(tx *gorm.DB, aliasName string, resolved *models.Actress) (bool, error) {
	var aliases []models.ActressAlias
	if err := tx.Where("alias_name = ?", aliasName).Find(&aliases).Error; err != nil {
		return false, wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	canonicalNames := []string{
		resolved.FullName(),
		resolved.JapaneseName,
		resolved.LastName + " " + resolved.FirstName,
		resolved.FirstName + " " + resolved.LastName,
	}
	for _, alias := range aliases {
		aliasKey := models.NormalizeActressNameKey(alias.CanonicalName)
		for _, canonical := range canonicalNames {
			canonicalKey := models.NormalizeActressNameKey(canonical)
			if aliasKey != "" && canonicalKey != "" && aliasKey == canonicalKey {
				return true, nil
			}
		}
	}
	return false, nil
}

func (u *MovieUpserter) recordFieldCollisionsTx(tx *gorm.DB, collisionRepo *CreditCollisionRepository, aliasRepo *ActressAliasRepository, credit *models.MovieCredit, resolved *models.Actress, policy CollisionPolicy, trusted map[string]bool) error {
	type fieldConflict struct {
		field     string
		reported  string
		canonical string
	}
	conflicts := make([]fieldConflict, 0, 2)
	reportedName := credit.CreditedName
	if reportedName == "" {
		reportedName = credit.CreditedJapaneseName
	}
	if reportedName == "" {
		reportedName = credit.Scraped.FullName()
	}
	reportedNameKey := models.NormalizeActressNameKey(reportedName)
	canonicalName := resolved.FullName()
	canonicalNameMatches := false
	for _, canonical := range []string{canonicalName, resolved.JapaneseName} {
		canonicalKey := models.NormalizeActressNameKey(canonical)
		if reportedNameKey != "" && canonicalKey != "" && reportedNameKey == canonicalKey {
			canonicalNameMatches = true
			break
		}
	}
	aliasNameMatches := false
	if reportedNameKey != "" && !canonicalNameMatches {
		var err error
		aliasNameMatches, err = aliasMatchesCanonicalTx(tx, reportedName, resolved)
		if err != nil {
			return err
		}
	}
	if reportedNameKey != "" && !canonicalNameMatches && !aliasNameMatches {
		conflicts = append(conflicts, fieldConflict{field: models.CreditFieldCreditedName, reported: reportedName, canonical: canonicalName})
	}
	reportedThumb := credit.ReportedThumbURL
	if reportedThumb == "" {
		reportedThumb = credit.Scraped.ThumbURL
	}
	if strings.TrimSpace(reportedThumb) != "" && strings.TrimSpace(resolved.ThumbURL) != "" &&
		reportedThumb != resolved.ThumbURL {
		conflicts = append(conflicts, fieldConflict{field: models.CreditFieldReportedThumb, reported: reportedThumb, canonical: resolved.ThumbURL})
	}

	active := make(map[string]string, len(conflicts))
	for _, conflict := range conflicts {
		active[conflict.field] = conflict.reported
	}
	for _, field := range []string{models.CreditFieldCreditedName, models.CreditFieldReportedThumb} {
		if err := collisionRepo.resolveSupersededOpenTx(tx, credit.ID, field, active[field]); err != nil {
			return err
		}
	}

	for _, c := range conflicts {
		collision := &models.CreditCollision{
			CreditID:       credit.ID,
			MovieContentID: credit.MovieContentID,
			Field:          c.field,
			ReportedValue:  c.reported,
			CanonicalValue: c.canonical,
		}
		if err := collisionRepo.RecordTx(tx, collision, credit.Source); err != nil {
			return err
		}
		if collision.Status != models.CollisionStatusOpen {
			continue
		}
		decision := ApplyFieldCollisionPolicy(policy, collision, credit.Source, trusted)
		if !decision.AutoResolved {
			continue
		}
		if err := collisionRepo.ResolveTx(tx, collision.ID, decision.Resolution); err != nil {
			return err
		}
		if decision.Resolution == models.CollisionResolutionAutoKeep || decision.Resolution == models.CollisionResolutionAutoAlias {
			if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).
				Update("display_force_canonical", true).Error; err != nil {
				return err
			}
		}
		if decision.CreateAlias && c.field == models.CreditFieldCreditedName {
			alias := &models.ActressAlias{AliasName: c.reported, CanonicalName: c.canonical}
			if err := aliasRepo.UpsertTx(tx, alias); err != nil {
				return err
			}
		}
	}
	return nil
}
