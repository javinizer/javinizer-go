package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCandidateResolutionUnionsKeyedAndKeylessRepresentations(t *testing.T) {
	t.Run("mixed candidates fail closed without mutation", func(t *testing.T) {
		db := newCreditTestDB(t)
		key := models.NormalizeActressNameKey("Stage Name")
		keyed := models.Actress{JapaneseName: "Stage Name", Origin: ActressOriginScrape, NameKey: key}
		keyless := models.Actress{DMMID: 91001, JapaneseName: "Ｓｔａｇｅ　Ｎａｍｅ", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&keyed).Error)
		require.NoError(t, db.Create(&keyless).Error)

		_, _, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "stage name"})
		require.ErrorIs(t, err, ErrActressCandidateAmbiguous)

		var actresses, credits int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
		require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
		require.EqualValues(t, 2, actresses)
		require.Zero(t, credits)
		for _, id := range []uint{keyed.ID, keyless.ID} {
			var preserved models.Actress
			require.NoError(t, db.First(&preserved, id).Error)
		}
	})

	t.Run("single keyed and keyless candidates still resolve", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			candidate models.Actress
			incoming  models.Actress
		}{
			{"keyed", models.Actress{FirstName: "Name", LastName: "Keyed", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Keyed Name")}, models.Actress{FirstName: "Name", LastName: "Keyed"}},
			{"keyless positive DMM", models.Actress{DMMID: 91002, JapaneseName: "か\u3099", Origin: ActressOriginScrape}, models.Actress{JapaneseName: "が"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				db := newCreditTestDB(t)
				require.NoError(t, db.Create(&tc.candidate).Error)
				resolved, outcome, err := ResolveActressIdentityTx(db.DB, &tc.incoming)
				require.NoError(t, err)
				require.Equal(t, ResolutionCandidateLinked, outcome)
				require.Equal(t, tc.candidate.ID, resolved.ID)
			})
		}
	})

	t.Run("positive DMM name evidence excludes a different positive DMM", func(t *testing.T) {
		db := newCreditTestDB(t)
		conflicting := models.Actress{DMMID: 91005, JapaneseName: "Shared Evidence", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&conflicting).Error)
		_, err := findCandidateByNameEvidenceTx(db.DB, &models.Actress{DMMID: 91006, JapaneseName: "shared evidence"})
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("exact positive DMM keeps precedence", func(t *testing.T) {
		db := newCreditTestDB(t)
		dmmless := models.Actress{JapaneseName: "Same Name", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Same Name")}
		exact := models.Actress{DMMID: 91003, JapaneseName: "Ｓａｍｅ　Ｎａｍｅ", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&dmmless).Error)
		require.NoError(t, db.Create(&exact).Error)

		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: exact.DMMID, JapaneseName: "Different Evidence"})
		require.NoError(t, err)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.Equal(t, exact.ID, resolved.ID)
	})
}

func TestCandidateBackfillDistinctPositiveDMMsRemainExactAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate-distinct-dmm.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}

	db, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	first := models.Actress{DMMID: 92001, JapaneseName: "Shared Stage Name", Origin: ActressOriginScrape}
	second := models.Actress{DMMID: 92002, JapaneseName: "Ｓｈａｒｅｄ　Ｓｔａｇｅ　Ｎａｍｅ", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	// Simulate rows marked by the legacy name-only backfill before restart.
	require.NoError(t, db.Model(&models.Actress{}).Where("id IN ?", []uint{first.ID, second.ID}).UpdateColumn(colAmbiguityQuarantined, true).Error)
	require.NoError(t, db.Close())

	for restart := 0; restart < 2; restart++ {
		db, err = New(cfg)
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		for _, expected := range []models.Actress{first, second} {
			var stored models.Actress
			require.NoError(t, db.First(&stored, expected.ID).Error)
			require.False(t, stored.AmbiguityQuarantined)
			resolved, outcome, resolveErr := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: expected.DMMID, JapaneseName: "unrelated"})
			require.NoError(t, resolveErr)
			require.Equal(t, ResolutionCandidateLinked, outcome)
			require.Equal(t, expected.ID, resolved.ID)
		}
		_, _, nameErr := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "shared stage name"})
		require.ErrorIs(t, nameErr, ErrActressCandidateAmbiguous)
		require.NoError(t, db.Close())
	}
}

func TestCandidateBackfillUnionsOldKeyedAndIntentionalKeylessAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate-union.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}

	old, err := New(cfg)
	require.NoError(t, err)
	sqlDB, err := old.DB.DB()
	require.NoError(t, err)
	_, err = newMigrationProvider(t, sqlDB).UpTo(ctx, 18)
	require.NoError(t, err)
	keyed := models.Actress{JapaneseName: "Stage Name", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Stage Name")}
	keyless := models.Actress{DMMID: 91004, JapaneseName: "Ｓｔａｇｅ　Ｎａｍｅ", Origin: ActressOriginScrape}
	require.NoError(t, old.Create(&keyed).Error)
	require.NoError(t, old.Create(&keyless).Error)
	require.NoError(t, old.Close())

	restarted, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restarted.Close() })
	require.NoError(t, restarted.RunMigrationsOnStartup(ctx))

	_, _, err = ResolveActressIdentityTx(restarted.DB, &models.Actress{JapaneseName: "stage name"})
	require.ErrorIs(t, err, ErrActressCandidateAmbiguous)
	for _, id := range []uint{keyed.ID, keyless.ID} {
		var candidate models.Actress
		require.NoError(t, restarted.First(&candidate, id).Error)
		require.Empty(t, candidate.NameKey)
		require.True(t, candidate.AmbiguityQuarantined)
	}
	var actresses, credits int64
	require.NoError(t, restarted.Model(&models.Actress{}).Count(&actresses).Error)
	require.NoError(t, restarted.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.EqualValues(t, 2, actresses)
	require.Zero(t, credits)
}

func TestDeleteStaleCandidatesGuardFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	stale := models.Actress{JapaneseName: "Delete Failure", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&stale).Error)
	require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", time.Now().UTC().Add(-48*time.Hour), stale.ID).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_candidate_gc BEFORE DELETE ON actresses BEGIN SELECT RAISE(ABORT, 'guard failure'); END").Error)

	pruned, err := NewActressRepository(db).DeleteStaleCandidates(t.Context(), time.Now().UTC().Add(-24*time.Hour))
	require.ErrorContains(t, err, "delete stale candidate")
	require.Zero(t, pruned)
	var preserved models.Actress
	require.NoError(t, db.First(&preserved, stale.ID).Error)
}

func TestDeleteStaleCandidatesUsesNormalizedAliasOwnership(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate-gc.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}

	old, err := New(cfg)
	require.NoError(t, err)
	sqlDB, err := old.DB.DB()
	require.NoError(t, err)
	_, err = newMigrationProvider(t, sqlDB).UpTo(ctx, 18)
	require.NoError(t, err)

	stale := time.Now().UTC().Add(-90 * 24 * time.Hour)
	owned := []models.Actress{
		{JapaneseName: "ＧＣ　Ｏｗｎｅｒ", Origin: ActressOriginScrape},
		{FirstName: "Given", LastName: "Ｆａｍｉｌｙ", Origin: ActressOriginScrape},
		{FirstName: "Ｓｉｎｇｌｅ", Origin: ActressOriginScrape},
	}
	for i := range owned {
		require.NoError(t, old.Create(&owned[i]).Error)
		require.NoError(t, old.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", stale, owned[i].ID).Error)
	}
	aliases := []models.ActressAlias{
		{AliasName: "JP Alias", CanonicalName: "gc owner"},
		{AliasName: "LF Alias", CanonicalName: "family given"},
		{AliasName: "One Alias", CanonicalName: "single"},
		// Two raw-distinct aliases with one normalized key intentionally model
		// an old conflicting ownership claim. GC must retain both owners.
		{AliasName: "Conflict Alias", CanonicalName: "gc owner"},
		{AliasName: "Ｃｏｎｆｌｉｃｔ　Ａｌｉａｓ", CanonicalName: "family given"},
	}
	for i := range aliases {
		require.NoError(t, old.Exec("INSERT INTO actress_aliases (alias_name, canonical_name, created_at, updated_at) VALUES (?, ?, ?, ?)", aliases[i].AliasName, aliases[i].CanonicalName, stale, stale).Error)
	}
	unrelated := models.Actress{JapaneseName: "Unrelated", Origin: ActressOriginScrape}
	inlineOnly := models.Actress{JapaneseName: "Inline Canonical", Aliases: "Inline Owner", Origin: ActressOriginScrape}
	for _, candidate := range []*models.Actress{&unrelated, &inlineOnly} {
		require.NoError(t, old.Create(candidate).Error)
		require.NoError(t, old.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", stale, candidate.ID).Error)
	}
	inlineAlias := models.ActressAlias{AliasName: "External Inline", CanonicalName: "inline owner"}
	require.NoError(t, old.Exec("INSERT INTO actress_aliases (alias_name, canonical_name, created_at, updated_at) VALUES (?, ?, ?, ?)", inlineAlias.AliasName, inlineAlias.CanonicalName, stale, stale).Error)
	require.NoError(t, old.Close())

	restarted, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restarted.Close() })
	require.NoError(t, restarted.RunMigrationsOnStartup(ctx))

	pruned, err := NewActressRepository(restarted).DeleteStaleCandidates(ctx, time.Now().UTC().Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 2, pruned)
	for _, candidate := range owned {
		var preserved models.Actress
		require.NoError(t, restarted.First(&preserved, candidate.ID).Error)
	}
	for _, id := range []uint{unrelated.ID, inlineOnly.ID} {
		var deleted models.Actress
		require.ErrorIs(t, restarted.First(&deleted, id).Error, gorm.ErrRecordNotFound)
	}
	var aliasCount int64
	require.NoError(t, restarted.Model(&models.ActressAlias{}).Count(&aliasCount).Error)
	require.EqualValues(t, len(aliases)+1, aliasCount)

	rows, err := restarted.Raw("PRAGMA foreign_key_check").Rows()
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close())

	require.NoError(t, restarted.Close())
	reopened, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	require.NoError(t, reopened.RunMigrationsOnStartup(ctx))
	for _, candidate := range owned {
		var preserved models.Actress
		require.NoError(t, reopened.First(&preserved, candidate.ID).Error)
	}
	resolved, _, err := ResolveActressIdentityTx(reopened.DB, &models.Actress{JapaneseName: "gc owner"})
	require.NoError(t, err)
	require.Equal(t, owned[0].ID, resolved.ID)
}

func TestCandidateBackfillPreservesStaleKeyAmbiguityAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate-stale-key.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
	db, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(ctx))

	legacyKey := models.NormalizeActressNameKey("Legacy Evidence")
	keyed := models.Actress{JapaneseName: "Renamed Candidate", NameKey: legacyKey, Origin: ActressOriginScrape}
	keyless := models.Actress{DMMID: 91007, JapaneseName: "Ｌｅｇａｃｙ　Ｅｖｉｄｅｎｃｅ", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&keyed).Error)
	require.NoError(t, db.Create(&keyless).Error)
	_, _, err = ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "legacy evidence"})
	require.ErrorIs(t, err, ErrActressCandidateAmbiguous)
	require.NoError(t, db.Close())

	restarted, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restarted.Close() })
	require.NoError(t, restarted.RunMigrationsOnStartup(ctx))
	_, _, err = ResolveActressIdentityTx(restarted.DB, &models.Actress{JapaneseName: "legacy evidence"})
	require.ErrorIs(t, err, ErrActressCandidateAmbiguous)
	for _, id := range []uint{keyed.ID, keyless.ID} {
		var preserved models.Actress
		require.NoError(t, restarted.First(&preserved, id).Error)
		require.Empty(t, preserved.NameKey)
		require.True(t, preserved.AmbiguityQuarantined)
	}
}

func TestDeleteStaleCandidatesCleansTranslationsAndKeepsReassignmentTargets(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys disabled", true: "foreign keys enabled"}[foreignKeys], func(t *testing.T) {
			ctx := context.Background()
			dsn := filepath.Join(t.TempDir(), "candidate-dependencies.db")
			if foreignKeys {
				dsn += "?_foreign_keys=1"
			} else {
				dsn += "?_foreign_keys=0"
			}
			db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(ctx))

			stale := time.Now().UTC().Add(-90 * 24 * time.Hour)
			translated := models.Actress{JapaneseName: "Translated Candidate", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&translated).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: translated.ID, Language: "en", DisplayName: "Translated"}).Error)

			source := models.Actress{JapaneseName: "Reassignment Source", Verified: true, Origin: ActressOriginUser}
			target := models.Actress{JapaneseName: "Reassignment Target", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, db.Create(&target).Error)
			movie := models.Movie{ContentID: "gc-target-" + map[bool]string{false: "off", true: "on"}[foreignKeys], ID: "GC-TARGET"}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: source.ID, TargetActressID: target.ID}).Error)
			for _, id := range []uint{translated.ID, target.ID} {
				require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", stale, id).Error)
			}

			pruned, err := NewActressRepository(db).DeleteStaleCandidates(ctx, time.Now().UTC().Add(-30*24*time.Hour))
			require.NoError(t, err)
			require.EqualValues(t, 1, pruned)
			var count int64
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", translated.ID).Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", translated.ID).Count(&count).Error)
			require.Zero(t, count)
			var preserved models.Actress
			require.NoError(t, db.First(&preserved, target.ID).Error)
			require.NoError(t, db.Model(&models.MovieCreditReassignment{}).Where("target_actress_id = ?", target.ID).Count(&count).Error)
			require.EqualValues(t, 1, count)

			rows, err := db.Raw("PRAGMA foreign_key_check").Rows()
			require.NoError(t, err)
			require.False(t, rows.Next())
			require.NoError(t, rows.Close())
		})
	}
}

func TestDeleteStaleCandidatesTranslationFailureRollsBackIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate-translation-failure.db") + "?_foreign_keys=0"
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	candidate := models.Actress{JapaneseName: "Translation Rollback", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: candidate.ID, Language: "en", DisplayName: "Rollback"}).Error)
	require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", time.Now().UTC().Add(-48*time.Hour), candidate.ID).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_candidate_translation_gc BEFORE DELETE ON actress_translations BEGIN SELECT RAISE(ABORT, 'translation guard failure'); END").Error)

	pruned, err := NewActressRepository(db).DeleteStaleCandidates(t.Context(), time.Now().UTC().Add(-24*time.Hour))
	require.ErrorContains(t, err, "translations for stale candidate")
	require.Zero(t, pruned)
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", candidate.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", candidate.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
