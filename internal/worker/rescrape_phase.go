package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// RescrapePhase handles single-file rescrape operations.
// Per ADR-0041: Rescrape owns the full rescrape sequence (scrape + poster gen +
// commit + cleanup). ScrapeSingle and CompleteRescrape remain for backward compat.
type RescrapePhase interface {
	ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error)
	CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error)
	// Rescrape performs the full rescrape lifecycle: file lookup, scrape, poster generation,
	// result commit, and cleanup. Per ADR-0041 Decision 3.
	Rescrape(ctx context.Context, inputs rescrapePhaseInputs, cmd RescrapeCmd) (*RescrapeResult, error)
}

type rescrapePhase struct{}

func NewRescrapePhase() RescrapePhase {
	return &rescrapePhase{}
}

func (p *rescrapePhase) ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	wf := inputs.WF

	if wf == nil {
		return nil, nil, fmt.Errorf("job %s: cannot scrape — workflow not configured", inputs.JobID.String())
	}

	// Per ADR-0033: direct scrape call with panic recovery, replacing the
	// errgroup+callback+mutex pattern. Same recovery semantics as scrape phase.
	timeout := inputs.Concurrency.WorkerTimeout
	taskCtx := ctx
	if timeout > 0 {
		var taskCancel context.CancelFunc
		taskCtx, taskCancel = context.WithTimeout(ctx, timeout)
		defer taskCancel()
	}

	result, meta, scrapeErr := func() (r *scrape.ScrapeResult, m *workflow.OrchestrationMeta, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				panicErr := panicutil.FormatRecover(rec)
				logging.Errorf("ScrapeSingle %s %v", filePath, panicErr)
				err = panicErr
			}
		}()
		return wf.Scrape(taskCtx, cmd, nil)
	}()

	return result, meta, scrapeErr
}

func (p *rescrapePhase) CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
	if inputs.ResultMap.IsGone() {
		return &RescrapeResult{Status: models.RescrapeStatusGone}, nil
	}

	// Read current movie ID before the commit (via the accessor)
	currentMovieIDBeforeUpdate := inputs.ResultMap.GetCurrentMovieID(filePath)

	// Apply multipart metadata from models.FileMatchInfo
	if info, ok := inputs.ResultMap.GetFileMatchInfo(filePath); ok {
		result.FileMatchInfo = info
	}

	// Atomically commit the result (handles locking, revision increment, progress recalculation).
	// CommitResult performs an atomic revision check to guard against races.
	// Revision conflicts (TOCTOU race or stale capturedRevision) are handled via
	// models.RescrapeStatusConflict — no error is returned. Real system errors are propagated.
	if commitErr := inputs.ResultMap.CommitResult(filePath, result, capturedRevision); commitErr != nil {
		if strings.HasPrefix(commitErr.Error(), "conflict:") {
			return &RescrapeResult{Status: models.RescrapeStatusConflict}, nil
		}
		return nil, commitErr
	}

	// Detect orphaned movie IDs. A movie ID is orphaned when this file no
	// longer references it (the file now uses movieID) and no other file does
	// either. currentMovieIDBeforeUpdate (read from the result map) and
	// oldMovieID (passed by the caller) both describe prior IDs and may be
	// equal — when they are, both branches would append the SAME id, so
	// de-duplicate via orphanSeen before appending.
	var orphanedIDs []string
	orphanSeen := make(map[string]struct{})
	addOrphan := func(id string) {
		if id == "" {
			return
		}
		if _, ok := orphanSeen[id]; ok {
			return
		}
		orphanSeen[id] = struct{}{}
		if !inputs.ResultMap.OtherResultUsesMovieID(filePath, id) {
			orphanedIDs = append(orphanedIDs, id)
		}
	}

	if currentMovieIDBeforeUpdate != "" && currentMovieIDBeforeUpdate != movieID {
		addOrphan(currentMovieIDBeforeUpdate)
	}

	if movieID != "" && oldMovieID != "" && movieID != oldMovieID {
		if currentMovieIDBeforeUpdate == oldMovieID {
			addOrphan(oldMovieID)
		}
	}

	return &RescrapeResult{OrphanedMovieIDs: orphanedIDs, Status: models.RescrapeStatusSuccess}, nil
}

// singleScrapeWork was removed per ADR-0033. ScrapeSingle now calls
// wf.Scrape directly with panic recovery, eliminating the callback pattern.

// rescrapeLifecycle holds the cleanup context for a rescrape operation,
// enabling automatic rollback on failure via withRescrapeStatus.
type rescrapeLifecycle struct {
	inputs rescrapePhaseInputs
	lookup *FileLookupResult
}

// withRescrapeStatus executes fn within a rescrape status-transition wrapper.
// If fn returns an error, or the outcome is Gone/Conflict/Failed, poster
// cleanup is performed automatically (rollback). On success, orphaned poster
// paths are cleaned up instead.
func withRescrapeStatus(lc rescrapeLifecycle, fn func() (*RescrapeResult, *MovieResult, error)) (*RescrapeResult, error) {
	outcome, movieResult, err := fn()
	cleanupMovie := func() *models.Movie {
		if movieResult != nil {
			return movieResult.Movie
		}
		return nil
	}
	if err != nil {
		CleanupMoviePosters(lc.inputs.Fs, lc.inputs.TempDir, lc.inputs.JobID, cleanupMovie())
		return nil, err
	}

	switch outcome.Status {
	case models.RescrapeStatusGone, models.RescrapeStatusConflict, models.RescrapeStatusFailed:
		CleanupMoviePosters(lc.inputs.Fs, lc.inputs.TempDir, lc.inputs.JobID, cleanupMovie())
		return outcome, nil
	}

	// Success: clean up orphaned poster paths
	newMovieID := ""
	if movieResult != nil && movieResult.Movie != nil {
		newMovieID = movieResult.Movie.ID
	}
	CleanupPosterPaths(lc.inputs.Fs, OrphanedPosterPaths(outcome.OrphanedMovieIDs, newMovieID, lc.inputs.TempDir, lc.inputs.JobID, lc.inputs.FsCaseCache))
	return outcome, nil
}

// replaceRescrapeResult attaches provenance metadata and file path to the
// rescrape outcome. Separated from the status-transition logic so that
// withRescrapeStatus stays focused on cleanup/rollback.
func replaceRescrapeResult(outcome *RescrapeResult, filePath string, movieResult *MovieResult, prov *ProvenanceData) {
	if prov != nil {
		outcome.Movie = movieResult.Movie
		outcome.FieldSources = prov.FieldSources
		outcome.ActressSources = prov.ActressSources
	} else {
		outcome.Movie = movieResult.Movie
	}
	outcome.FilePath = filePath
}

// Rescrape performs the full rescrape lifecycle. Per ADR-0041 Decision 3:
// owns file lookup, scrape, poster generation, result commit, and cleanup.
func (p *rescrapePhase) Rescrape(ctx context.Context, inputs rescrapePhaseInputs, cmd RescrapeCmd) (*RescrapeResult, error) {
	var queryOverride string
	var rawInput string

	if cmd.ManualSearchInput != "" {
		rawInput = cmd.ManualSearchInput
		if strings.HasPrefix(strings.ToLower(cmd.ManualSearchInput), "http://") ||
			strings.HasPrefix(strings.ToLower(cmd.ManualSearchInput), "https://") {
			queryOverride = cmd.ManualSearchInput
		} else {
			queryOverride = strings.TrimSpace(cmd.ManualSearchInput)
		}
	} else {
		queryOverride = cmd.MovieID
	}

	var selectedScrapers []string
	if len(cmd.SelectedScrapers) > 0 {
		selectedScrapers = cmd.SelectedScrapers
	}

	scrapeCmd := scrape.ScrapeCmd{
		MovieID:          queryOverride,
		RawInput:         rawInput,
		ForceRefresh:     cmd.Force,
		SelectedScrapers: selectedScrapers,
	}

	// File lookup
	var lookup *FileLookupResult
	if cmd.FilePath != "" {
		var capturedRevision uint64
		var oldMovieID string
		if inputs.ResultMap != nil {
			capturedRevision = inputs.Finder.GetRevision(cmd.FilePath)
			currentMovieID := inputs.ResultMap.GetCurrentMovieID(cmd.FilePath)
			if currentMovieID != "" {
				oldMovieID = currentMovieID
			}
		}
		lookup = &FileLookupResult{
			FilePath:         cmd.FilePath,
			OldMovieID:       oldMovieID,
			CapturedRevision: capturedRevision,
		}
	} else {
		var err error
		lookup, err = inputs.Finder.FindFileForMovieID(cmd.MovieID)
		if err != nil {
			return nil, err
		}
	}

	var prov *ProvenanceData
	var movieResult *MovieResult

	lc := rescrapeLifecycle{inputs: inputs, lookup: lookup}

	outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *MovieResult, error) {
		// Scrape
		scrapeResult, meta, scrapeErr := p.ScrapeSingle(ctx, inputs, lookup.FilePath, scrapeCmd)
		if scrapeErr != nil {
			return nil, nil, scrapeErr
		}
		if scrapeResult == nil {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "scrape produced no result"}, nil, nil
		}
		if scrapeResult.Status == scrape.StatusFailed {
			// The scrape package populates scrapeResult.Message with a verbose,
			// per-scraper failure summary via buildNoResultsError (e.g.
			// "No results from any scraper: fc2: movie PPV-2856053 not found on FC2").
			// Surface it verbatim so callers see why the rescrape failed;
			// fall back to the generic label only when the scrape returned
			// no payload. Mirrors the fix applied to ScrapePhase's no-result
			// branch (commit 42d89e65).
			errMsg := fmt.Sprintf("scrape failed for %s", queryOverride)
			if strings.TrimSpace(scrapeResult.Message) != "" {
				errMsg = scrapeResult.Message
			}
			return &RescrapeResult{
				Status: models.RescrapeStatusFailed,
				Error:  errMsg,
			}, nil, nil
		}

		// Construct the post-rescrape MovieResult. Per ADR-0041 Decision 3, the
		// authoritative FileMatchInfo is the tracker's stored entry (the scanner
		// output), which CompleteRescrape.CommitResult restores onto this result.
		// Build a fallback here that carries Name + Extension so a tracker map-miss
		// (nil map or path-normalization mismatch) doesn't leak a MovieResult
		// with empty Extension — which would make the organize preview render the
		// video row without `.mp4`. Mirrors scrape_phase.go's backfill.
		fallbackFMI := models.FileMatchInfo{
			Path:      lookup.FilePath,
			Name:      filepath.Base(lookup.FilePath),
			Extension: filepath.Ext(lookup.FilePath),
		}
		movieResult, prov = scrapeResultToMovieResult(fallbackFMI, scrapeResult, meta)

		// Honor cancellation before any poster generation/commit work: ScrapeSingle
		// checks ctx, but once it returns this path would otherwise still generate
		// posters and CommitResult even if cancellation fired mid-scrape.
		if err := ctx.Err(); err != nil {
			return nil, movieResult, err
		}

		// Poster generation
		if inputs.PosterGen != nil && movieResult.Movie != nil {
			if posterErr := inputs.PosterGen.GeneratePoster(ctx, inputs.JobID.String(), movieResult.Movie); posterErr != nil {
				s := posterErr.Error()
				movieResult.PosterError = &s
			}
			movieResult.PosterGenerated = true
		}

		// Re-check after poster generation before committing.
		if err := ctx.Err(); err != nil {
			return nil, movieResult, err
		}

		newMovieID := movieResult.FileMatchInfo.MovieID
		if movieResult.Movie != nil && movieResult.Movie.ID != "" {
			newMovieID = movieResult.Movie.ID
		}

		// Honor the caller's merge policy (preset/scalar_strategy/array_strategy).
		// When MergeEnabled is set and an existing result is present, merge the
		// freshly scraped Movie into the existing one via the same NFO merge
		// engine the apply path uses, instead of wholesale-replacing it. This
		// closes the gap where the API accepted + validated merge options but
		// RescrapeCmd silently dropped them. When MergeEnabled is false (the
		// default for callers that supply no merge options), behavior is
		// unchanged: the scraped Movie replaces the existing one on commit.
		// Merge the scraped Movie into the existing one when requested AND an
		// existing result is present. Per ADR-0030: MergeEnabled gates whether
		// merging is applied at all; when false (the default for callers that
		// supply no merge options), behavior is unchanged: the scraped Movie
		// replaces the existing one on commit. The image-URL reconciliation and
		// scraped-baseline establishment happen in mergeRescrapeMovie (merge path
		// with existing) or in the unified establishScrapedBaseline call below
		// (non-merge, or merge-enabled with no prior result).
		baselineFromScraped := true
		if cmd.MergeEnabled && movieResult.Movie != nil && inputs.ResultMap != nil {
			if existing, getErr := inputs.ResultMap.GetMovieResult(lookup.FilePath); getErr == nil && existing != nil && existing.Movie != nil {
				movieResult.Movie = mergeRescrapeMovie(existing.Movie, movieResult.Movie, cmd.Merge, lookup.FilePath)
				baselineFromScraped = false // mergeRescrapeMovie already established it
			}
		}
		if baselineFromScraped && movieResult.Movie != nil {
			// Non-merge (wholesale-replace) path, or merge-enabled with no prior
			// result: the scraped movie carries no Original* (scrapers don't
			// populate them), so establish the revert baseline from its own poster
			// fields. Without this, Reset would have no target until the first
			// manual edit snapshotted it lazily.
			establishScrapedBaseline(movieResult.Movie, movieResult.Movie)
		}

		// Commit result
		outcome, commitErr := p.CompleteRescrape(inputs, lookup.FilePath, movieResult, lookup.CapturedRevision, newMovieID, lookup.OldMovieID)
		if commitErr != nil {
			return nil, movieResult, commitErr
		}

		return outcome, movieResult, nil
	})

	if err != nil {
		return nil, err
	}

	// Attach provenance and file path on success
	if outcome.Status == models.RescrapeStatusSuccess {
		replaceRescrapeResult(outcome, lookup.FilePath, movieResult, prov)
	}

	return outcome, nil
}

// mergeRescrapeMovie merges a freshly scraped Movie into the existing one for
// a rescrape, using the same NFO merge engine as the apply path. The scraped
// movie's ID is preserved (a rescrape may resolve a new/corrected ID); all
// other fields are merged per the resolved scalar/array strategy, with the
// existing movie treated as the "nfo"/preserved side. On merge failure the
// scraped movie is returned unchanged (wholesale-replace fallback) so a bad
// merge never blocks the rescrape; the failure is logged.
func mergeRescrapeMovie(existing, scraped *models.Movie, opts workflow.MergeOptions, filePath string) *models.Movie {
	merged, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, opts.ScalarStrategy, opts.ArrayStrategy)
	if err != nil {
		logging.Errorf("rescrape merge failed for %s, falling back to replace: %v", filePath, err)
		// Establish the scraped baseline on the wholesale-replace fallback so
		// the caller's baselineFromScraped=false expectation still holds; without
		// this the returned movie would carry no Original* and Reset would have
		// no target until the first manual edit snapshotted it lazily.
		establishScrapedBaseline(scraped, scraped)
		return scraped
	}
	if merged == nil || merged.Merged == nil {
		establishScrapedBaseline(scraped, scraped)
		return scraped
	}
	merged.Merged.ID = scraped.ID

	// Image URLs (PosterURL/CoverURL) are content-bound scraped assets, not
	// curated metadata. The generic merge treats them as ordinary string
	// fields, so the default prefer-nfo rescrape preserves the existing
	// movie's images — defeating the rescrape's purpose (a refresh rescrape
	// kept a stale/broken poster) and, when the rescrape resolves a different
	// content-id, leaving images that belong to a different movie on the
	// resolved content. CroppedPosterURL is already special-cased to always
	// use the scraped value in the merge engine; PosterURL/CoverURL are
	// reconciled here, locally to the rescrape path (the shared merge engine
	// is intentionally untouched so the organize/apply path can still
	// preserve a user's on-disk NFO images).
	//
	// Rule:
	//   - content-id change: take the scraper's images, clearing when the
	//     scraper has none (the existing images are for different content).
	//   - same content: take the scraper's images when it provides them
	//     (a rescrape should refresh), otherwise keep the merged value so a
	//     scraper that found no image doesn't wipe a valid existing one.
	takeScraperImages := false
	if existing != nil && scraped.ID != "" && scraped.ID != existing.ID {
		merged.Merged.Poster.CoverURL = strings.TrimSpace(scraped.Poster.CoverURL)
		merged.Merged.Poster.PosterURL = strings.TrimSpace(scraped.Poster.PosterURL)
		takeScraperImages = true
	} else {
		if strings.TrimSpace(scraped.Poster.CoverURL) != "" {
			merged.Merged.Poster.CoverURL = strings.TrimSpace(scraped.Poster.CoverURL)
			takeScraperImages = true
		}
		if strings.TrimSpace(scraped.Poster.PosterURL) != "" {
			merged.Merged.Poster.PosterURL = strings.TrimSpace(scraped.Poster.PosterURL)
			takeScraperImages = true
		}
	}
	// When the scraper's poster is authoritative (content-id change, or it
	// provided a fresh image), carry its crop state too — otherwise the merged
	// movie would keep the existing (possibly different) ShouldCropPoster and
	// Reset would not reflect the rescrape's crop intent.
	if takeScraperImages {
		merged.Merged.Poster.ShouldCropPoster = scraped.Poster.ShouldCropPoster
	}

	// The poster-original group (OriginalPosterURL/OriginalCroppedPosterURL/
	// OriginalShouldCropPoster/OriginalCoverURL) is the revert baseline the
	// review UI restores on Reset — it must track the scraper's value, not be
	// preserved across content changes. The generic prefer-nfo merge would
	// carry the existing (possibly previous-content) Original* forward, so a
	// rescrape that resolved a different content-id would leave the revert
	// target pointing at the old content's images. Re-establish the baseline
	// from the freshly scraped movie so Reset always returns to what this
	// rescrape produced. (The frontend already falls back to the current field
	// when Original* is empty, so an empty scraper value is the correct
	// baseline when the scraper found no image.)
	establishScrapedBaseline(merged.Merged, scraped)
	return merged.Merged
}
