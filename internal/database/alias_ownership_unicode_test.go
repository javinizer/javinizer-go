package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdoptAliasRejectsDifferentOwnerAndRollsBack(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	var ownerA, ownerB models.Actress
	ownerA = models.Actress{JapaneseName: "Owner A", Verified: true, Origin: ActressOriginUser}
	ownerB = models.Actress{JapaneseName: "Owner B", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&ownerA).Error)
	require.NoError(t, db.Create(&ownerB).Error)
	require.NoError(t, db.Model(&credit).Updates(map[string]any{"actress_id": ownerB.ID, "display_force_canonical": false}).Error)
	require.NoError(t, db.Model(&collision).Updates(map[string]any{
		"reported_value": "Ｓｔａｇｅ　Ｎａｍｅ", "canonical_value": ownerB.JapaneseName,
	}).Error)
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &models.ActressAlias{
		AliasName: "Stage Name", CanonicalName: ownerA.JapaneseName,
	}))
	var before models.Movie
	require.NoError(t, db.First(&before, "content_id = ?", credit.MovieContentID).Error)

	_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionAdoptAlias, 0)
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)

	found, findErr := NewActressAliasRepository(db).FindByAliasName(t.Context(), "stage name")
	require.NoError(t, findErr)
	require.Equal(t, ownerA.JapaneseName, found.CanonicalName)
	var afterCollision models.CreditCollision
	require.NoError(t, db.First(&afterCollision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, afterCollision.Status)
	require.Empty(t, afterCollision.Resolution)
	var afterCredit models.MovieCredit
	require.NoError(t, db.First(&afterCredit, credit.ID).Error)
	require.False(t, afterCredit.DisplayForceCanonical)
	var after models.Movie
	require.NoError(t, db.First(&after, "content_id = ?", credit.MovieContentID).Error)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}

func TestUnicodeAliasNormalizationRepositoryResolverAndRestart(t *testing.T) {
	require.Equal(t, models.NormalizeActressNameKey("が"), models.NormalizeActressNameKey("か\u3099"))
	require.Equal(t, models.NormalizeActressNameKey("Stage Name"), models.NormalizeActressNameKey("Ｓｔａｇｅ　Ｎａｍｅ"))

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "unicode-alias.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
	db, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	owner := models.Actress{JapaneseName: "所有者", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&owner).Error)
	repo := NewActressAliasRepository(db)
	first := &models.ActressAlias{AliasName: "が", CanonicalName: owner.JapaneseName}
	require.NoError(t, repo.Create(ctx, first))
	equivalent := &models.ActressAlias{AliasName: "か\u3099", CanonicalName: owner.JapaneseName}
	require.NoError(t, repo.Create(ctx, equivalent))
	require.Equal(t, first.ID, equivalent.ID)
	found, err := repo.FindByAliasName(ctx, "か\u3099")
	require.NoError(t, err)
	require.Equal(t, first.ID, found.ID)
	resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "か\u3099"})
	require.NoError(t, err)
	require.Equal(t, ResolutionMatched, outcome)
	require.Equal(t, owner.ID, resolved.ID)

	// Simulate a database whose version-19 keys were produced by the old
	// lowercase/whitespace-only algorithm. Startup must re-key all rows.
	require.NoError(t, db.Exec("UPDATE actress_aliases SET alias_name_key = ?, updated_at = ? WHERE id = ?", "か\u3099", "2020-01-02 03:04:05", first.ID).Error)
	var aliasUpdatedBefore string
	require.NoError(t, db.Raw("SELECT updated_at FROM actress_aliases WHERE id = ?", first.ID).Scan(&aliasUpdatedBefore).Error)
	require.NoError(t, db.Close())
	db, err = New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	found, err = NewActressAliasRepository(db).FindByAliasName(ctx, "が")
	require.NoError(t, err)
	require.Equal(t, first.ID, found.ID)
	var aliasUpdatedAfter string
	require.NoError(t, db.Raw("SELECT updated_at FROM actress_aliases WHERE id = ?", first.ID).Scan(&aliasUpdatedAfter).Error)
	require.Equal(t, aliasUpdatedBefore, aliasUpdatedAfter)
}

func TestUnicodeMigrationPreservesDifferentOwnersAsAmbiguous(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "unicode-conflict.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
	db, err := New(cfg)
	require.NoError(t, err)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.UpTo(ctx, 18)
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		"INSERT INTO actress_aliases (alias_name, canonical_name) VALUES (?, ?), (?, ?)",
		"が", "Owner A", "か\u3099", "Owner B",
	).Error)
	require.NoError(t, db.Close())

	db, err = New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	_, err = NewActressAliasRepository(db).FindByAliasName(ctx, "が")
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	var count int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Where("alias_name_key = ?", models.NormalizeActressNameKey("が")).Count(&count).Error)
	require.EqualValues(t, 2, count)

	owner := models.Actress{DMMID: 99101, JapaneseName: "Owner A", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&owner).Error)
	movie := creditMovie("unicode-ambiguous-gate", []models.MovieCredit{{
		CreditedName: "か\u3099", Scraped: models.Actress{DMMID: owner.DMMID},
	}})
	_, err = NewMovieRepository(db).Upsert(ctx, movie)
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	var movies int64
	require.NoError(t, db.Model(&models.Movie{}).Where("content_id = ?", movie.ContentID).Count(&movies).Error)
	require.Zero(t, movies)
}

func TestAliasOwnerClaimDoesNotMutateEquivalentDuplicate(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressAliasRepository(db)
	first := &models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Owner A"}
	require.NoError(t, repo.Create(t.Context(), first))
	claim := &models.ActressAlias{AliasName: "Ｓｔａｇｅ　Ｎａｍｅ", CanonicalName: "Ｏｗｎｅｒ　Ａ"}
	require.NoError(t, repo.Upsert(t.Context(), claim))
	require.Equal(t, first.ID, claim.ID)
	var stored models.ActressAlias
	require.NoError(t, db.First(&stored, first.ID).Error)
	require.Equal(t, "Stage Name", stored.AliasName)
	require.Equal(t, "Owner A", stored.CanonicalName)

	err := repo.Upsert(t.Context(), &models.ActressAlias{AliasName: "stage name", CanonicalName: "Owner B"})
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	require.NoError(t, db.First(&stored, first.ID).Error)
	require.Equal(t, "Owner A", stored.CanonicalName)
}

func TestAliasOwnerConflictErrorContainsActionableOwners(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressAliasRepository(db)
	require.NoError(t, repo.Create(t.Context(), &models.ActressAlias{AliasName: "Alias", CanonicalName: "Owner A"}))
	err := repo.Upsert(context.Background(), &models.ActressAlias{AliasName: "Ａｌｉａｓ", CanonicalName: "Owner B"})
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	require.Contains(t, err.Error(), "Owner A")
	require.Contains(t, err.Error(), "Owner B")
}

func TestAliasClaimDatabaseLockFailsClosed(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("CREATE TRIGGER lock_alias_claim BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'database is locked'); END").Error)
	err := NewActressAliasRepository(db).Create(t.Context(), &models.ActressAlias{AliasName: "Locked Alias", CanonicalName: "Owner"})
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
}

func TestConcurrentNormalizedAliasClaimsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-alias.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
	first, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, first.RunMigrationsOnStartup(t.Context()))
	second, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, claim := range []struct {
		db           *DB
		alias, owner string
	}{{first, "Stage Name", "Owner A"}, {second, "Ｓｔａｇｅ　Ｎａｍｅ", "Owner B"}} {
		claim := claim
		go func() {
			<-start
			errs <- NewActressAliasRepository(claim.db).Upsert(t.Context(), &models.ActressAlias{AliasName: claim.alias, CanonicalName: claim.owner})
		}()
	}
	close(start)
	errA, errB := <-errs, <-errs
	if errA == nil {
		errA, errB = errB, errA
	}
	require.ErrorIs(t, errA, ErrActressAliasOwnershipConflict)
	require.NoError(t, errB)

	var rows []models.ActressAlias
	require.NoError(t, first.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Contains(t, []string{"Owner A", "Owner B"}, rows[0].CanonicalName)
	found, err := NewActressAliasRepository(first).FindByAliasName(t.Context(), "stage name")
	require.NoError(t, err)
	require.Equal(t, rows[0].CanonicalName, found.CanonicalName)
}

func TestCanonicalRetargetRejectsHomonymousIdentityAndRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	first := models.Actress{DMMID: 88001, JapaneseName: "Shared Name", Verified: true, Origin: ActressOriginUser}
	second := models.Actress{DMMID: 88002, JapaneseName: "Shared Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	alias := models.ActressAlias{AliasName: "Second Only Alias", CanonicalName: "Shared Name"}
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &alias))

	err := repo.RenameNameFields(t.Context(), first.ID, "", "", "First Renamed")
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)

	var storedFirst models.Actress
	require.NoError(t, db.First(&storedFirst, first.ID).Error)
	require.Equal(t, "Shared Name", storedFirst.JapaneseName)
	var storedAlias models.ActressAlias
	require.NoError(t, db.First(&storedAlias, alias.ID).Error)
	require.Equal(t, "Shared Name", storedAlias.CanonicalName)
}

func TestMergeCanonicalRetargetRejectsThirdHomonymAndRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 88101, JapaneseName: "Merge Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{DMMID: 88102, JapaneseName: "Shared Source", Verified: true, Origin: ActressOriginUser}
	homonym := models.Actress{DMMID: 88103, JapaneseName: "Ｓｈａｒｅｄ　Ｓｏｕｒｃｅ", Verified: true, Origin: ActressOriginUser}
	for _, actress := range []*models.Actress{&target, &source, &homonym} {
		require.NoError(t, db.Create(actress).Error)
	}
	alias := models.ActressAlias{AliasName: "Source Only Alias", CanonicalName: source.JapaneseName}
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &alias))
	movie := models.Movie{ContentID: "homonym-merge-rollback", RenderGeneration: 17}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{
		MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: source.JapaneseName,
	}).Error)

	_, err := repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"japanese_name": "target"})
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)

	for _, expected := range []models.Actress{target, source, homonym} {
		var stored models.Actress
		require.NoError(t, db.First(&stored, expected.ID).Error)
		require.Equal(t, expected.JapaneseName, stored.JapaneseName)
	}
	var storedAlias models.ActressAlias
	require.NoError(t, db.First(&storedAlias, alias.ID).Error)
	require.Equal(t, source.JapaneseName, storedAlias.CanonicalName)
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
	require.Equal(t, int64(17), storedMovie.RenderGeneration)
	require.False(t, storedMovie.RenderDirty)
}

func TestMergeCanonicalRetargetUsesExplicitSourceAndLeavesOtherIdentityUntouched(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 88201, JapaneseName: "Merge Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{DMMID: 88202, JapaneseName: "Merge Source", Verified: true, Origin: ActressOriginUser}
	other := models.Actress{DMMID: 88203, JapaneseName: "Other Source", Verified: true, Origin: ActressOriginUser}
	for _, actress := range []*models.Actress{&target, &source, &other} {
		require.NoError(t, db.Create(actress).Error)
	}
	sourceAlias := models.ActressAlias{AliasName: "Source Alias", CanonicalName: source.JapaneseName}
	otherAlias := models.ActressAlias{AliasName: "Other Alias", CanonicalName: other.JapaneseName}
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &sourceAlias))
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &otherAlias))

	_, err := repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"japanese_name": "target"})
	require.NoError(t, err)

	require.ErrorIs(t, db.First(&models.Actress{}, source.ID).Error, gorm.ErrRecordNotFound)
	var storedSourceAlias, storedOtherAlias models.ActressAlias
	require.NoError(t, db.First(&storedSourceAlias, sourceAlias.ID).Error)
	require.Equal(t, target.JapaneseName, storedSourceAlias.CanonicalName)
	require.NoError(t, db.First(&storedOtherAlias, otherAlias.ID).Error)
	require.Equal(t, other.JapaneseName, storedOtherAlias.CanonicalName)
	var storedOther models.Actress
	require.NoError(t, db.First(&storedOther, other.ID).Error)
	require.Equal(t, other.JapaneseName, storedOther.JapaneseName)
}

func TestRetargetProvenCanonicalAliasesRejectsUnprovenOwner(t *testing.T) {
	db := newCreditTestDB(t)
	err := retargetProvenCanonicalAliasesTx(db.DB, 0, 0, "Owner A", "Owner B", map[string]struct{}{})
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
}

func TestCanonicalRetargetIdentityProofFailureContracts(t *testing.T) {
	t.Run("owner verification query", func(t *testing.T) {
		db := newCreditTestDB(t)
		owner := models.Actress{JapaneseName: "Old Owner", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&owner).Error)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Old Alias", CanonicalName: owner.JapaneseName}).Error)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		err := retargetProvenCanonicalAliasesTx(db.DB, owner.ID, owner.ID, owner.JapaneseName, "New Owner", map[string]struct{}{models.NormalizeActressNameKey(owner.JapaneseName): {}})
		require.ErrorContains(t, err, "verify unique actress alias owner")
	})

	t.Run("snapshot outside merge pair", func(t *testing.T) {
		db := newCreditTestDB(t)
		target := models.Actress{JapaneseName: "New Owner", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)
		err := transitionActressCanonicalNamesForMergeTx(db.DB, target.ID, target.ID+1, &models.Actress{ID: target.ID + 2, JapaneseName: "Old Owner"})
		require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	})
}

func TestCandidateNameKeyNFKCBackfillAndConflictFailClosed(t *testing.T) {
	t.Run("single candidate rekeys and resolves after restart", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "candidate-rekey.db")
		cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
		db, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		candidate := models.Actress{JapaneseName: "が", Verified: false, Origin: ActressOriginScrape, NameKey: "か\u3099"}
		require.NoError(t, db.Create(&candidate).Error)
		require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", "2020-01-02 03:04:05", candidate.ID).Error)
		var updatedBefore string
		require.NoError(t, db.Raw("SELECT updated_at FROM actresses WHERE id = ?", candidate.ID).Scan(&updatedBefore).Error)
		require.NoError(t, db.Close())

		db, err = New(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		var stored models.Actress
		require.NoError(t, db.First(&stored, candidate.ID).Error)
		require.Equal(t, models.NormalizeActressNameKey("が"), stored.NameKey)
		var updatedAfter string
		require.NoError(t, db.Raw("SELECT updated_at FROM actresses WHERE id = ?", candidate.ID).Scan(&updatedAfter).Error)
		require.Equal(t, updatedBefore, updatedAfter)
		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "か\u3099"})
		require.NoError(t, err)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.Equal(t, candidate.ID, resolved.ID)
	})

	t.Run("equivalent legacy candidates are preserved and ambiguous", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "candidate-conflict.db")
		cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
		db, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		for _, candidate := range []models.Actress{
			{JapaneseName: "が", Origin: ActressOriginScrape, NameKey: "が"},
			{JapaneseName: "か\u3099", Origin: ActressOriginScrape, NameKey: "か\u3099"},
		} {
			require.NoError(t, db.Create(&candidate).Error)
		}
		require.NoError(t, db.Close())

		db, err = New(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		var candidates []models.Actress
		require.NoError(t, db.Where("verified = ?", false).Order("id").Find(&candidates).Error)
		require.Len(t, candidates, 2)
		for i := range candidates {
			require.Empty(t, candidates[i].NameKey)
			require.True(t, candidates[i].AmbiguityQuarantined)
		}
		_, _, err = ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "が"})
		require.ErrorIs(t, err, ErrActressCandidateAmbiguous)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Where("verified = ?", false).Count(&count).Error)
		require.EqualValues(t, 2, count)
	})
}

func TestImportUpsertReloadsRetargetOwnerInsideTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import-owner-proof.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	candidate := models.Actress{JapaneseName: "Stale Name", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Stale Name")}
	require.NoError(t, db.Create(&candidate).Error)

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:concurrent_import_owner", func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "actresses" {
			return
		}
		once.Do(func() {
			_, execErr := sqlDB.ExecContext(t.Context(), "UPDATE actresses SET japanese_name = ? WHERE id = ?", "Concurrent Name", candidate.ID)
			require.NoError(t, execErr)
			_, execErr = sqlDB.ExecContext(t.Context(), "INSERT INTO actress_aliases (alias_name, alias_name_key, canonical_name, canonical_name_key) VALUES (?, ?, ?, ?)",
				"Concurrent Alias", models.NormalizeActressNameKey("Concurrent Alias"), "Concurrent Name", models.NormalizeActressNameKey("Concurrent Name"))
			require.NoError(t, execErr)
		})
	}))

	incoming := models.Actress{ID: candidate.ID, JapaneseName: "Imported Name"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	alias, err := NewActressAliasRepository(db).FindByAliasName(t.Context(), "Concurrent Alias")
	require.NoError(t, err)
	require.Equal(t, "Imported Name", alias.CanonicalName)
}

func TestCandidateNameKeyBackfillFailureContracts(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Close())
		require.Error(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
	})

	for _, tc := range []struct {
		name, trigger string
		candidates    []models.Actress
	}{
		{"clear", "CREATE TRIGGER fail_candidate_key_clear BEFORE UPDATE OF name_key ON actresses WHEN NEW.name_key IS NULL BEGIN SELECT RAISE(ABORT, 'clear'); END", []models.Actress{{JapaneseName: "Clear", NameKey: "clear", Origin: ActressOriginScrape}}},
		{"rekey", "CREATE TRIGGER fail_candidate_key_rekey BEFORE UPDATE OF name_key ON actresses WHEN NEW.name_key IS NOT NULL BEGIN SELECT RAISE(ABORT, 'rekey'); END", []models.Actress{{JapaneseName: "Ｒｅｋｅｙ", NameKey: "legacy-rekey", Origin: ActressOriginScrape}}},
		{"quarantine", "CREATE TRIGGER fail_candidate_key_quarantine BEFORE UPDATE OF ambiguity_quarantined ON actresses BEGIN SELECT RAISE(ABORT, 'quarantine'); END", []models.Actress{{JapaneseName: "が", NameKey: "が", Origin: ActressOriginScrape}, {JapaneseName: "か\u3099", NameKey: "か\u3099", Origin: ActressOriginScrape}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			for i := range tc.candidates {
				require.NoError(t, db.Create(&tc.candidates[i]).Error)
			}
			require.NoError(t, db.Exec(tc.trigger).Error)
			require.Error(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
		})
	}

	t.Run("startup reports candidate backfill", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actresses", 1)
		err := db.RunMigrationsOnStartup(t.Context())
		require.ErrorContains(t, err, "backfill normalized actress candidate keys")
	})
}

func TestKeylessCandidateFallbackContracts(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		db := newCreditTestDB(t)
		candidate := models.Actress{JapaneseName: "が", Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&candidate).Error)
		found, err := findCandidateByNameEvidenceTx(db.DB, &models.Actress{JapaneseName: "か\u3099"})
		require.NoError(t, err)
		require.Equal(t, candidate.ID, found.ID)
	})

	t.Run("fallback query error", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "query", "actresses", 2)
		_, err := findCandidateByNameEvidenceTx(db.DB, &models.Actress{JapaneseName: "missing"})
		require.Error(t, err)
	})
}

func TestImportUpsertFailsIfMatchedIdentityDisappearsBeforeTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import-disappears.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	candidate := models.Actress{JapaneseName: "Candidate", Origin: ActressOriginScrape, NameKey: "candidate"}
	require.NoError(t, db.Create(&candidate).Error)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:delete_import_owner", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actresses" {
			once.Do(func() {
				_, execErr := sqlDB.ExecContext(t.Context(), "DELETE FROM actresses WHERE id = ?", candidate.ID)
				require.NoError(t, execErr)
			})
		}
	}))
	err = NewActressRepository(db).ImportUpsert(t.Context(), &models.Actress{ID: candidate.ID, JapaneseName: "Imported"})
	require.ErrorContains(t, err, "load imported actress")
}
