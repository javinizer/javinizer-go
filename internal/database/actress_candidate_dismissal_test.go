package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCandidateDismissalQueueLifecycle(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	stamp := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	candidates := []models.Actress{
		{FirstName: "One", Origin: ActressOriginScrape},
		{FirstName: "Two", Origin: ActressOriginScrape},
		{FirstName: "Three", Origin: ActressOriginScrape},
		{FirstName: "Verified", Origin: ActressOriginUser, Verified: true},
	}
	for i := range candidates {
		require.NoError(t, db.Create(&candidates[i]).Error)
		require.NoError(t, db.Model(&candidates[i]).UpdateColumn("updated_at", stamp).Error)
	}
	count, err := repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.Zero(t, count, "new migration must not dismiss existing candidates")
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidates[0].ID, true))
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidates[2].ID, true))
	var dismissedAt string
	require.NoError(t, db.Raw("SELECT dismissed_at FROM actress_candidate_dismissals WHERE actress_id = ?", candidates[0].ID).Scan(&dismissedAt).Error)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidates[0].ID, true))
	var repeatedAt string
	require.NoError(t, db.Raw("SELECT dismissed_at FROM actress_candidate_dismissals WHERE actress_id = ?", candidates[0].ID).Scan(&repeatedAt).Error)
	require.Equal(t, dismissedAt, repeatedAt)
	count, err = repo.CountCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	pending, err := repo.ListCandidates(t.Context(), 1, 0)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, candidates[1].ID, pending[0].ID)
	count, err = repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	for offset, expected := range []uint{candidates[0].ID, candidates[2].ID} {
		page, err := repo.ListDismissedCandidates(t.Context(), 1, offset)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, expected, page[0].ID, "equal timestamps must use deterministic ID ordering")
	}
	empty, err := repo.ListDismissedCandidates(t.Context(), 1, 2)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.ErrorIs(t, repo.PromoteCandidate(t.Context(), candidates[0].ID, "Promoted", "", "", ""), ErrCandidateDismissed)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidates[0].ID, false))
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidates[0].ID, false))
	require.NoError(t, repo.PromoteCandidate(t.Context(), candidates[0].ID, "Promoted", "", "", ""))
	for _, dismissed := range []bool{false, true} {
		require.ErrorIs(t, repo.SetCandidateDismissed(t.Context(), candidates[0].ID, dismissed), ErrCandidateAlreadyVerified)
		require.ErrorIs(t, repo.SetCandidateDismissed(t.Context(), 0, dismissed), ErrInvalidLookup)
		require.True(t, IsNotFound(repo.SetCandidateDismissed(t.Context(), 99999, dismissed)))
	}
}

func TestCandidateDismissalPreservesIdentityCreditsAndEvidence(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{DMMID: 98123, FirstName: "Candidate", JapaneseName: "候補", Origin: ActressOriginScrape, AmbiguityQuarantined: true}
	require.NoError(t, db.Create(&candidate).Error)
	for _, movieID := range []string{"dismiss-visible", "dismiss-suppressed"} {
		movie := models.Movie{ContentID: movieID, ID: movieID, RenderGeneration: 9}
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: movieID, ActressID: candidate.ID, CreditedName: "Source Evidence", Source: "test", Suppressed: movieID == "dismiss-suppressed"}
		require.NoError(t, db.Create(&credit).Error)
		require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movieID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen, ReportedValue: "Source Evidence"}).Error)
	}
	snapshot := func() map[string][]map[string]any {
		result := map[string][]map[string]any{}
		for _, table := range []string{"actresses", "movies", "movie_credits", "credit_collisions", "movie_actresses", "movie_credit_reassignments", "actress_aliases", "actress_translations"} {
			var rows []map[string]any
			require.NoError(t, db.Table(table).Find(&rows).Error)
			result[table] = rows
		}
		return result
	}
	before := snapshot()
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, true))
	require.Equal(t, before, snapshot())
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, false))
	require.Equal(t, before, snapshot())
}

func TestCandidateDismissalSurvivesRescrapeAndGC(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	repo := NewActressRepository(db)
	makeMovie := func() *models.Movie {
		return creditMovie("dismiss-rescrape", []models.MovieCredit{{CreditedName: "Source", Scraped: models.Actress{DMMID: 98321, FirstName: "Source"}}})
	}
	saved, err := repos.MovieRepo.UpsertWithTranslations(t.Context(), makeMovie(), nil, nil)
	require.NoError(t, err)
	id := saved.Credits[0].ActressID
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), id, true))
	saved, err = repos.MovieRepo.UpsertWithTranslations(t.Context(), makeMovie(), nil, nil)
	require.NoError(t, err)
	require.Equal(t, id, saved.Credits[0].ActressID)
	require.False(t, saved.Credits[0].Suppressed)
	count, err := repo.CountCandidates(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	orphan := models.Actress{FirstName: "Dismissed orphan", Origin: ActressOriginScrape}
	stale := models.Actress{FirstName: "Stale orphan", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&orphan).Error)
	require.NoError(t, db.Create(&stale).Error)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), orphan.ID, true))
	pruned, err := repo.DeleteStaleCandidates(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), pruned)
	var stored models.Actress
	require.NoError(t, db.First(&stored, orphan.ID).Error)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), orphan.ID, false))
}

func TestCandidateDismissalDeleteAndMergeCleanup(t *testing.T) {
	for _, foreignKeys := range []string{"ON", "OFF"} {
		t.Run(foreignKeys, func(t *testing.T) {
			db := newCreditTestDB(t)
			require.NoError(t, db.Exec("PRAGMA foreign_keys = "+foreignKeys).Error)
			repo := NewActressRepository(db)
			for _, operation := range []string{"delete", "merge"} {
				source := models.Actress{FirstName: "Candidate " + operation, Origin: ActressOriginScrape}
				require.NoError(t, db.Create(&source).Error)
				require.NoError(t, repo.SetCandidateDismissed(t.Context(), source.ID, true))
				if operation == "delete" {
					require.NoError(t, repo.Delete(t.Context(), source.ID))
				} else {
					target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
					require.NoError(t, db.Create(&target).Error)
					_, err := repo.Merge(t.Context(), target.ID, source.ID, nil)
					require.NoError(t, err)
				}
				var count int64
				require.NoError(t, db.Table("actress_candidate_dismissals").Count(&count).Error)
				require.Zero(t, count)
			}
		})
	}
}

type dismissalRaceContextKey struct{}

func TestCandidateDismissalPromotionRace(t *testing.T) {
	for _, pausedOperation := range []string{"dismiss", "promote"} {
		t.Run(pausedOperation, func(t *testing.T) {
			db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "dismissal-race.db"), LogLevel: "silent"})
			require.NoError(t, err)
			workerJoined := true // No worker exists until all setup succeeds.
			t.Cleanup(func() {
				if workerJoined {
					_ = db.Close()
				}
			})
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			repo := NewActressRepository(db)
			candidate := models.Actress{FirstName: "Candidate", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), dismissalRaceContextKey{}, true), 10*time.Second)
			defer cancel()
			read := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
			var once sync.Once
			callback := "test:pause_dismissal_candidate_read"
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement == nil || tx.Statement.Context.Value(dismissalRaceContextKey{}) != true {
					return
				}
				loaded, ok := tx.Statement.Dest.(*models.Actress)
				if !ok || loaded.ID != candidate.ID {
					return
				}
				once.Do(func() {
					close(read)
					select {
					case <-release:
					case <-ctx.Done():
					}
				})
			}))
			t.Cleanup(func() {
				if workerJoined {
					_ = db.Callback().Query().Remove(callback)
				}
			})
			result := make(chan error, 1)
			done := make(chan struct{})
			// Registered after callback/DB cleanup, so every early Fatal releases
			// and joins the worker before those resources are torn down.
			t.Cleanup(func() {
				releaseWorker()
				cancel()
				select {
				case <-done:
					workerJoined = true
					// The success path may already have consumed the result.
					select {
					case <-result:
					default:
					}
				case <-time.After(10 * time.Second):
					// Do not race callback removal or DB closure against a stuck
					// worker; fail the test without tearing its resources down.
					t.Error("candidate dismissal race worker did not stop after cancellation")
				}
			})
			workerJoined = false
			go func() {
				defer close(done)
				if pausedOperation == "dismiss" {
					result <- repo.SetCandidateDismissed(ctx, candidate.ID, true)
				} else {
					result <- repo.PromoteCandidate(ctx, candidate.ID, "Promoted", "", "", "")
				}
			}()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if pausedOperation == "dismiss" {
				err = repo.PromoteCandidate(t.Context(), candidate.ID, "Promoted", "", "", "")
			} else {
				err = repo.SetCandidateDismissed(t.Context(), candidate.ID, true)
			}
			releaseWorker()
			require.NoError(t, err)
			select {
			case err = <-result:
				if pausedOperation == "dismiss" {
					require.ErrorIs(t, err, ErrCandidateAlreadyVerified)
				} else {
					require.ErrorIs(t, err, ErrCandidateDismissed)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, db.First(&candidate, candidate.ID).Error)
			dismissed, err := candidateDismissedTx(db.DB, candidate.ID)
			require.NoError(t, err)
			require.NotEqual(t, candidate.Verified, dismissed, "only the winner's state may commit")
		})
	}
}
