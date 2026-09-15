package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreditRepositoriesLifecycle(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	ctx := context.Background()
	credits, collisions := service.Credits, service.Collisions
	found, err := credits.FindByMovieAndActress(ctx, credit.MovieContentID, credit.ActressID)
	require.NoError(t, err)
	require.Equal(t, credit.ID, found.ID)
	_, err = credits.FindByMovieAndActress(ctx, "missing", credit.ActressID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = credits.FindByCreditID(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, credits.UpdateOrderPinned(ctx, credit.ID, 4, true))
	require.NoError(t, credits.SetDisplayForceCanonical(ctx, credit.ID, true))
	require.NoError(t, credits.MarkMovieDirty(ctx, credit.MovieContentID))
	list, err := credits.ListByActress(ctx, credit.ActressID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].OrderPinned)
	require.Equal(t, 4, list[0].OrderIndex)
	require.True(t, list[0].DisplayForceCanonical)
	count, err := credits.CountByActress(ctx, credit.ActressID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	open, err := collisions.ListOpenByActress(ctx, credit.ActressID)
	require.NoError(t, err)
	require.Len(t, open, 1)
	counts, err := collisions.CountOpenByMovieBatch(ctx, []string{credit.MovieContentID, "missing"})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[credit.MovieContentID])
	require.Zero(t, counts["missing"])
	has, err := collisions.HasOpenForMovie(ctx, credit.MovieContentID)
	require.NoError(t, err)
	require.True(t, has)
	require.NoError(t, collisions.Resolve(ctx, collision.ID, models.CollisionResolutionKeepIdentity))
	has, err = collisions.HasOpenForMovie(ctx, credit.MovieContentID)
	require.NoError(t, err)
	require.False(t, has)
	require.NoError(t, collisions.Reopen(ctx, collision.ID))
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.True(t, collision.UserPinned)
	require.Empty(t, collision.Resolution)
	collision.CanonicalValue = "New Truth"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return collisions.RecordTx(tx, &collision, "javdb") }))
	require.Equal(t, 2, collision.Occurrences)
	require.Equal(t, "dmm,javdb", collision.SourcesSeen)
	require.NoError(t, collisions.CloseByCredit(ctx, credit.ID, models.CollisionResolutionByRemoval))
	open, err = collisions.ListOpenByMovie(ctx, credit.MovieContentID)
	require.NoError(t, err)
	require.Empty(t, open)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return credits.DeleteTx(tx, credit.MovieContentID, credit.ActressID) }))
	count, err = credits.CountByActress(ctx, credit.ActressID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRecordTxReopensAutomaticallyRemovedCollision(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	collision.Status = models.CollisionStatusResolved
	collision.Resolution = models.CollisionResolutionByRemoval
	require.NoError(t, db.Save(&collision).Error)
	incoming := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: credit.MovieContentID,
		Field:          collision.Field,
		ReportedValue:  collision.ReportedValue,
		CanonicalValue: collision.CanonicalValue,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return service.Collisions.RecordTx(tx, &incoming, "javdb") }))
	require.Equal(t, models.CollisionStatusOpen, incoming.Status)
	require.Empty(t, incoming.Resolution)
	require.Equal(t, 2, incoming.Occurrences)

	incoming.Status = models.CollisionStatusResolved
	incoming.Resolution = models.CollisionResolutionKeepIdentity
	require.NoError(t, db.Save(&incoming).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return service.Collisions.RecordTx(tx, &incoming, "dmm") }))
	require.Equal(t, models.CollisionStatusResolved, incoming.Status)
	require.Equal(t, models.CollisionResolutionKeepIdentity, incoming.Resolution)
}

func TestCreditRepositoriesCancellationErrors(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, c := service.Credits, service.Collisions
	_, err := r.ListByMovie(ctx, "x")
	require.Error(t, err)
	_, err = r.FindByCreditID(ctx, credit.ID)
	require.Error(t, err)
	_, err = r.FindByMovieAndActress(ctx, credit.MovieContentID, credit.ActressID)
	require.Error(t, err)
	_, err = r.ListByActress(ctx, credit.ActressID)
	require.Error(t, err)
	_, err = r.CountByActress(ctx, credit.ActressID)
	require.Error(t, err)
	require.Error(t, r.UpdateOverride(ctx, credit.ID, "x", true))
	require.Error(t, r.UpdateSuppressed(ctx, credit.ID, true))
	require.Error(t, r.UpdateOrderPinned(ctx, credit.ID, 1, true))
	require.Error(t, r.SetDisplayForceCanonical(ctx, credit.ID, true))
	require.Error(t, r.MarkMovieDirty(ctx, credit.MovieContentID))
	_, err = c.ListOpenByMovie(ctx, "x")
	require.Error(t, err)
	_, err = c.ListOpenByActress(ctx, credit.ActressID)
	require.Error(t, err)
	_, err = c.CountOpenByMovieBatch(ctx, []string{"x"})
	require.Error(t, err)
	_, err = c.HasOpenForMovie(ctx, "x")
	require.Error(t, err)
	require.Error(t, c.Resolve(ctx, collision.ID, "keep_identity"))
	require.Error(t, c.Reopen(ctx, collision.ID))
	tx := db.WithContext(ctx)
	_, err = r.ListByMovieTx(tx, "x")
	require.Error(t, err)
	_, err = c.ListOpenByMovieTx(tx, "x")
	require.Error(t, err)
	require.Error(t, r.DeleteTx(tx, "x", 1))
	require.Error(t, r.DeleteByIDTx(tx, credit.ID))
	require.Error(t, r.UpsertTx(tx, &credit))
	require.Error(t, c.RecordTx(tx, &collision, "x"))
	require.Error(t, c.ResolveTx(tx, collision.ID, "keep_identity"))
}

func TestImportUpsertPromotesDMMlessCandidateByNameKey(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{
		FirstName: "NameOnly",
		LastName:  "Candidate",
		Verified:  false,
		Origin:    ActressOriginScrape,
		NameKey:   models.NormalizeActressNameKey("Candidate NameOnly"),
	}
	require.NoError(t, repo.Create(context.Background(), &candidate))

	incoming := models.Actress{FirstName: "NameOnly", LastName: "Candidate"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, candidate.ID, incoming.ID)
	require.True(t, incoming.Verified)
	require.Equal(t, ActressOriginImport, incoming.Origin)

	stored, err := repo.FindByID(context.Background(), candidate.ID)
	require.NoError(t, err)
	require.True(t, stored.Verified)
	require.Equal(t, ActressOriginImport, stored.Origin)
}

func TestImportUpsertMatchesDMMBackedCandidateByUniqueName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{
		DMMID:     987650,
		FirstName: "NameOnly",
		LastName:  "DMMCandidate",
		Verified:  false,
		Origin:    ActressOriginScrape,
	}
	require.NoError(t, repo.Create(context.Background(), &candidate))
	movie := models.Movie{ContentID: "dmm-candidate-import", ID: "dmm-candidate-import"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{
		CreditID: credit.ID, MovieContentID: movie.ContentID,
		Field: models.CreditFieldIdentityLink, ReportedValue: "DMMCandidate NameOnly",
		CanonicalValue: "DMMCandidate NameOnly", Status: models.CollisionStatusOpen,
	}
	require.NoError(t, db.Create(&collision).Error)

	incoming := models.Actress{FirstName: "NameOnly", LastName: "DMMCandidate"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, candidate.ID, incoming.ID)
	require.Equal(t, candidate.DMMID, incoming.DMMID)
	require.True(t, incoming.Verified)

	var storedCredit models.MovieCredit
	require.NoError(t, db.First(&storedCredit, credit.ID).Error)
	require.Equal(t, candidate.ID, storedCredit.ActressID)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.ElementsMatch(t, []uint{candidate.ID}, actressIDs)
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
	require.True(t, storedMovie.RenderDirty)
	var storedCollision models.CreditCollision
	require.NoError(t, db.First(&storedCollision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, storedCollision.Status)
	require.Equal(t, models.CollisionResolutionKeepIdentity, storedCollision.Resolution)
}

func TestImportUpsertRollsBackCandidatePromotionOnProjectionFailure(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{
		DMMID:     987651,
		FirstName: "Atomic",
		LastName:  "Candidate",
		Origin:    ActressOriginScrape,
	}
	require.NoError(t, repo.Create(context.Background(), &candidate))
	movie := models.Movie{ContentID: "atomic-import-promotion", ID: "atomic-import-promotion"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_import_projection_dirty BEFORE UPDATE OF render_dirty ON movies BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_import_projection_dirty").Error })

	incoming := models.Actress{FirstName: candidate.FirstName, LastName: candidate.LastName}
	require.Error(t, repo.ImportUpsert(context.Background(), &incoming))
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.False(t, stored.Verified)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.Empty(t, actressIDs)
}

func TestImportUpsertRollsBackCandidatePromotionOnCollisionCleanupFailure(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{DMMID: 987652, FirstName: "Collision", LastName: "Candidate", Origin: ActressOriginScrape}
	require.NoError(t, repo.Create(context.Background(), &candidate))
	require.NoError(t, db.Migrator().DropTable(&models.CreditCollision{}))

	incoming := models.Actress{FirstName: candidate.FirstName, LastName: candidate.LastName}
	require.Error(t, repo.ImportUpsert(context.Background(), &incoming))
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.False(t, stored.Verified)
}

func TestImportUpsertIDLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	injectDatabaseCallbackError(t, db, "query", "actresses", 1)
	require.Error(t, repo.ImportUpsert(context.Background(), &models.Actress{ID: 999999, FirstName: "Name"}))
}

func TestImportUpsertMatchesVerifiedDMMlessIdentityForDMMID(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	existing := models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repo.Create(context.Background(), &existing))

	incoming := models.Actress{DMMID: 987665, FirstName: "Same", LastName: "Person"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, existing.ID, incoming.ID)

	stored, err := repo.FindByID(context.Background(), existing.ID)
	require.NoError(t, err)
	require.Equal(t, 0, stored.DMMID)
}

func TestImportUpsertDoesNotMergeDMMBackedHomonym(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	existing := models.Actress{DMMID: 987660, FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repo.Create(context.Background(), &existing))

	incoming := models.Actress{DMMID: 987661, FirstName: "Same", LastName: "Person"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.NotEqual(t, existing.ID, incoming.ID)
	require.Equal(t, 987661, incoming.DMMID)

	stored, err := repo.FindByID(context.Background(), existing.ID)
	require.NoError(t, err)
	require.Equal(t, 987660, stored.DMMID)
}

func TestImportUpsertMatchesByExistingIDBeforeName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	existing := models.Actress{DMMID: 987670, FirstName: "Original", LastName: "Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repo.Create(context.Background(), &existing))

	incoming := models.Actress{ID: existing.ID, FirstName: "Edited", LastName: "Names"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, existing.ID, incoming.ID)
	require.Equal(t, existing.DMMID, incoming.DMMID)

	stored, err := repo.FindByID(context.Background(), existing.ID)
	require.NoError(t, err)
	require.Equal(t, "Original", stored.FirstName)
	require.Equal(t, "Name", stored.LastName)
}

func TestImportUpsertLeavesAmbiguousDMMBackedCandidatesUnmatched(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	for _, dmmID := range []int{987651, 987652} {
		candidate := models.Actress{
			DMMID:     dmmID,
			FirstName: "Same",
			LastName:  "Candidate",
			Verified:  false,
			Origin:    ActressOriginScrape,
		}
		require.NoError(t, repo.Create(context.Background(), &candidate))
	}

	incoming := models.Actress{FirstName: "Same", LastName: "Candidate"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.NotEqual(t, 0, incoming.ID)
	require.NotEqual(t, 987651, incoming.DMMID)
	require.True(t, incoming.Verified)
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Where("verified = ?", true).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestFindDMMCandidateByExactNameQueryError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	injectDatabaseCallbackError(t, db, "query", "actresses", 1)
	_, err := repo.findDMMCandidateByExactName(context.Background(), &models.Actress{FirstName: "Name", LastName: "Candidate"})
	require.Error(t, err)
}

func TestFindImportMatchDMMCandidateLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	injectDatabaseCallbackError(t, db, "query", "actresses", 3)
	_, err := repo.findImportMatch(context.Background(), &models.Actress{FirstName: "Name", LastName: "Candidate"})
	require.Error(t, err)
}

func TestFindImportMatchWithoutName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	found, err := repo.findImportMatch(context.Background(), &models.Actress{})
	require.NoError(t, err)
	require.Nil(t, found)
}

func TestExactActressNamesMatch(t *testing.T) {
	tests := []struct {
		name  string
		left  *models.Actress
		right *models.Actress
		want  bool
	}{
		{"nil left", nil, &models.Actress{}, false},
		{"nil right", &models.Actress{}, nil, false},
		{"japanese", &models.Actress{JapaneseName: "名前"}, &models.Actress{JapaneseName: " 名前 "}, true},
		{"western", &models.Actress{FirstName: "Name", LastName: "Candidate"}, &models.Actress{FirstName: "Name", LastName: "Candidate"}, true},
		{"western mismatch", &models.Actress{FirstName: "Name", LastName: "Candidate"}, &models.Actress{FirstName: "Other", LastName: "Candidate"}, false},
		{"first only", &models.Actress{FirstName: "Name"}, &models.Actress{FirstName: "Name"}, true},
		{"last only", &models.Actress{LastName: "Candidate"}, &models.Actress{LastName: "Candidate"}, true},
		{"empty", &models.Actress{}, &models.Actress{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, exactActressNamesMatch(tt.left, tt.right))
		})
	}
}

func TestImportUpsertPromotesDMMlessCandidateWithDMMID(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{
		FirstName: "NameOnly",
		LastName:  "DMMCandidate",
		Verified:  false,
		Origin:    ActressOriginScrape,
		NameKey:   models.NormalizeActressNameKey("DMMCandidate NameOnly"),
	}
	require.NoError(t, repo.Create(context.Background(), &candidate))

	incoming := models.Actress{DMMID: 987654, FirstName: "NameOnly", LastName: "DMMCandidate"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, candidate.ID, incoming.ID)
	require.Equal(t, 987654, incoming.DMMID)
	require.True(t, incoming.Verified)
	require.Equal(t, ActressOriginImport, incoming.Origin)

	stored, err := repo.FindByID(context.Background(), candidate.ID)
	require.NoError(t, err)
	require.Equal(t, 987654, stored.DMMID)
	require.True(t, stored.Verified)
}

func TestImportUpsertPreservesDMMIDForProtectedIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	existing := models.Actress{DMMID: 24680, FirstName: "Protected", LastName: "Identity", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, repo.Create(context.Background(), &existing))

	incoming := models.Actress{FirstName: "Protected", LastName: "Identity", ThumbURL: "import-thumb"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.Equal(t, existing.ID, incoming.ID)
	require.Equal(t, existing.DMMID, incoming.DMMID)

	stored, err := repo.FindByID(context.Background(), existing.ID)
	require.NoError(t, err)
	require.Equal(t, existing.DMMID, stored.DMMID)
}

func TestImportUpsertCandidateLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	injectDatabaseCallbackError(t, db, "query", "actresses", 2)
	incoming := models.Actress{FirstName: "Candidate", LastName: "Only"}
	require.Error(t, repo.ImportUpsert(context.Background(), &incoming))
}

func TestIdentityCatalogOwnershipAndImports(t *testing.T) {
	db, service, credit, _ := collisionFixture(t)
	r := service.Actresses
	ctx := context.Background()
	require.NoError(t, r.UpdateCanonicalFields(ctx, credit.ActressID, "Edited", "Person", "名前", "thumb"))
	a, err := r.FindByID(ctx, credit.ActressID)
	require.NoError(t, err)
	require.Equal(t, "Edited", a.FirstName)
	require.True(t, a.Verified)
	require.NoError(t, r.SetUserOwned(ctx, a.ID))
	require.Error(t, r.ImportUpsert(ctx, nil))
	candidate := models.Actress{DMMID: 789, FirstName: "Candidate", LastName: "Scrape", Origin: ActressOriginScrape}
	require.NoError(t, r.Create(ctx, &candidate))
	candidateImport := models.Actress{DMMID: candidate.DMMID, FirstName: "Imported", LastName: "Person"}
	require.NoError(t, r.ImportUpsert(ctx, &candidateImport))
	require.True(t, candidateImport.Verified)
	require.Equal(t, ActressOriginImport, candidateImport.Origin)

	incoming := models.Actress{DMMID: 123, FirstName: "Import", LastName: "Person", JapaneseName: "輸入", ThumbURL: "import-thumb"}
	require.NoError(t, r.ImportUpsert(ctx, &incoming))
	require.True(t, incoming.Verified)
	require.Equal(t, ActressOriginImport, incoming.Origin)
	replacement := models.Actress{DMMID: 123, FirstName: "Wrong", LastName: "Wrong", JapaneseName: "違う", ThumbURL: "wrong"}
	require.NoError(t, r.ImportUpsert(ctx, &replacement))
	require.Equal(t, incoming.ID, replacement.ID)
	require.Equal(t, "Import", replacement.FirstName)
	require.Equal(t, "import-thumb", replacement.ThumbURL)
	byName := models.Actress{FirstName: "Import", LastName: "Person"}
	require.NoError(t, r.ImportUpsert(ctx, &byName))
	require.Equal(t, incoming.ID, byName.ID)
	empty := models.Actress{DMMID: 456, Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&empty).Error)
	fill := models.Actress{DMMID: 456, FirstName: "Filled", LastName: "Name", JapaneseName: "補完", ThumbURL: "filled"}
	require.NoError(t, r.ImportUpsert(ctx, &fill))
	require.Equal(t, empty.ID, fill.ID)
	require.Equal(t, "Filled", fill.FirstName)
	require.Equal(t, "filled", fill.ThumbURL)
	_, err = r.FindVerifiedByDMMID(ctx, 0)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.FindVerifiedByDMMID(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.FindVerifiedByAlias(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)
	for _, canonical := range []string{"", "absent", "Import Person"} {
		alias := models.ActressAlias{AliasName: "alias-" + canonical, CanonicalName: canonical}
		require.NoError(t, db.Create(&alias).Error)
		found, err := r.FindVerifiedByAlias(ctx, alias.AliasName)
		if canonical == "Import Person" {
			require.NoError(t, err)
			require.Equal(t, incoming.ID, found.ID)
		} else {
			require.ErrorIs(t, err, ErrNotFound)
		}
	}
	matches, err := r.FindVerifiedByExactName(ctx, "", "", "")
	require.NoError(t, err)
	require.Empty(t, matches)
	matches, err = r.FindVerifiedByExactName(ctx, "名前", "", "")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	candidates, err := r.ListCandidates(ctx, 0, 0)
	require.NoError(t, err)
	require.Empty(t, candidates)
	count, err := r.CountCandidates(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	translations, err := r.FreshTranslationsByActress(ctx, a.ID)
	require.NoError(t, err)
	require.Empty(t, translations)
	r.markCreditingMoviesDirty(ctx, 0)
}

func TestIdentityCatalogCancellationErrors(t *testing.T) {
	_, service, credit, _ := collisionFixture(t)
	r := service.Actresses
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.FindVerifiedByDMMID(ctx, 1)
	require.Error(t, err)
	_, err = r.FindVerifiedByAlias(ctx, "x")
	require.Error(t, err)
	_, err = r.FindVerifiedByExactName(ctx, "x", "", "")
	require.Error(t, err)
	_, err = r.ListCandidates(ctx, 1, 0)
	require.Error(t, err)
	_, err = r.CountCandidates(ctx)
	require.Error(t, err)
	require.Error(t, r.PromoteCandidate(ctx, credit.ActressID, "a", "b", "c", "d"))
	require.Error(t, r.SetUserOwned(ctx, credit.ActressID))
	require.Error(t, r.UpdateCanonicalFields(ctx, credit.ActressID, "a", "b", "c", "d"))
	require.Error(t, r.ImportUpsert(ctx, &models.Actress{DMMID: 1}))
	require.Error(t, r.ImportUpsert(ctx, &models.Actress{FirstName: "x"}))
	_, err = r.DeleteStaleCandidates(ctx, time.Now())
	require.Error(t, err)
	_, err = r.FreshTranslationsByActress(ctx, credit.ActressID)
	require.Error(t, err)
	r.markCreditingMoviesDirty(ctx, credit.ActressID)
}
