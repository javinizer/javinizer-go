package database

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Structural audit regression: repeated publication-neutral writes must not
// invalidate an already-rendered movie. The mutation boundary, not callers,
// owns generation comparison so every entry point gets identical semantics.
func TestStructuralAuditNoopCreditOverridePreservesRenderGeneration(t *testing.T) {
	db, service, credit, _ := collisionFixture(t)
	ctx := context.Background()

	require.NoError(t, service.UpdateCreditOverride(ctx, credit.ID, "Pinned Display", true))
	var afterChange int64
	require.NoError(t, db.Model(&credit).Table("movies").Where("content_id = ?", credit.MovieContentID).
		Pluck("render_generation", &afterChange).Error)

	require.NoError(t, service.UpdateCreditOverride(ctx, credit.ID, "Pinned Display", true))
	var afterNoop int64
	require.NoError(t, db.Model(&credit).Table("movies").Where("content_id = ?", credit.MovieContentID).
		Pluck("render_generation", &afterNoop).Error)

	require.Equal(t, afterChange, afterNoop, "same-value override must not stale a staged render")
}

// Structural audit regression: aliases are matching evidence, not render
// inputs. Catalog PUT reaches ActressRepository.Update, so alias-only edits
// must persist without staling every credited movie.
func TestStructuralAuditAliasOnlyCatalogUpdatePreservesRenderGeneration(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	ctx := context.Background()
	repo := NewActressRepository(db)

	actress, err := repo.FindByID(ctx, credit.ActressID)
	require.NoError(t, err)
	actress.Aliases = "Historical Alias"
	require.NoError(t, repo.Update(ctx, actress))

	var movieGeneration int64
	require.NoError(t, db.Table("movies").Where("content_id = ?", credit.MovieContentID).
		Pluck("render_generation", &movieGeneration).Error)
	require.Zero(t, movieGeneration, "matching-only catalog edits must not stale rendered artifacts")
}

// Structural audit regression: translation provenance controls cache freshness,
// but only translated text is rendered. Rewriting provenance alone must not
// invalidate an artifact generated from identical text.
func TestStructuralAuditTranslationProvenanceOnlyUpsertPreservesRenderGeneration(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := context.Background()
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ContentID: "translation-provenance", ID: "translation-provenance", Title: "Title",
		Translations: []models.MovieTranslation{{
			Language: "en", Title: "Translated", SourceName: "provider-a", SettingsHash: "hash-a",
		}},
	}
	_, err := repo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)

	movie.Translations[0].SourceName = "provider-b"
	movie.Translations[0].SettingsHash = "hash-b"
	updated, err := repo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)
	require.Zero(t, updated.RenderGeneration, "translation provenance is not an artifact input")
}

func TestArtifactRenderProjectionFieldClassification(t *testing.T) {
	base := func() *models.Movie {
		actress := &models.Actress{ID: 7, FirstName: "Canonical", Verified: true}
		return &models.Movie{
			ContentID: "projection", ID: "projection", Title: "Title",
			Translations: []models.MovieTranslation{{Language: "en", Title: "Translated", SourceName: "provider-a", SettingsHash: "hash-a"}},
			Credits:      []models.MovieCredit{{ActressID: actress.ID, Actress: actress, CreditedName: "Credited", ReportedThumbURL: "reported", Origin: "scrape"}},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*models.Movie)
		changed bool
	}{
		{"translation output", func(m *models.Movie) { m.Translations[0].Title = "Changed" }, true},
		{"translation source provenance", func(m *models.Movie) { m.Translations[0].SourceName = "provider-b" }, false},
		{"translation settings provenance", func(m *models.Movie) { m.Translations[0].SettingsHash = "hash-b" }, false},
		{"credit source provenance", func(m *models.Movie) { m.Credits[0].Origin = "user" }, false},
		{"reported thumb collision evidence", func(m *models.Movie) { m.Credits[0].ReportedThumbURL = "other" }, false},
		{"canonical display name", func(m *models.Movie) { m.Credits[0].Actress.FirstName = "Changed" }, true},
		{"display selection", func(m *models.Movie) { m.Credits[0].DisplayForceCanonical = true }, true},
		{"eligibility", func(m *models.Movie) { m.Credits[0].Actress.Verified = false }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := base(), base()
			tc.mutate(after)
			require.Equal(t, tc.changed, !reflect.DeepEqual(artifactRenderProjection(before), artifactRenderProjection(after)))
		})
	}
}

func TestDirectCreditMutationUsesAtomicProjectionInvalidation(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	repo := NewMovieCreditRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.UpdateOrderPinned(ctx, credit.ID, 4, true))
	var movie models.Movie
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
	require.True(t, movie.RenderDirty)
	require.EqualValues(t, 1, movie.RenderGeneration)

	require.NoError(t, repo.UpdateOrderPinned(ctx, credit.ID, 4, true))
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
	require.EqualValues(t, 1, movie.RenderGeneration)

	require.NoError(t, repo.SetDisplayForceCanonical(ctx, credit.ID, true))
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
	require.True(t, movie.RenderDirty)
	require.EqualValues(t, 2, movie.RenderGeneration)
}

func TestRenderInvalidationHelperGuardsAndFailures(t *testing.T) {
	t.Run("normalization", func(t *testing.T) {
		require.Equal(t, []string{"a", "b"}, uniqueContentIDs([]string{"", "b", "a", "b"}))
		ids, err := movieContentIDsForActressesTx(newCreditTestDB(t).DB)
		require.NoError(t, err)
		require.Nil(t, ids)
	})
	t.Run("missing credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		_, err := movieContentIDForCreditTx(db.DB, 999)
		require.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("missing movies are skipped", func(t *testing.T) {
		db := newCreditTestDB(t)
		snapshots, err := captureMovieRenderSnapshotsTx(db.DB, []string{"missing"})
		require.NoError(t, err)
		require.Empty(t, snapshots)
		before := movieRenderSnapshot{"deleted": {projection: movieRenderInputs{}, renderGeneration: 2}}
		require.NoError(t, invalidateChangedMovieRenderInputsTx(db.DB, before, []string{"deleted"}))
	})
	t.Run("snapshot movie read", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.Movie{}))
		_, err := captureMovieRenderSnapshotsTx(db.DB, []string{"broken"})
		require.Error(t, err)
	})
	t.Run("snapshot eligibility read", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Create(&models.Movie{ContentID: "snapshot", ID: "snapshot"}).Error)
		require.NoError(t, db.Migrator().DropTable(&models.CreditCollision{}))
		_, err := captureMovieRenderSnapshotsTx(db.DB, []string{"snapshot"})
		require.Error(t, err)
	})
	t.Run("reload movie read", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Create(&models.Movie{ContentID: "reload", ID: "reload"}).Error)
		before, err := captureMovieRenderSnapshotsTx(db.DB, []string{"reload"})
		require.NoError(t, err)
		require.NoError(t, db.Migrator().DropTable(&models.Movie{}))
		require.Error(t, invalidateChangedMovieRenderInputsTx(db.DB, before, []string{"reload"}))
	})
	t.Run("reload eligibility read", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Create(&models.Movie{ContentID: "eligibility", ID: "eligibility"}).Error)
		before, err := captureMovieRenderSnapshotsTx(db.DB, []string{"eligibility"})
		require.NoError(t, err)
		require.NoError(t, db.Migrator().DropTable(&models.CreditCollision{}))
		require.Error(t, invalidateChangedMovieRenderInputsTx(db.DB, before, []string{"eligibility"}))
	})
	t.Run("mutation failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		sentinel := errors.New("mutation failed")
		err := mutateMovieRenderInputsTx(db.DB, nil, func() error { return sentinel })
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("generation guard", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, invalidateMovieRenderGenerationTx(db.DB, nil, &models.Movie{}))
		require.NoError(t, invalidateMovieRenderGenerationTx(db.DB, &models.Movie{}, nil))
		require.NoError(t, invalidateMovieRenderGenerationTx(db.DB, &models.Movie{RenderGeneration: 1}, &models.Movie{RenderGeneration: 2}))
	})
}

func TestStructuralMutationBoundaryFailures(t *testing.T) {
	type boundary struct {
		name string
		run  func(context.Context, *ActressRepository, models.Actress) error
	}
	boundaries := []boundary{
		{"update", func(ctx context.Context, r *ActressRepository, a models.Actress) error {
			a.Aliases = "changed"
			return r.Update(ctx, &a)
		}},
		{"rename", func(ctx context.Context, r *ActressRepository, a models.Actress) error {
			return r.RenameNameFields(ctx, a.ID, "changed", a.LastName, a.JapaneseName)
		}},
		{"delete", func(ctx context.Context, r *ActressRepository, a models.Actress) error { return r.Delete(ctx, a.ID) }},
		{"promote", func(ctx context.Context, r *ActressRepository, a models.Actress) error {
			return r.PromoteCandidate(ctx, a.ID, "changed", a.LastName, a.JapaneseName, a.ThumbURL)
		}},
		{"canonical", func(ctx context.Context, r *ActressRepository, a models.Actress) error {
			return r.UpdateCanonicalFields(ctx, a.ID, "changed", a.LastName, a.JapaneseName, a.ThumbURL)
		}},
		{"import", func(ctx context.Context, r *ActressRepository, a models.Actress) error {
			a.FirstName = "imported"
			a.Origin = ActressOriginScrape
			a.Verified = false
			return r.ImportUpsert(ctx, &a)
		}},
	}
	for _, tc := range boundaries {
		t.Run(tc.name+" affected-key read", func(t *testing.T) {
			db, _, credit, _ := collisionFixture(t)
			var actress models.Actress
			require.NoError(t, db.First(&actress, credit.ActressID).Error)
			injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
			require.Error(t, tc.run(t.Context(), NewActressRepository(db), actress))
		})
		t.Run(tc.name+" snapshot read", func(t *testing.T) {
			db, _, credit, _ := collisionFixture(t)
			var actress models.Actress
			require.NoError(t, db.First(&actress, credit.ActressID).Error)
			injectDatabaseCallbackError(t, db, "query", "movies", 1)
			require.Error(t, tc.run(t.Context(), NewActressRepository(db), actress))
		})
	}

	t.Run("canonical missing row", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.Error(t, NewActressRepository(db).UpdateCanonicalFields(t.Context(), 999, "x", "", "", ""))
	})
	for _, fault := range []struct{ operation, table string }{{"update", "actresses"}, {"create", "actress_aliases"}, {"query", "credit_collisions"}} {
		t.Run("canonical "+fault.operation+" "+fault.table, func(t *testing.T) {
			db, _, credit, _ := collisionFixture(t)
			injectDatabaseCallbackError(t, db, fault.operation, fault.table, 1)
			err := NewActressRepository(db).UpdateCanonicalFields(t.Context(), credit.ActressID, "Changed", "Name", "", "thumb")
			require.Error(t, err)
		})
	}
}

func TestUpdateCanonicalFieldsProjectionFailureRollsBack(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_projection BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
	err := NewActressRepository(db).UpdateCanonicalFields(t.Context(), credit.ActressID, "Changed", "Name", "", "thumb")
	require.Error(t, err)
}

func TestStructuralCollisionAndWrapperFailures(t *testing.T) {
	t.Run("resolve collision reload", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity, 0)
		require.Error(t, err)
	})
	t.Run("resolve credit reload", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity, 0)
		require.Error(t, err)
	})
	t.Run("resolve remaining count", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 2)
		_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity, 0)
		require.Error(t, err)
	})
	t.Run("resolve canonical projection", func(t *testing.T) {
		db, service, _, collision := collisionFixture(t)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_resolve_projection BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionAdoptCanonical, 0)
		require.Error(t, err)
	})
	t.Run("suppression missing and read failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.ErrorIs(t, setCreditSuppressedTx(db.DB, 999, true), ErrNotFound)
		db, service, credit, _ := collisionFixture(t)
		require.NoError(t, service.SetCreditSuppressed(t.Context(), credit.ID, true))
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, service.SetCreditSuppressed(t.Context(), credit.ID, false))
	})
	t.Run("direct wrapper missing credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.ErrorIs(t, NewMovieCreditRepository(db).UpdateOverride(t.Context(), 999, "x", true), ErrNotFound)
	})
	t.Run("reassign empty content id", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		copy := credit
		copy.MovieContentID = ""
		require.NoError(t, NewMovieCreditRepository(db).ReassignCredit(t.Context(), &copy, target.ID))
		copy.ID = 999
		require.Error(t, NewMovieCreditRepository(db).ReassignCredit(t.Context(), &copy, target.ID))
	})
	t.Run("reassign target-credit query", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 3)
		require.Error(t, NewMovieCreditRepository(db).ReassignCredit(t.Context(), &credit, target.ID))
	})
	t.Run("generic movie update rollback", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := models.Movie{ContentID: "generic-update", ID: "generic-update", Title: "before"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_generic_update BEFORE UPDATE OF title ON movies BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		movie.Title = "after"
		require.Error(t, NewMovieRepository(db).Update(t.Context(), &movie))
	})
	t.Run("merge credit failure after snapshot", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		target, source := models.Actress{FirstName: "Target", Verified: true}, models.Actress{FirstName: "Source", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&source).Error)
		movie := models.Movie{ContentID: "merge-credit-fault", ID: "merge-credit-fault"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID}).Error)
		plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
		require.NoError(t, err)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 3)
		_, err = repo.merger.ExecuteMerge(t.Context(), plan, db)
		require.Error(t, err)
	})
}

func TestStructuralResidualFaultBranches(t *testing.T) {
	t.Run("canonical collision reconcile", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 2)
		require.Error(t, NewActressRepository(db).UpdateCanonicalFields(t.Context(), credit.ActressID, "Changed", "Name", "", "thumb"))
	})
	t.Run("import projection restore", func(t *testing.T) {
		db := newCreditTestDB(t)
		candidate := models.Actress{FirstName: "Candidate", Verified: false, Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&candidate).Error)
		movie := models.Movie{ContentID: "import-projection", ID: "import-projection"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_import_projection BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		incoming := candidate
		incoming.FirstName = "Imported"
		require.Error(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	})
	t.Run("suppression credit read", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Close())
		require.Error(t, setCreditSuppressedTx(db.DB, 1, true))
	})
	t.Run("suppression collision restore read", func(t *testing.T) {
		db, service, credit, _ := collisionFixture(t)
		require.NoError(t, service.SetCreditSuppressed(t.Context(), credit.ID, true))
		require.NoError(t, db.Preload("Actress").First(&credit, credit.ID).Error)
		injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
		require.Error(t, restoreSuppressedCreditCollisionsTx(db.DB, &credit))
	})
	t.Run("reassign target credit read", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true}
		require.NoError(t, db.Create(&target).Error)
		injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
		require.Error(t, reassignCreditTx(db.DB, &credit, target.ID))
	})
	t.Run("override success block", func(t *testing.T) {
		_, service, credit, _ := collisionFixture(t)
		require.NoError(t, service.UpdateCreditOverride(t.Context(), credit.ID, "covered", true))
	})
}

func TestMovieUpsertRenderStateReloadFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	movie := &models.Movie{ContentID: "render-state-reload", ID: "render-state-reload", Title: "before"}
	_, err := repo.Upsert(t.Context(), movie)
	require.NoError(t, err)
	movie.Title = "after"
	injectDatabaseCallbackError(t, db, "query", "movies", 5)
	_, err = repo.Upsert(t.Context(), movie)
	require.Error(t, err)
}
