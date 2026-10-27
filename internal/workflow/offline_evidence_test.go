package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

type evidenceScrapeOrchestrator struct {
	result *scrape.ScrapeResult
	err    error
}

func (s evidenceScrapeOrchestrator) Execute(context.Context, scrape.ScrapeCmd) (*scrape.ScrapeResult, *OrchestrationMeta, error) {
	return s.result, &OrchestrationMeta{}, s.err
}
func TestWorkflowOfflineEvidenceNeverChangesMovieOrScrapeOutcome(t *testing.T) {
	for _, mode := range []string{"default off", "success", "cached", "failed", "error", "nil"} {
		t.Run(mode, func(t *testing.T) {
			movie := &models.Movie{ID: "TEST-001", Title: "Original", Actresses: []models.Actress{{DMMID: 7, JapaneseName: "Original actor"}}}
			before := movie.Clone()
			result := &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: movie, OfflineEvidence: &scrapeevidence.Report{SnapshotDigest: "must not leak"}}
			var err error
			switch mode {
			case "cached":
				result.Cached = true
			case "failed":
				result.Status = scrape.StatusFailed
			case "error":
				err = errors.New("scrape failed")
			case "nil":
				result = nil
				err = errors.New("scrape failed")
			}
			ctx := t.Context()
			release := func() {}
			if mode != "default off" {
				ctx, release = (*scrapeevidence.Service)(nil).Begin(ctx, true)
			}
			defer release()
			workflow := &Workflow{scrape: evidenceScrapeOrchestrator{result: result, err: err}}
			got, _, gotErr := workflow.Scrape(ctx, scrape.ScrapeCmd{})
			require.Equal(t, err, gotErr)
			require.Same(t, result, got)
			require.Equal(t, before, movie)
			if got == nil {
				return
			}
			require.Same(t, movie, got.Movie)
			if mode == "success" || mode == "cached" {
				require.Equal(t, scrapeevidence.Unavailable, got.OfflineEvidence.Outcome)
			} else {
				require.Nil(t, got.OfflineEvidence)
			}
		})
	}
}
