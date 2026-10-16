package database

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportCanonicalEnrichmentReconcilesCollisionsAcrossRestart(t *testing.T) {
	ctx := t.Context()
	dsn := filepath.Join(t.TempDir(), "import-lifecycle.db")
	open := func() *DB {
		db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		return db
	}
	db := open()
	t.Cleanup(func() { _ = db.Close() })
	actress := models.Actress{DMMID: 891001, FirstName: "Yui", LastName: "Hatano"}
	require.NoError(t, NewActressRepository(db).Create(ctx, &actress))
	scrape := func() *models.Movie {
		return creditMovie("import-durable", []models.MovieCredit{{
			CreditedName: "波多野結衣", Source: "fixture",
			Scraped: models.Actress{DMMID: actress.DMMID, JapaneseName: "波多野結衣"},
		}})
	}
	before, err := NewMovieRepository(db).Upsert(ctx, scrape())
	require.NoError(t, err)
	rows, err := NewCreditCollisionRepository(db).ListOpenByMovie(ctx, before.ContentID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	collisionID := rows[0].ID
	enrich := func() {
		incoming := models.Actress{DMMID: actress.DMMID, FirstName: "Wrong", LastName: "Wrong", JapaneseName: "波多野結衣"}
		require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
		require.Equal(t, actress.ID, incoming.ID)
		require.Equal(t, "Hatano Yui", incoming.FullName())
		require.Equal(t, ActressOriginUser, incoming.Origin)
	}
	enrich()
	for _, stage := range []string{"after import", "after repeated import", "after restart", "after identical rescrape"} {
		switch stage {
		case "after repeated import":
			enrich()
		case "after restart":
			require.NoError(t, db.Close())
			db = open()
		case "after identical rescrape":
			_, err = NewMovieRepository(db).Upsert(ctx, scrape())
			require.NoError(t, err)
		}
		movie, err := NewMovieRepository(db).FindByContentID(ctx, before.ContentID)
		require.NoError(t, err, stage)
		require.Equal(t, "波多野結衣", movie.Credits[0].Actress.JapaneseName, stage)
		require.Equal(t, "波多野結衣", movie.Credits[0].CreditedName, "reported evidence must not change")
		require.Equal(t, before.RenderGeneration+1, movie.RenderGeneration, stage)
		require.Equal(t, stage == "after import", movie.RenderDirty, stage)
		rows, err = NewCreditCollisionRepository(db).ListOpenByMovie(ctx, movie.ContentID)
		require.NoError(t, err)
		require.Empty(t, rows, stage)
		var collision models.CreditCollision
		require.NoError(t, db.First(&collision, collisionID).Error)
		require.Equal(t, models.CollisionStatusResolved, collision.Status, stage)
		require.Equal(t, models.CollisionResolutionAdoptCanonical, collision.Resolution, stage)
		called := false
		err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, movie.ContentID, movie.RenderGeneration, func(*models.Movie) error {
			called = true
			return nil
		})
		require.NoError(t, err, stage)
		require.True(t, called, stage)
	}
}

func TestImportCanonicalEnrichmentPreservesAcceptedAliases(t *testing.T) {
	for _, origin := range []string{ActressOriginUser, ActressOriginImport} {
		t.Run(origin, func(t *testing.T) {
			db := newCreditTestDB(t)
			ctx := t.Context()
			repo := NewActressRepository(db)
			actress := models.Actress{DMMID: 891002, FirstName: "Yui", ThumbURL: "protected.jpg", Verified: true, Origin: origin}
			require.NoError(t, repo.Create(ctx, &actress))
			alias := models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Yui"}
			require.NoError(t, NewActressAliasRepository(db).Create(ctx, &alias))
			translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Old translation"}
			require.NoError(t, db.Create(&translation).Error)
			incoming := models.Actress{ID: actress.ID, DMMID: 999999, FirstName: "Wrong", LastName: "Hatano", ThumbURL: "wrong.jpg"}
			require.NoError(t, repo.ImportUpsert(ctx, &incoming))
			require.Equal(t, "Hatano Yui", incoming.FullName())
			require.Equal(t, actress.DMMID, incoming.DMMID)
			require.Equal(t, actress.ThumbURL, incoming.ThumbURL)
			require.Equal(t, actress.CreatedAt, incoming.CreatedAt)
			require.Equal(t, origin, incoming.Origin)
			require.True(t, incoming.Verified)
			require.NoError(t, db.First(&alias, alias.ID).Error)
			require.Equal(t, "Hatano Yui", alias.CanonicalName)
			for _, name := range []string{"Stage Name", "Yui"} {
				found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{FirstName: name})
				require.NoError(t, err)
				require.Equal(t, ResolutionMatched, outcome)
				require.Equal(t, actress.ID, found.ID, name)
			}
			var count int64
			require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", actress.ID).Count(&count).Error)
			require.Zero(t, count)
			translation = models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Current translation"}
			require.NoError(t, db.Create(&translation).Error)
			require.NoError(t, repo.ImportUpsert(ctx, &incoming))
			require.NoError(t, db.First(&translation, translation.ID).Error, "identical import must retain current translations")
			require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestImportCanonicalEnrichmentRepairsPreviouslyStaleCollision(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := t.Context()
	actress := models.Actress{DMMID: 891009, FirstName: "Yui", LastName: "Hatano"}
	require.NoError(t, NewActressRepository(db).Create(ctx, &actress))
	before, err := NewMovieRepository(db).Upsert(ctx, creditMovie("import-stale", []models.MovieCredit{{
		CreditedName: "波多野結衣", Source: "fixture", Scraped: models.Actress{DMMID: actress.DMMID, JapaneseName: "波多野結衣"},
	}}))
	require.NoError(t, err)
	// Model a persisted import from before this fix: the canonical field was
	// filled, but the matching collision was never reconciled.
	require.NoError(t, db.Model(&actress).Update("japanese_name", "波多野結衣").Error)
	incoming := models.Actress{DMMID: actress.DMMID, JapaneseName: "波多野結衣"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
	after, err := NewMovieRepository(db).FindByContentID(ctx, before.ContentID)
	require.NoError(t, err)
	require.Equal(t, before.RenderGeneration+1, after.RenderGeneration, "gate-only changes must invalidate stale work")
	rows, err := NewCreditCollisionRepository(db).ListOpenByMovie(ctx, before.ContentID)
	require.NoError(t, err)
	require.Empty(t, rows)
	called := false
	err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, after.ContentID, after.RenderGeneration, func(*models.Movie) error { called = true; return nil })
	require.NoError(t, err)
	require.True(t, called)
}

func TestImportCanonicalEnrichmentPreservesRemainingConflicts(t *testing.T) {
	for _, tc := range []struct {
		id, reported, thumb string
		pinned              bool
		wantField           string
	}{
		{id: "remaining-name", reported: "Different Person", wantField: models.CreditFieldCreditedName},
		{id: "remaining-thumb", reported: "波多野結衣", thumb: "different.jpg", wantField: models.CreditFieldReportedThumb},
		{id: "remaining-pinned", reported: "波多野結衣", pinned: true, wantField: models.CreditFieldCreditedName},
	} {
		t.Run(tc.id, func(t *testing.T) {
			db := newCreditTestDB(t)
			ctx := t.Context()
			actress := models.Actress{DMMID: 891003, FirstName: "Yui", LastName: "Hatano", ThumbURL: "protected.jpg"}
			require.NoError(t, NewActressRepository(db).Create(ctx, &actress))
			before, err := NewMovieRepository(db).Upsert(ctx, creditMovie(tc.id, []models.MovieCredit{{
				CreditedName: tc.reported, ReportedThumbURL: tc.thumb, Source: "fixture",
				Scraped: models.Actress{DMMID: actress.DMMID, JapaneseName: tc.reported, ThumbURL: tc.thumb},
			}}))
			require.NoError(t, err)
			if tc.pinned {
				require.NoError(t, db.Model(&models.CreditCollision{}).Where("movie_content_id = ?", tc.id).Update("user_pinned", true).Error)
			}
			incoming := models.Actress{DMMID: actress.DMMID, JapaneseName: "波多野結衣", ThumbURL: "different.jpg"}
			require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
			rows, err := NewCreditCollisionRepository(db).ListOpenByMovie(ctx, tc.id)
			require.NoError(t, err)
			require.Len(t, rows, 1, tc.id)
			require.Equal(t, tc.wantField, rows[0].Field, tc.id)
			movie, err := NewMovieRepository(db).FindByContentID(ctx, tc.id)
			require.NoError(t, err)
			require.Equal(t, before.Credits[0].CreditedName, movie.Credits[0].CreditedName)
			require.Equal(t, "protected.jpg", movie.Credits[0].Actress.ThumbURL)
			called := false
			err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, tc.id, movie.RenderGeneration, func(*models.Movie) error { called = true; return nil })
			require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked, tc.id)
			require.False(t, called)
		})
	}
}

func TestImportCanonicalEnrichmentRespectsAliasOwnership(t *testing.T) {
	for _, sharedCanonical := range []bool{false, true} {
		name := "unrelated alias is preserved"
		if sharedCanonical {
			name = "shared canonical owner aborts import"
		}
		t.Run(name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			actress := models.Actress{DMMID: 891007, FirstName: "Yui"}
			other := models.Actress{DMMID: 891008, FirstName: "Other Person"}
			alias := models.ActressAlias{AliasName: "Yui", CanonicalName: other.FirstName}
			if sharedCanonical {
				other.FirstName = actress.FirstName
				alias = models.ActressAlias{AliasName: "Stage Name", CanonicalName: actress.FirstName}
			}
			require.NoError(t, repo.Create(t.Context(), &actress))
			require.NoError(t, repo.Create(t.Context(), &other))
			require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &alias))
			incoming := models.Actress{DMMID: actress.DMMID, LastName: "Hatano"}
			err := repo.ImportUpsert(t.Context(), &incoming)
			if sharedCanonical {
				require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
			} else {
				require.NoError(t, err)
			}
			var stored models.ActressAlias
			require.NoError(t, db.First(&stored, alias.ID).Error)
			require.Equal(t, alias, stored)
			require.NoError(t, db.First(&actress, actress.ID).Error)
			if sharedCanonical {
				require.Empty(t, actress.LastName)
			} else {
				require.Equal(t, "Hatano", actress.LastName)
			}
		})
	}
}

func TestImportCanonicalEnrichmentRefreshesOnlyIdentitySnapshots(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{DMMID: 891004, FirstName: "Yui"}
	require.NoError(t, NewActressRepository(db).Create(t.Context(), &actress))
	cases := []struct {
		id, source, origin string
		override           bool
		wantName           string
	}{
		{id: "snapshot-empty-source", origin: string(models.CreditOriginUser), wantName: "Hatano Yui"},
		{id: "snapshot-legacy", source: "legacy", origin: string(models.CreditOriginUser), wantName: "Hatano Yui"},
		{id: "snapshot-scraped", source: "fixture", origin: string(models.CreditOriginScrape), wantName: "Yui"},
		{id: "snapshot-overridden", origin: string(models.CreditOriginUser), override: true, wantName: "Yui"},
	}
	for _, tc := range cases {
		movie := models.Movie{ContentID: tc.id, ID: tc.id, RenderGeneration: 4}
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: tc.id, ActressID: actress.ID, CreditedName: "Yui", Origin: tc.origin, Source: tc.source, UserOverride: tc.override}
		require.NoError(t, db.Create(&credit).Error)
	}
	incoming := models.Actress{DMMID: actress.DMMID, LastName: "Hatano"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	var credits []models.MovieCredit
	require.NoError(t, db.Order("id").Find(&credits).Error)
	require.Len(t, credits, 4)
	for i, credit := range credits {
		require.Equal(t, cases[i].wantName, credit.CreditedName, credit.MovieContentID)
		var movie models.Movie
		require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
		require.EqualValues(t, 5, movie.RenderGeneration)
		require.True(t, movie.RenderDirty)
	}
}

func TestImportCanonicalEnrichmentPropagatesSiblingQuarantine(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := t.Context()
	actress := models.Actress{FirstName: "Canonical"}
	require.NoError(t, NewActressRepository(db).Create(ctx, &actress))
	before, err := NewMovieRepository(db).Upsert(ctx, creditMovie("import-sibling", []models.MovieCredit{{
		CreditedName: "波多野結衣", Source: "fixture", Scraped: models.Actress{DMMID: 891005, JapaneseName: "波多野結衣"},
	}}))
	require.NoError(t, err)
	require.Len(t, before.Credits, 1)
	require.False(t, before.Credits[0].Actress.Verified)
	require.False(t, before.Credits[0].Actress.AmbiguityQuarantined)
	incoming := models.Actress{ID: actress.ID, JapaneseName: "波多野結衣"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
	after, err := NewMovieRepository(db).FindByContentID(ctx, before.ContentID)
	require.NoError(t, err)
	require.Equal(t, before.Credits[0].ActressID, after.Credits[0].ActressID, "import must not merge ambiguous identities")
	require.False(t, after.Credits[0].Actress.Verified)
	require.True(t, after.Credits[0].Actress.AmbiguityQuarantined)
	require.False(t, after.Credits[0].RenderVisible())
	require.Equal(t, before.RenderGeneration+1, after.RenderGeneration)
	called := false
	err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, after.ContentID, after.RenderGeneration, func(*models.Movie) error { called = true; return nil })
	require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
	require.False(t, called)
	require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
	repeated, err := NewMovieRepository(db).FindByContentID(ctx, before.ContentID)
	require.NoError(t, err)
	require.False(t, repeated.Credits[0].Actress.Verified)
	require.True(t, repeated.Credits[0].Actress.AmbiguityQuarantined, "re-importing the verified owner must not promote or release its sibling")
	require.Equal(t, after.RenderGeneration, repeated.RenderGeneration)
}

func TestImportCanonicalEnrichmentPreservesCandidatePromotion(t *testing.T) {
	db := newCreditTestDB(t)
	ctx := t.Context()
	require.NoError(t, NewActressRepository(db).Create(ctx, &models.Actress{FirstName: "Shared Name"}))
	before, err := NewMovieRepository(db).Upsert(ctx, creditMovie("import-promoted", []models.MovieCredit{{
		CreditedName: "Shared Name", Source: "fixture", Scraped: models.Actress{DMMID: 891010, FirstName: "Shared Name"},
	}}))
	require.NoError(t, err)
	require.Len(t, before.Credits, 1)
	require.True(t, before.Credits[0].Actress.AmbiguityQuarantined)
	incoming := *before.Credits[0].Actress
	incoming.JapaneseName = "確認済み"
	require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
	after, err := NewMovieRepository(db).FindByContentID(ctx, before.ContentID)
	require.NoError(t, err)
	require.True(t, after.Credits[0].Actress.Verified)
	require.False(t, after.Credits[0].Actress.AmbiguityQuarantined)
	rows, err := NewCreditCollisionRepository(db).ListOpenByMovie(ctx, before.ContentID)
	require.NoError(t, err)
	require.Empty(t, rows, "canonical reconciliation must not undo explicit candidate promotion")
	var resolved models.CreditCollision
	require.NoError(t, db.Where("credit_id = ? AND field = ?", before.Credits[0].ID, models.CreditFieldIdentityLink).First(&resolved).Error)
	require.Equal(t, models.CollisionResolutionKeepIdentity, resolved.Resolution)
	called := false
	err = NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, after.ContentID, after.RenderGeneration, func(*models.Movie) error { called = true; return nil })
	require.NoError(t, err)
	require.True(t, called)
}

func TestImportCanonicalEnrichmentLifecycleFailureRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
	}{
		{"translation", "BEFORE DELETE ON actress_translations"},
		{"snapshot", "BEFORE UPDATE ON movie_credits"},
		{"alias retarget", "BEFORE UPDATE ON actress_aliases"},
		{"alias preservation", "BEFORE INSERT ON actress_aliases"},
		{"collision", "BEFORE UPDATE ON credit_collisions"},
		{"generation", "BEFORE UPDATE OF render_generation ON movies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			actress := models.Actress{DMMID: 891006, FirstName: "Yui"}
			require.NoError(t, NewActressRepository(db).Create(t.Context(), &actress))
			alias := models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Yui"}
			require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &alias))
			translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Original translation"}
			require.NoError(t, db.Create(&translation).Error)
			movie := models.Movie{ContentID: "import-rollback", ID: "import-rollback", RenderGeneration: 7}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Yui", Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Hatano Yui", CanonicalValue: "Yui", Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)
			require.NoError(t, db.Exec("CREATE TRIGGER fail_import_lifecycle "+tc.trigger+" BEGIN SELECT RAISE(ABORT, 'injected import lifecycle failure'); END").Error)
			incoming := models.Actress{DMMID: actress.DMMID, LastName: "Hatano"}
			require.ErrorContains(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming), "injected import lifecycle failure")
			var stored models.Actress
			require.NoError(t, db.First(&stored, actress.ID).Error)
			require.Equal(t, actress, stored)
			require.NoError(t, db.First(&alias, alias.ID).Error)
			require.Equal(t, "Yui", alias.CanonicalName)
			require.NoError(t, db.First(&translation, translation.ID).Error)
			require.NoError(t, db.First(&credit, credit.ID).Error)
			require.Equal(t, "Yui", credit.CreditedName)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, collision.Status)
			require.Equal(t, "Yui", collision.CanonicalValue)
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			require.EqualValues(t, 7, movie.RenderGeneration)
			require.False(t, movie.RenderDirty)
			var aliases int64
			require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
			require.EqualValues(t, 1, aliases)
		})
	}
}
