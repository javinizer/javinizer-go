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

func openLegacyLifecycleDB(t *testing.T, foreignKeys bool, name string) *DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), name) + map[bool]string{false: "?_foreign_keys=0", true: "?_foreign_keys=1"}[foreignKeys]
	db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	return db
}

func assertNoForeignKeyViolations(t *testing.T, db *DB) {
	t.Helper()
	rows, err := db.Raw("PRAGMA foreign_key_check").Rows()
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestDeleteStaleCandidatesRetainsLegacyProjectionReference(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			db := openLegacyLifecycleDB(t, foreignKeys, "candidate-legacy.db")
			associated := models.Actress{JapaneseName: "Legacy Candidate", Origin: ActressOriginScrape}
			unrelated := models.Actress{JapaneseName: "Unrelated Candidate", Origin: ActressOriginScrape}
			nullMovieCandidate := models.Actress{JapaneseName: "Null Movie Candidate", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&associated).Error)
			require.NoError(t, db.Create(&unrelated).Error)
			require.NoError(t, db.Create(&nullMovieCandidate).Error)
			movie := models.Movie{ContentID: "legacy-candidate", ID: "LEGACY-CANDIDATE"}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, associated.ID).Error)
			// Legacy schema columns are nullable. A NULL actress_id must not turn a
			// NOT IN guard into UNKNOWN and globally disable unrelated pruning.
			nullMovie := models.Movie{ContentID: "legacy-null-association", ID: "LEGACY-NULL"}
			require.NoError(t, db.Create(&nullMovie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, NULL), (NULL, ?)", nullMovie.ContentID, nullMovieCandidate.ID).Error)
			staleIDs := []uint{associated.ID, unrelated.ID, nullMovieCandidate.ID}
			expectedPruned := int64(2)
			var danglingCandidate models.Actress
			if !foreignKeys {
				danglingCandidate = models.Actress{JapaneseName: "Dangling Movie Candidate", Origin: ActressOriginScrape}
				require.NoError(t, db.Create(&danglingCandidate).Error)
				require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "missing-movie", danglingCandidate.ID).Error)
				staleIDs = append(staleIDs, danglingCandidate.ID)
				expectedPruned++
			}
			stale := time.Now().UTC().Add(-48 * time.Hour)
			require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id IN ?", stale, staleIDs).Error)

			pruned, err := NewActressRepository(db).DeleteStaleCandidates(t.Context(), time.Now().UTC().Add(-24*time.Hour))
			require.NoError(t, err)
			require.EqualValues(t, expectedPruned, pruned)
			var preserved models.Actress
			require.NoError(t, db.First(&preserved, associated.ID).Error)
			var associationCount int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, associated.ID).Count(&associationCount).Error)
			require.EqualValues(t, 1, associationCount)
			for _, id := range []uint{unrelated.ID, nullMovieCandidate.ID, danglingCandidate.ID} {
				if id != 0 {
					require.ErrorIs(t, db.First(&models.Actress{}, id).Error, gorm.ErrRecordNotFound)
				}
			}
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id IS NULL OR movie_content_id = ?", "missing-movie").Count(&associationCount).Error)
			require.Zero(t, associationCount)
			assertNoForeignKeyViolations(t, db)
		})
	}
}

func TestDeleteStaleCandidatesLegacyGuardQueryFailureRollsBack(t *testing.T) {
	db := openLegacyLifecycleDB(t, false, "candidate-guard-failure.db")
	candidate := models.Actress{JapaneseName: "Guard Failure", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id = ?", time.Now().UTC().Add(-48*time.Hour), candidate.ID).Error)
	require.NoError(t, db.Exec("DROP TABLE movie_actresses").Error)

	pruned, err := NewActressRepository(db).DeleteStaleCandidates(t.Context(), time.Now().UTC().Add(-24*time.Hour))
	require.ErrorContains(t, err, "stale candidate")
	require.Zero(t, pruned)
	require.NoError(t, db.First(&models.Actress{}, candidate.ID).Error)
}

func TestLegacyOnlyDeleteInvalidatesRenderAndPublicationFence(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			db := openLegacyLifecycleDB(t, foreignKeys, "delete-legacy.db")
			actress := models.Actress{JapaneseName: "Legacy Delete", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			movie := models.Movie{ContentID: "legacy-delete", ID: "LEGACY-DELETE", RenderGeneration: 7}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?), (NULL, ?)", movie.ContentID, actress.ID, actress.ID).Error)

			require.NoError(t, NewActressRepository(db).Delete(t.Context(), actress.ID))
			stored := loadArtifactPublicationMovie(t, db, movie.ContentID)
			require.True(t, stored.RenderDirty)
			require.EqualValues(t, 8, stored.RenderGeneration)
			var count int64
			require.NoError(t, db.Table("movie_actresses").Where("actress_id = ?", actress.ID).Count(&count).Error)
			require.Zero(t, count)
			called := false
			repo := NewMovieRepository(db)
			require.ErrorIs(t, repo.WithApplyArtifactPublicationFence(context.Background(), movie.ContentID, 7, func(*models.Movie) error { called = true; return nil }), ErrApplyPublicationStale)
			require.False(t, called)
			require.NoError(t, repo.WithApplyArtifactPublicationFence(context.Background(), movie.ContentID, 8, func(*models.Movie) error { called = true; return nil }))
			require.True(t, called)
			assertNoForeignKeyViolations(t, db)
		})
	}
}

func TestLegacyOnlyCanonicalEditsInvalidateExactlyOnce(t *testing.T) {
	for _, operation := range []string{"rename", "canonical"} {
		t.Run(operation, func(t *testing.T) {
			db := openLegacyLifecycleDB(t, false, operation+"-legacy.db")
			actress := models.Actress{FirstName: "Before", LastName: "Name", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			movie := models.Movie{ContentID: "legacy-" + operation, ID: "LEGACY-EDIT", RenderGeneration: 29}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?), (NULL, ?)", movie.ContentID, actress.ID, actress.ID).Error)

			var err error
			if operation == "rename" {
				err = NewActressRepository(db).RenameNameFields(t.Context(), actress.ID, "After", actress.LastName, actress.JapaneseName)
			} else {
				err = NewActressRepository(db).UpdateCanonicalFields(t.Context(), actress.ID, "After", actress.LastName, actress.JapaneseName, "new-thumb")
			}
			require.NoError(t, err)
			stored := loadArtifactPublicationMovie(t, db, movie.ContentID)
			require.True(t, stored.RenderDirty)
			require.EqualValues(t, 30, stored.RenderGeneration)
		})
	}
}

func TestLegacyOnlyMergeInvalidatesExactlyOnceAndAvoidsNoopBump(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			db := openLegacyLifecycleDB(t, foreignKeys, "merge-legacy.db")
			target := models.Actress{JapaneseName: "Legacy Target", Verified: true, Origin: ActressOriginUser}
			source := models.Actress{JapaneseName: "Legacy Source", Verified: true, Origin: ActressOriginUser}
			noopSource := models.Actress{JapaneseName: "Legacy Target", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, db.Create(&noopSource).Error)
			changed := models.Movie{ContentID: "legacy-merge-changed", ID: "LEGACY-MERGE-CHANGED", RenderGeneration: 11}
			unchanged := models.Movie{ContentID: "legacy-merge-noop", ID: "LEGACY-MERGE-NOOP", RenderGeneration: 19}
			require.NoError(t, db.Create(&changed).Error)
			require.NoError(t, db.Create(&unchanged).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?), (?, ?), (NULL, ?)", changed.ContentID, source.ID, unchanged.ContentID, target.ID, source.ID).Error)
			if !foreignKeys {
				require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "missing-merge-movie", source.ID).Error)
			}

			_, err := NewActressRepository(db).Merge(t.Context(), target.ID, source.ID, map[string]string{"japanese_name": MergeResolutionSource})
			require.NoError(t, err)
			changedAfter := loadArtifactPublicationMovie(t, db, changed.ContentID)
			require.True(t, changedAfter.RenderDirty)
			require.EqualValues(t, 12, changedAfter.RenderGeneration)
			var ids []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", changed.ContentID).Pluck("actress_id", &ids).Error)
			require.Equal(t, []uint{target.ID}, ids)
			var nullAssociationCount int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id IS NULL OR movie_content_id = ?", "missing-merge-movie").Count(&nullAssociationCount).Error)
			require.Zero(t, nullAssociationCount)

			// The first merge also changes the target's canonical name, so its existing
			// projection is legitimately invalidated. Simulate publishing that result,
			// then prove a merge that leaves the rendered target unchanged does not bump.
			require.NoError(t, db.Model(&models.Movie{}).Where("content_id = ?", unchanged.ContentID).Update("render_dirty", false).Error)
			noopBaseline := loadArtifactPublicationMovie(t, db, unchanged.ContentID)
			noopSource.JapaneseName = "Legacy Source"
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", noopSource.ID).Update("japanese_name", noopSource.JapaneseName).Error)
			_, err = NewActressRepository(db).Merge(t.Context(), target.ID, noopSource.ID, nil)
			require.NoError(t, err)
			unchangedAfter := loadArtifactPublicationMovie(t, db, unchanged.ContentID)
			require.False(t, unchangedAfter.RenderDirty)
			require.Equal(t, noopBaseline.RenderGeneration, unchangedAfter.RenderGeneration)
			assertNoForeignKeyViolations(t, db)
		})
	}
}

func TestLegacyNullMovieAssociationCleanupFailureRollsBackMerge(t *testing.T) {
	db := openLegacyLifecycleDB(t, false, "null-association-rollback.db")
	target := models.Actress{JapaneseName: "Null Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{JapaneseName: "Null Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (NULL, ?)", source.ID).Error)
	require.NoError(t, db.Exec(`
CREATE TRIGGER fail_null_association_cleanup
BEFORE DELETE ON movie_actresses
WHEN OLD.movie_content_id IS NULL
BEGIN SELECT RAISE(ABORT, 'null cleanup failure'); END`).Error)

	_, err := NewActressRepository(db).Merge(t.Context(), target.ID, source.ID, map[string]string{"japanese_name": MergeResolutionSource})
	require.ErrorContains(t, err, "null cleanup failure")
	var preservedSource, preservedTarget models.Actress
	require.NoError(t, db.First(&preservedSource, source.ID).Error)
	require.NoError(t, db.First(&preservedTarget, target.ID).Error)
	require.Equal(t, "Null Target", preservedTarget.JapaneseName)
	var count int64
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id IS NULL AND actress_id = ?", source.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestLegacyOnlyIdentityMutationInvalidationFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"delete", "merge"} {
		t.Run(operation, func(t *testing.T) {
			db := openLegacyLifecycleDB(t, false, operation+"-rollback.db")
			target := models.Actress{JapaneseName: "Rollback Target", Verified: true, Origin: ActressOriginUser}
			source := models.Actress{JapaneseName: "Rollback Source", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			movie := models.Movie{ContentID: "legacy-" + operation + "-rollback", ID: "ROLLBACK", RenderGeneration: 23}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)
			require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_generation BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'generation failure'); END").Error)

			var err error
			if operation == "delete" {
				err = NewActressRepository(db).Delete(t.Context(), source.ID)
			} else {
				_, err = NewActressRepository(db).Merge(t.Context(), target.ID, source.ID, map[string]string{"japanese_name": MergeResolutionSource})
			}
			require.ErrorContains(t, err, "invalidate render")
			require.NoError(t, db.First(&models.Actress{}, source.ID).Error)
			var count int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, source.ID).Count(&count).Error)
			require.EqualValues(t, 1, count)
			stored := loadArtifactPublicationMovie(t, db, movie.ContentID)
			require.False(t, stored.RenderDirty)
			require.EqualValues(t, 23, stored.RenderGeneration)
		})
	}
}
