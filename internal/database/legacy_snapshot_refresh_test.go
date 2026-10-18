package database

import (
	"encoding/xml"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Seed the old schema, not a hand-built approximation of migration 16's output.
func legacySnapshotDB(t *testing.T, first, last, japanese string, alreadyUpgraded bool) (*DB, *Config) {
	t.Helper()
	cfg := &Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "legacy.db"), LogLevel: "silent"}
	db, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, conn)
	_, err = provider.UpTo(t.Context(), 15)
	require.NoError(t, err)
	require.NoError(t, db.Exec("INSERT INTO movies(content_id,id,title) VALUES('legacy-snapshot','LEGACY-SNAPSHOT','Local fixture')").Error)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,first_name,last_name,japanese_name) VALUES(1,90101,?,?,?)", first, last, japanese).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses(movie_content_id,actress_id) VALUES('legacy-snapshot',1)").Error)
	if alreadyUpgraded {
		// Simulate a database upgraded by a previous binary, before this fix.
		_, err = provider.UpTo(t.Context(), 21)
		require.NoError(t, err)
	}
	db = reopenLegacySnapshotDB(t, db, cfg)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	return reopenLegacySnapshotDB(t, db, cfg), cfg
}

func reopenLegacySnapshotDB(t *testing.T, db *DB, cfg *Config) *DB {
	t.Helper()
	require.NoError(t, db.Close())
	reopened, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func requireLegacySnapshotNFO(t *testing.T, movie *models.Movie, useCredit bool, name string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	generator := nfo.NewGenerator(fs, &nfo.Config{UseCreditedName: useCredit})
	require.NoError(t, generator.GenerateAtPath(t.Context(), movie, "/snapshot.nfo", "", nil))
	data, err := afero.ReadFile(fs, "/snapshot.nfo")
	require.NoError(t, err)
	var output struct {
		Actors []struct {
			Name string `xml:"name"`
		} `xml:"actor"`
	}
	require.NoError(t, xml.Unmarshal(data, &output))
	require.Len(t, output.Actors, 1)
	require.Equal(t, name, output.Actors[0].Name)
}

func TestLegacySnapshotMigrationRenameReopenAndNFO(t *testing.T) {
	for _, alreadyUpgraded := range []bool{false, true} {
		upgrade := "fresh-upgrade"
		if alreadyUpgraded {
			upgrade = "previously-upgraded"
		}
		for _, names := range []struct{ label, first, last, japanese string }{
			{"first-only", "Solo", "", "旧名"},
			{"last-only", "", "Solo", "旧名"},
			{"full-name", "Person", "Old", "旧名"},
			{"japanese-fallback", "", "", "旧名"},
		} {
			t.Run(upgrade+"/"+names.label, func(t *testing.T) {
				db, cfg := legacySnapshotDB(t, names.first, names.last, names.japanese, alreadyUpgraded)
				credits, err := NewMovieCreditRepository(db).ListByMovie(t.Context(), "legacy-snapshot")
				require.NoError(t, err)
				require.Len(t, credits, 1)
				initial := credits[0]
				require.Equal(t, names.last+" "+names.first, initial.CreditedName)
				require.True(t, initial.LegacyInferred)
				require.Equal(t, "legacy", initial.Source)
				require.Equal(t, string(models.CreditOriginUser), initial.Origin)
				// No startup rewrite is needed: display already ignores the padding.
				movie, err := NewMovieRepository(db).FindByID(t.Context(), "LEGACY-SNAPSHOT")
				require.NoError(t, err)
				requireLegacySnapshotNFO(t, movie, true, initial.Actress.FullName())
				for rename, last := range []string{"New", "Next"} {
					actor, err := NewActressRepository(db).FindByID(t.Context(), 1)
					require.NoError(t, err)
					actor.FirstName, actor.LastName, actor.JapaneseName = "Person", last, "新名"
					require.NoError(t, NewActressRepository(db).Update(t.Context(), actor))
					db = reopenLegacySnapshotDB(t, db, cfg)
					movie, err = NewMovieRepository(db).FindByID(t.Context(), "LEGACY-SNAPSHOT")
					require.NoError(t, err)
					require.Len(t, movie.Credits, 1)
					require.Equal(t, initial.ID, movie.Credits[0].ID)
					require.Equal(t, last+" Person", movie.Credits[0].Actress.FullName())
					require.True(t, movie.RenderDirty)
					require.EqualValues(t, rename+1, movie.RenderGeneration)
					for _, useCredit := range []bool{false, true} {
						requireLegacySnapshotNFO(t, movie, useCredit, last+" Person")
					}
					if names.label != "japanese-fallback" {
						require.Equal(t, last+" Person", movie.Credits[0].CreditedName)
						require.Equal(t, "新名", movie.Credits[0].CreditedJapaneseName)
					}
				}
				var violations []struct{ Table string }
				require.NoError(t, db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error)
				require.Empty(t, violations)
			})
		}
	}
}

func TestLegacySnapshotAllCanonicalRenamePaths(t *testing.T) {
	for _, path := range []string{"name-fields", "identity-fields", "canonical-fields", "import", "adopt-canonical"} {
		t.Run(path, func(t *testing.T) {
			db, cfg := legacySnapshotDB(t, "Solo", "", "", true)
			repo := NewActressRepository(db)
			ctx := t.Context()
			switch path {
			case "name-fields":
				require.NoError(t, repo.RenameNameFields(ctx, 1, "Solo", "New", ""))
			case "identity-fields":
				require.NoError(t, repo.RenameIdentityFields(ctx, 1, "Solo", "New", "", ""))
			case "canonical-fields":
				require.NoError(t, repo.UpdateCanonicalFields(ctx, 1, "Solo", "New", "", ""))
			case "import":
				require.NoError(t, repo.ImportUpsert(ctx, &models.Actress{DMMID: 90101, LastName: "New"}))
			case "adopt-canonical":
				require.NoError(t, db.Create(&models.Movie{ContentID: "reported", ID: "REPORTED"}).Error)
				credit := models.MovieCredit{MovieContentID: "reported", ActressID: 1, CreditedName: "New Solo", Source: "fixture", Origin: string(models.CreditOriginScrape)}
				require.NoError(t, db.Create(&credit).Error)
				collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: "reported", Field: models.CreditFieldCreditedName, ReportedValue: "New Solo", CanonicalValue: "Solo", Status: models.CollisionStatusOpen}
				require.NoError(t, db.Create(&collision).Error)
				_, err := NewCollisionService(db).Resolve(ctx, collision.ID, models.CollisionResolutionAdoptCanonical, 0)
				require.NoError(t, err)
			}
			db = reopenLegacySnapshotDB(t, db, cfg)
			movie, err := NewMovieRepository(db).FindByID(ctx, "LEGACY-SNAPSHOT")
			require.NoError(t, err)
			require.Equal(t, "New Solo", movie.Credits[0].CreditedName)
			requireLegacySnapshotNFO(t, movie, true, "New Solo")
		})
	}
}

func TestLegacySnapshotRefreshPreservesAttribution(t *testing.T) {
	for _, choice := range []string{
		"user-override", "dormant-override", "forced-canonical", "credited-alias", "extra-whitespace",
		"not-inferred", "scrape-origin", "scrape-source", "empty-source", "name-decision", "identity-decision", "reassignment", "already-stale",
	} {
		t.Run(choice, func(t *testing.T) {
			db, cfg := legacySnapshotDB(t, "Solo", "", "旧名", true)
			ctx := t.Context()
			credits, err := NewMovieCreditRepository(db).ListByMovie(ctx, "legacy-snapshot")
			require.NoError(t, err)
			credit := credits[0]
			repo := NewMovieCreditRepository(db)
			actorID := credit.ActressID
			switch choice {
			case "user-override", "dormant-override":
				require.NoError(t, repo.UpdateOverride(ctx, credit.ID, "Chosen Alias", choice == "user-override"))
			case "forced-canonical":
				require.NoError(t, repo.SetDisplayForceCanonical(ctx, credit.ID, true))
			case "credited-alias":
				require.NoError(t, db.Model(&credit).Update("credited_name", "Stage Solo").Error)
			case "extra-whitespace":
				require.NoError(t, db.Model(&credit).Update("credited_name", " Solo ").Error)
			case "not-inferred":
				require.NoError(t, db.Model(&credit).Update("legacy_inferred", false).Error)
			case "scrape-origin":
				require.NoError(t, db.Model(&credit).Update("origin", string(models.CreditOriginScrape)).Error)
			case "scrape-source", "empty-source":
				source := "fixture"
				if choice == "empty-source" {
					source = ""
				}
				require.NoError(t, db.Model(&credit).Update("source", source).Error)
			case "name-decision", "identity-decision":
				field, resolution := models.CreditFieldCreditedName, models.CollisionResolutionAdoptAlias
				if choice == "identity-decision" {
					field, resolution = models.CreditFieldIdentityLink, models.CollisionResolutionKeepIdentity
				}
				// A stored decision is still provenance after canonical display
				// has subsequently been switched off for the credit.
				collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: credit.MovieContentID, Field: field, ReportedValue: "Solo", CanonicalValue: "Solo", Status: models.CollisionStatusResolved, Resolution: resolution}
				require.NoError(t, db.Create(&collision).Error)
			case "reassignment":
				target := models.Actress{DMMID: 90102, FirstName: "Solo", JapaneseName: "旧名"}
				require.NoError(t, NewActressRepository(db).Create(ctx, &target))
				require.NoError(t, repo.ReassignCredit(ctx, &credit, target.ID))
				actorID = target.ID
			case "already-stale":
				// Older versions may already have lost the relation between these
				// names. Do not guess based on aliases or trim-equivalence.
				require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", actorID).Update("first_name", "Earlier Rename").Error)
			}
			var before models.MovieCredit
			require.NoError(t, db.First(&before, credit.ID).Error)
			require.NoError(t, NewActressRepository(db).RenameNameFields(ctx, actorID, "Person", "New", "新名"))
			db = reopenLegacySnapshotDB(t, db, cfg)
			var after models.MovieCredit
			require.NoError(t, db.First(&after, credit.ID).Error)
			require.Equal(t, before, after, "an explicit or non-migration attribution must not be rewritten")
		})
	}
}

func TestLegacySnapshotRefreshPreservesOrderSuppressionAndRollsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			db, cfg := legacySnapshotDB(t, "Solo", "", "旧名", true)
			ctx := t.Context()
			creditRepo := NewMovieCreditRepository(db)
			credits, err := creditRepo.ListByMovie(ctx, "legacy-snapshot")
			require.NoError(t, err)
			credit := credits[0]
			require.NoError(t, creditRepo.UpdateOrderPinned(ctx, credit.ID, 9, true))
			require.NoError(t, creditRepo.UpdateSuppressed(ctx, credit.ID, true))
			if fail {
				require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_snapshot BEFORE UPDATE OF credited_name ON movie_credits BEGIN SELECT RAISE(ABORT, 'injected snapshot failure'); END").Error)
			}
			err = NewActressRepository(db).RenameNameFields(ctx, 1, "Person", "New", "新名")
			if fail {
				require.ErrorContains(t, err, "injected snapshot failure")
			} else {
				require.NoError(t, err)
			}
			db = reopenLegacySnapshotDB(t, db, cfg)
			credits, err = NewMovieCreditRepository(db).ListByMovie(ctx, "legacy-snapshot")
			require.NoError(t, err)
			require.Equal(t, credit.ID, credits[0].ID)
			require.True(t, credits[0].Suppressed)
			require.True(t, credits[0].OrderPinned)
			require.Equal(t, 9, credits[0].OrderIndex)
			if fail {
				require.Equal(t, " Solo", credits[0].CreditedName)
				require.Equal(t, "Solo", credits[0].Actress.FullName())
			} else {
				require.Equal(t, "New Person", credits[0].CreditedName)
			}
		})
	}
}
