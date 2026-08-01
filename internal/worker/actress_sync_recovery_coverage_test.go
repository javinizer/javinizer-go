package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func newRecoveryMovieRepo(t *testing.T, actressID uint) *database.MovieRepository {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "movies.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "recovery", ID: "REC-1", DisplayTitle: "Recovery"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "recovery", actressID).Error)
	return database.NewMovieRepository(db)
}

func TestRecoverMissingDMMIdentityBranches(t *testing.T) {
	callbackErr := errors.New("callback")

	t.Run("identity query error", func(t *testing.T) {
		db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "closed.db")})
		require.NoError(t, err)
		repo := database.NewActressRepository(db)
		require.NoError(t, db.Close())
		_, _, _, err = recoverMissingDMMIdentity(context.Background(), &models.Actress{ID: 1, JapaneseName: "名"}, repo, nil, nil, nil, nil)
		require.Error(t, err)
	})

	t.Run("canonical merge error", func(t *testing.T) {
		_, repo, movieRepo, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名"})
		require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 20, JapaneseName: "同名"}))
		_, _, _, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, nil,
			func(uint, uint) (*database.ActressMergeResult, error) { return nil, callbackErr }, nil)
		require.ErrorIs(t, err, callbackErr)
	})

	t.Run("source snapshot callback", func(t *testing.T) {
		_, repo, movieRepo, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名"})
		canonical := &models.Actress{DMMID: 21, JapaneseName: "同名"}
		require.NoError(t, repo.Create(context.Background(), canonical))
		got, _, fields, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, nil, nil, nil, linkedIdentityRecoveryOptions{
			expectedSource: *target,
			mergeActressesWithSource: func(targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
				require.Equal(t, canonical.ID, targetID)
				require.Equal(t, target.ID, expectedSource.ID)
				return &database.ActressMergeResult{MergedActress: *canonical}, nil
			},
		})
		require.NoError(t, err)
		require.Equal(t, canonical.ID, got.ID)
		require.Equal(t, []string{"merged_duplicate"}, fields)
	})

	t.Run("target snapshot callback", func(t *testing.T) {
		_, repo, movieRepo, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "同名"})
		canonical := &models.Actress{DMMID: 22, JapaneseName: "同名"}
		require.NoError(t, repo.Create(context.Background(), canonical))
		got, _, _, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, nil, nil, nil, linkedIdentityRecoveryOptions{
			expectedSource: *target,
			mergeActressesWithTargetSource: func(targetID, sourceID uint, expectedTarget, expectedSource models.Actress) (*database.ActressMergeResult, error) {
				require.Equal(t, canonical.ID, targetID)
				require.Equal(t, canonical.ID, expectedTarget.ID)
				require.Equal(t, target.ID, expectedSource.ID)
				return &database.ActressMergeResult{MergedActress: *canonical}, nil
			},
		})
		require.NoError(t, err)
		require.Equal(t, canonical.ID, got.ID)
	})

	for _, tc := range []struct {
		name    string
		assign  func(uint, int) (bool, error)
		wantErr bool
		wantNil bool
	}{
		{name: "assign error", assign: func(uint, int) (bool, error) { return false, callbackErr }, wantErr: true},
		{name: "assignment lost", assign: func(uint, int) (bool, error) { return false, nil }, wantNil: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo, _, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "対象"})
			movieRepo := newRecoveryMovieRepo(t, target.ID)
			scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 30, JapaneseName: "対象"}}}}
			got, _, _, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, []models.Scraper{scraper},
				func(uint, uint) (*database.ActressMergeResult, error) { t.Fatal("unexpected merge"); return nil, nil }, tc.assign)
			if tc.wantErr {
				require.ErrorIs(t, err, callbackErr)
			} else {
				require.NoError(t, err)
				require.Nil(t, got)
			}
		})
	}

	t.Run("load after assignment error", func(t *testing.T) {
		db, repo, _, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "対象"})
		movieRepo := newRecoveryMovieRepo(t, target.ID)
		scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 31, JapaneseName: "対象"}}}}
		_, _, _, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, []models.Scraper{scraper}, nil,
			func(uint, int) (bool, error) { require.NoError(t, db.Close()); return true, nil })
		require.Error(t, err)
	})

	t.Run("incompatible existing canonical", func(t *testing.T) {
		_, repo, _, target := newActressSyncFixture(t, &models.Actress{JapaneseName: "対象", FirstName: "Target"})
		require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 32, JapaneseName: "別名", FirstName: "Other"}))
		movieRepo := newRecoveryMovieRepo(t, target.ID)
		scraper := &actressSyncScraper{result: &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 32, JapaneseName: "対象"}}}}
		got, _, _, err := recoverMissingDMMIdentity(context.Background(), target, repo, movieRepo, []models.Scraper{scraper}, nil, nil)
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func TestManagerNoCandidatesNilTaskAndHeartbeat(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "manager.db")})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db)})

	_, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.ErrorContains(t, err, "no actresses")

	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "nil-actress-job", Scope: "selected", Status: models.ActressSyncJobPending, CreatedAt: now}
	task := models.ActressSyncTask{ID: "nil-actress-task", JobID: job.ID, DedupeKey: "nil-actress", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(claimed, time.Second, nil)
	require.Equal(t, models.ActressSyncTaskFailed, claimed.Status)

	heartbeatDone := make(chan struct{})
	started := time.Now()
	manager.heartbeat(context.Background(), "missing", "token", 3*time.Second, heartbeatDone)
	require.GreaterOrEqual(t, time.Since(started), time.Second)
}
