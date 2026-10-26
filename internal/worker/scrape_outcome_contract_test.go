package worker

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRescrapeUnpublishedFailuresPreserveCommittedResult(t *testing.T) {
	commitErr := errors.New("result store unavailable")
	for _, tc := range []struct {
		name      string
		wf        workflow.WorkflowInterface
		commitErr error
		wantError string
		wantRows  int
	}{
		{name: "workflow panic", wf: &panicScrapeWorkflow{}, wantError: "intentional test panic", wantRows: 1},
		{name: "nil scrape result", wf: &stubRescrapeWorkflow{}},
		{name: "commit failure", wf: &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: &models.Movie{ID: "NEW-001", Title: "Uncommitted"}}}, commitErr: commitErr, wantError: commitErr.Error(), wantRows: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const file = "/synthetic/OLD-001.mp4"
			db := newHistoryTestDB(t)
			rm := newStubResultMap()
			previous := &resultstore.MovieResult{Movie: &models.Movie{ID: "OLD-001", Title: "Committed"}, Status: models.JobStatusCompleted, Persisted: true}
			rm.results[file] = previous
			rm.commitErr = tc.commitErr
			inputs := makeRescrapeInputs(tc.wf)
			inputs.ResultMap = rm
			inputs.Finder = resultstore.New(1, []string{file})
			inputs.Fs = afero.NewMemMapFs()
			inputs.HistoryRepo = db.Repositories().HistoryRepo

			got, err := NewRescrapePhase().Rescrape(t.Context(), inputs, RescrapeCmd{FilePath: file, MovieID: "NEW-001"})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, got, "failed rescrape must not publish a successful response")
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
				require.Equal(t, models.RescrapeStatusFailed, got.Status)
				require.Equal(t, "scrape produced no result", got.Error)
			}
			if tc.commitErr != nil {
				require.ErrorIs(t, err, tc.commitErr)
			}
			current, readErr := rm.GetMovieResult(file)
			require.NoError(t, readErr)
			require.Same(t, previous, current)
			require.Equal(t, "Committed", current.Movie.Title)
			require.Equal(t, "OLD-001", current.Movie.ID)
			require.Zero(t, current.Revision)
			require.Empty(t, rm.getCommitted())
			records, readErr := inputs.HistoryRepo.FindByBatchJobID(t.Context(), inputs.JobID.String())
			require.NoError(t, readErr)
			require.Len(t, records, tc.wantRows)
			for _, row := range records {
				require.Equal(t, models.HistoryStatusFailed, row.Status)
				require.Equal(t, file, row.OriginalPath)
				require.Contains(t, row.ErrorMessage, tc.wantError)
			}
		})
	}
}

func TestScrapePersistPanicNotifiesAndAuditsFailureOnce(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	const callbackName = "test:panic_movie_persist"
	callbackCalls := 0
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "movies" {
			callbackCalls++
			panic("synthetic movie persistence panic")
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove(callbackName)) })
	const file = "/synthetic/PANIC-001.mp4"
	movie := &models.Movie{ID: "PANIC-001", ContentID: "panic001", Title: "Not saved"}
	tracker := resultstore.New(1, []string{file})
	tracker.UpdateFileResult(file, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, RecoveryWarning: "obsolete warning"})
	inputs := scrapePhaseInputs{JobID: "panic-persist", MovieRepo: repos.MovieRepo, HistoryRepo: repos.HistoryRepo, Updater: tracker}
	outcome := scrapeFileOutcome{FilePath: file, MovieID: movie.ID, Success: true, Result: &scrape.ScrapeResult{Movie: movie, Status: scrape.StatusCompleted}}
	ch := make(chan scrapeFileOutcome, 1)
	ch <- outcome
	close(ch)
	var failures []string
	recorded := persistScrapeOutcomes(t.Context(), ch, inputs, func(path, id, message string) {
		require.Equal(t, file, path)
		require.Equal(t, movie.ID, id)
		failures = append(failures, message)
	})
	require.Equal(t, 1, callbackCalls, "panic must originate in the real persistence transaction")
	require.Equal(t, map[string]bool{file: true}, recorded)
	require.Equal(t, []string{"persist panic: synthetic movie persistence panic"}, failures)
	trackScrapeResults(inputs, []scrapeFileOutcome{outcome}, recorded)
	published, err := tracker.GetMovieResult(file)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, published.Status)
	require.False(t, published.Persisted)
	require.Empty(t, published.RecoveryWarning)
	require.Equal(t, failures[0], published.Error)
	records, err := repos.HistoryRepo.FindByBatchJobID(t.Context(), inputs.JobID.String())
	require.NoError(t, err)
	require.Len(t, records, 1, "tracking must not append success after persistence recovered a panic")
	require.Equal(t, models.HistoryStatusFailed, records[0].Status)
	require.Equal(t, failures[0], records[0].ErrorMessage)
	var count int64
	require.NoError(t, db.Model(&models.Movie{}).Count(&count).Error)
	require.Zero(t, count, "the panicking transaction must roll back")
}

func TestScrapePersistTranslationsLeavesSharedMovieUntouched(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	actress := models.Actress{DMMID: 880001, FirstName: "Original", LastName: "Person", Verified: true, Origin: "user"}
	require.NoError(t, repos.ActressRepo.Create(t.Context(), &actress))
	movie := &models.Movie{ID: "TRANS-001", ContentID: "trans001", Title: "Original title", Genres: []models.Genre{{Name: "Original genre"}}, Actresses: []models.Actress{actress}}
	scrape.BuildCreditsFromScrape(movie, nil, nil)
	before := movie.Clone()
	output := &translation.TranslationOutput{
		GenreTranslations:   []models.GenreTranslationData{{GenreIndex: 0, Language: "en", Name: "Translated genre", SourceName: "fixture"}},
		ActressTranslations: []models.ActressTranslationData{{ActressIndex: 0, Language: "en", FirstName: "Translated", LastName: "Person", DisplayName: "Translated Person", SourceName: "fixture"}},
	}
	const file = "/synthetic/TRANS-001.mp4"
	tracker := resultstore.New(1, []string{file})
	tracker.UpdateFileResult(file, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted})
	inputs := scrapePhaseInputs{JobID: "translations-persist", MovieRepo: repos.MovieRepo, HistoryRepo: repos.HistoryRepo, Updater: tracker}
	outcome := scrapeFileOutcome{FilePath: file, MovieID: movie.ID, Success: true, Result: &scrape.ScrapeResult{Movie: movie, Status: scrape.StatusCompleted, TranslationOutput: output}}
	require.True(t, persistScrapeOutcome(t.Context(), outcome, inputs, nil))
	require.Equal(t, before, movie, "DB association/translation resolution must not mutate readers' shared movie")
	stored, err := repos.MovieRepo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Len(t, stored.Genres, 1)
	var genres []models.GenreTranslation
	require.NoError(t, db.Find(&genres).Error)
	require.Len(t, genres, 1)
	require.Equal(t, stored.Genres[0].ID, genres[0].GenreID)
	require.Equal(t, "Translated genre", genres[0].Name)
	require.Equal(t, "en", genres[0].Language)
	require.Equal(t, "fixture", genres[0].SourceName)
	var actresses []models.ActressTranslation
	require.NoError(t, db.Find(&actresses).Error)
	require.Len(t, actresses, 1)
	require.Equal(t, actress.ID, actresses[0].ActressID)
	require.Equal(t, "Translated Person", actresses[0].DisplayName)
	require.Equal(t, "en", actresses[0].Language)
	require.Equal(t, "fixture", actresses[0].SourceName)
	published, err := tracker.GetMovieResult(file)
	require.NoError(t, err)
	require.True(t, published.Persisted)
	require.NotSame(t, movie, published.Movie)
	require.Equal(t, stored.Genres, published.Movie.Genres)
	records, err := repos.HistoryRepo.FindByBatchJobID(t.Context(), inputs.JobID.String())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, models.HistoryStatusSuccess, records[0].Status)
}

func TestOrganizeRecoveryWarningSurvivesHistoryRoundTrip(t *testing.T) {
	db := newHistoryTestDB(t)
	repo := db.Repositories().HistoryRepo
	const warning = "Output committed; transaction backup cleanup pending. Do not reapply."
	movie := &models.Movie{ID: "RECOVERY-001"}
	result := &workflow.ApplyResult{Movie: movie, PublicationFinalized: true, RecoveryWarning: warning, VideoOutcome: "replaced", OrganizeResult: &organizer.OrganizeResult{NewPath: "/synthetic/output.mp4", Warnings: []string{"backup retained"}}}
	inputs := applyPhaseInputs{JobID: "recovery-history", HistoryRepo: repo, OperationMode: "organize"}
	auditOrganizeSuccess(inputs, movie, "/synthetic/input.mp4", result, ApplyPhaseConfig{})
	records, err := repo.FindByBatchJobID(t.Context(), inputs.JobID.String())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, models.HistoryStatusSuccess, records[0].Status)
	require.Equal(t, result.OrganizeResult.NewPath, records[0].NewPath)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(records[0].Metadata), &metadata))
	require.Equal(t, warning, metadata["recovery_warning"])
	require.Equal(t, "replaced", metadata["video_outcome"])
	require.Equal(t, "organize", metadata["operation_mode"])
	require.Equal(t, []any{"backup retained"}, metadata["warnings"])
}
