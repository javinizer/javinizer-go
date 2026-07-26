package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditMatrix_Scrape(t *testing.T) {
	type testCase struct {
		name       string
		outcome    scrapeFileOutcome
		recorded   map[string]bool
		wantRows   int
		wantStatus models.HistoryStatus
		wantOp     models.HistoryOperation
	}

	cases := []testCase{
		{
			name:     "success not persisted (pre-cancel): one success row",
			outcome:  scrapeFileOutcome{FilePath: "/in/a.mp4", MovieID: "A-001", Success: true, Result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "A-001"}, Status: scrape.StatusCompleted}},
			recorded: nil,
			wantRows: 1, wantStatus: models.HistoryStatusSuccess, wantOp: models.HistoryOpScrape,
		},
		{
			name:     "success already persisted: no row from track",
			outcome:  scrapeFileOutcome{FilePath: "/in/b.mp4", MovieID: "B-001", Success: true, Result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "B-001"}, Status: scrape.StatusCompleted}},
			recorded: map[string]bool{"/in/b.mp4": true},
			wantRows: 0,
		},
		{
			name:     "scraper failure: one failed row",
			outcome:  scrapeFileOutcome{FilePath: "/in/c.mp4", MovieID: "C-001", Failed: true, ErrorMsg: "no results"},
			recorded: nil,
			wantRows: 1, wantStatus: models.HistoryStatusFailed, wantOp: models.HistoryOpScrape,
		},
		{
			name:     "scraper panic: one failed row",
			outcome:  scrapeFileOutcome{FilePath: "/in/d.mp4", MovieID: "D-001", Panic: true, PanicMsg: "boom"},
			recorded: nil,
			wantRows: 1, wantStatus: models.HistoryStatusFailed, wantOp: models.HistoryOpScrape,
		},
		{
			name:     "cancellation: no row",
			outcome:  scrapeFileOutcome{FilePath: "/in/e.mp4", MovieID: "E-001", Failed: true, Cancelled: true, ErrorMsg: "canceled"},
			recorded: nil,
			wantRows: 0,
		},
		{
			name:     "success + cancellation: no row (cancelled)",
			outcome:  scrapeFileOutcome{FilePath: "/in/f.mp4", MovieID: "F-001", Success: true, Cancelled: true},
			recorded: nil,
			wantRows: 0,
		},
		{
			name:     "success no result movie: no row",
			outcome:  scrapeFileOutcome{FilePath: "/in/g.mp4", MovieID: "G-001", Success: true, Result: &scrape.ScrapeResult{Movie: nil}},
			recorded: nil,
			wantRows: 0,
		},
		{
			name:     "panic takes precedence over failed",
			outcome:  scrapeFileOutcome{FilePath: "/in/h.mp4", MovieID: "H-001", Panic: true, Failed: true, PanicMsg: "panic", ErrorMsg: "error"},
			recorded: nil,
			wantRows: 1, wantStatus: models.HistoryStatusFailed, wantOp: models.HistoryOpScrape,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newHistoryTestDB(t)
			repos := db.Repositories()
			inputs := scrapePhaseInputs{JobID: "matrix-job", HistoryRepo: repos.HistoryRepo}
			trackScrapeResults(inputs, []scrapeFileOutcome{tc.outcome}, tc.recorded)
			records, err := repos.HistoryRepo.FindByBatchJobID(context.Background(), "matrix-job")
			require.NoError(t, err)
			assert.Len(t, records, tc.wantRows)
			if tc.wantRows == 1 {
				assert.Equal(t, tc.wantStatus, records[0].Status)
				assert.Equal(t, tc.wantOp, records[0].Operation)
				assert.Equal(t, tc.outcome.FilePath, records[0].OriginalPath)
				if tc.outcome.Panic {
					assert.Equal(t, tc.outcome.PanicMsg, records[0].ErrorMessage)
				} else if tc.outcome.Failed {
					assert.Equal(t, tc.outcome.ErrorMsg, records[0].ErrorMessage)
				}
			}
		})
	}
}

func TestAuditMatrix_Organize(t *testing.T) {
	type testCase struct {
		name         string
		outcome      applyFileOutcome
		skipOrganize bool
		result       *workflow.ApplyResult
		applyErr     error
		wantRows     int
		wantStatus   models.HistoryStatus
		wantNewPath  string
	}

	organizeResult := &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/out/movie.mp4"},
	}

	cases := []testCase{
		{
			name:     "success with organize result: one success row with NewPath",
			outcome:  applyFileOutcome{FilePath: "/in/a.mp4", MovieID: "A-001", Success: true},
			result:   organizeResult,
			wantRows: 1, wantStatus: models.HistoryStatusSuccess, wantNewPath: "/out/movie.mp4",
		},
		{
			name:     "success without organize result (metadata-artwork): no row",
			outcome:  applyFileOutcome{FilePath: "/in/b.mp4", MovieID: "B-001", Success: true},
			result:   &workflow.ApplyResult{},
			wantRows: 0,
		},
		{
			name:     "failure with organize result: one failed row",
			outcome:  applyFileOutcome{FilePath: "/in/c.mp4", MovieID: "C-001", Failed: true, ErrorMsg: "apply failed"},
			result:   organizeResult,
			applyErr: errors.New("apply failed"),
			wantRows: 1, wantStatus: models.HistoryStatusFailed,
		},
		{
			name:         "failure without organize result (organize skipped): no row",
			outcome:      applyFileOutcome{FilePath: "/in/d.mp4", MovieID: "D-001", Failed: true, ErrorMsg: "download failed"},
			skipOrganize: true,
			result:       &workflow.ApplyResult{},
			applyErr:     errors.New("download failed"),
			wantRows:     0,
		},
		{
			name:     "cancellation: no row",
			outcome:  applyFileOutcome{FilePath: "/in/e.mp4", MovieID: "E-001", Cancelled: true, ErrorMsg: "canceled"},
			result:   &workflow.ApplyResult{},
			applyErr: context.Canceled,
			wantRows: 0,
		},
		{
			name:     "cancellation with organize result (moved then cancelled): one success row",
			outcome:  applyFileOutcome{FilePath: "/in/f.mp4", MovieID: "F-001", Cancelled: true},
			result:   organizeResult,
			applyErr: context.Canceled,
			wantRows: 1, wantStatus: models.HistoryStatusSuccess, wantNewPath: "/out/movie.mp4",
		},
		{
			name:     "panic with organize enabled: one failed row from trackApplyResults",
			outcome:  applyFileOutcome{FilePath: "/in/g.mp4", MovieID: "G-001", Panic: true, Failed: true, PanicMsg: "boom"},
			wantRows: 1, wantStatus: models.HistoryStatusFailed,
		},
		{
			name:         "panic with organize skipped: no row",
			outcome:      applyFileOutcome{FilePath: "/in/h.mp4", MovieID: "H-001", Panic: true, Failed: true, PanicMsg: "boom"},
			skipOrganize: true,
			wantRows:     0,
		},
		{
			name:     "nil result on success path: no row",
			outcome:  applyFileOutcome{FilePath: "/in/i.mp4", MovieID: "I-001", Success: true},
			result:   nil,
			wantRows: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newHistoryTestDB(t)
			repos := db.Repositories()
			inputs := applyPhaseInputs{
				JobID:           "matrix-org-job",
				HistoryRepo:     repos.HistoryRepo,
				OrganizeSkipped: tc.skipOrganize,
				OperationMode:   "organize",
			}
			movie := &models.Movie{ID: tc.outcome.MovieID, Title: "Test"}
			cfg := ApplyPhaseConfig{}

			// Simulate interpretApplyResult's history decision
			if tc.outcome.Success {
				auditOrganizeSuccess(inputs, movie, tc.outcome.FilePath, tc.result, cfg)
			} else if tc.outcome.Failed && !tc.outcome.Cancelled && !tc.outcome.Panic {
				if !tc.skipOrganize || (tc.result != nil && tc.result.OrganizeResult != nil) {
					auditOrganizeFailure(inputs, movie, tc.outcome.FilePath, tc.result, tc.applyErr, cfg)
				}
			} else if tc.outcome.Cancelled && tc.result != nil && tc.result.OrganizeResult != nil {
				auditOrganizeSuccess(inputs, movie, tc.outcome.FilePath, tc.result, cfg)
			}

			// Simulate trackApplyResults panic decision
			if tc.outcome.Panic && !tc.outcome.Cancelled && !tc.skipOrganize {
				auditOrganizePanic(inputs, tc.outcome)
			}

			records, err := repos.HistoryRepo.FindByBatchJobID(context.Background(), "matrix-org-job")
			require.NoError(t, err)
			assert.Len(t, records, tc.wantRows)
			if tc.wantRows == 1 {
				assert.Equal(t, tc.wantStatus, records[0].Status)
				assert.Equal(t, models.HistoryOpOrganize, records[0].Operation)
				if tc.wantNewPath != "" {
					assert.Equal(t, tc.wantNewPath, records[0].NewPath)
				}
				if tc.outcome.Panic {
					assert.Equal(t, tc.outcome.PanicMsg, records[0].ErrorMessage)
				} else if tc.outcome.Failed {
					assert.Equal(t, tc.outcome.ErrorMsg, records[0].ErrorMessage)
				}
			}
		})
	}
}

func TestAuditMatrix_Rescrape(t *testing.T) {
	type testCase struct {
		name       string
		err        error
		wantRows   int
		wantStatus models.HistoryStatus
	}

	cases := []testCase{
		{
			name:     "success: one success row",
			err:      nil,
			wantRows: 1, wantStatus: models.HistoryStatusSuccess,
		},
		{
			name:     "scraper failure: one failed row",
			err:      errors.New("scraper error"),
			wantRows: 1, wantStatus: models.HistoryStatusFailed,
		},
		{
			name:     "cancellation: no row",
			err:      context.Canceled,
			wantRows: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newHistoryTestDB(t)
			repos := db.Repositories()
			inputs := rescrapePhaseInputs{JobID: "matrix-rescrape-job", HistoryRepo: repos.HistoryRepo}

			if tc.err == nil {
				auditRescrapeSuccess(inputs, "R-001", "/in/r.mp4")
			} else {
				auditRescrapeFailure(inputs, "R-001", "/in/r.mp4", tc.err)
			}

			records, err := repos.HistoryRepo.FindByBatchJobID(context.Background(), "matrix-rescrape-job")
			require.NoError(t, err)
			assert.Len(t, records, tc.wantRows)
			if tc.wantRows == 1 {
				assert.Equal(t, tc.wantStatus, records[0].Status)
				assert.Equal(t, models.HistoryOpScrape, records[0].Operation)
			}
		})
	}
}

func TestAuditMatrix_CreateError_LogAndContinue(t *testing.T) {
	repo := &failingHistoryRepo{err: errors.New("db down")}
	assert.NotPanics(t, func() {
		recordHistory(context.Background(), repo, models.History{
			MovieID:   "ERR-001",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
		})
	})
	assert.Equal(t, 1, repo.callCount)
}
