package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/scrape"
	timeoutPkg "github.com/javinizer/javinizer-go/internal/timeout"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// RescrapePhase handles single-file rescrape operations.
// Rescrape owns the full rescrape sequence (scrape + poster gen +
// commit + cleanup). ScrapeSingle and CompleteRescrape remain for backward compat.
type RescrapePhase interface {
	ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error)
	CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *resultstore.MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error)
	// Rescrape performs the full rescrape lifecycle: file lookup, scrape, poster generation,
	// result commit, and cleanup.
	Rescrape(ctx context.Context, inputs rescrapePhaseInputs, cmd RescrapeCmd) (*RescrapeResult, error)
}

type rescrapePhase struct{}

// NewRescrapePhase returns the default RescrapePhase implementation.
func NewRescrapePhase() RescrapePhase {
	return &rescrapePhase{}
}

func (p *rescrapePhase) ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	wf := inputs.WF

	if wf == nil {
		return nil, nil, fmt.Errorf("job %s: cannot scrape — workflow not configured", inputs.JobID.String())
	}

	// direct scrape call with panic recovery, replacing the
	// errgroup+callback+mutex pattern. Same recovery semantics as scrape phase.
	workerTimeout := inputs.Concurrency.WorkerTimeout
	taskCtx := ctx
	if workerTimeout > 0 {
		var taskCancel context.CancelFunc
		taskCtx, taskCancel = context.WithTimeout(ctx, workerTimeout)
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
		// Nest the overall scrape operation timeout (scrapers.request_timeout_seconds)
		// inside the worker_timeout task context. The sooner deadline wins.
		scrapeCtx := taskCtx
		if inputs.Concurrency.RequestTimeout > 0 {
			resolved := timeoutPkg.FromDuration(inputs.Concurrency.RequestTimeout, "config:scrapers.request_timeout_seconds")
			logging.Debugf("Rescrape: applying request timeout %s (nested within worker_timeout)", resolved)
			var scrapeCancel context.CancelFunc
			scrapeCtx, scrapeCancel = context.WithTimeout(taskCtx, resolved.Duration)
			defer scrapeCancel()
		}
		result, meta, err := wf.Scrape(scrapeCtx, cmd)
		if scrapeCtx.Err() != nil && err == nil {
			if result == nil {
				result = &scrape.ScrapeResult{Status: scrape.StatusFailed, Message: "scrape timed out"}
			} else {
				result.Status = scrape.StatusFailed
				result.Message = "scrape timed out"
			}
			err = scrapeCtx.Err()
		}
		return result, meta, err
	}()

	return result, meta, scrapeErr
}

func (p *rescrapePhase) CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *resultstore.MovieResult, capturedRevision uint64, movieID string, oldMovieID string) (*RescrapeResult, error) {
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

	rescrapeResult := &RescrapeResult{OrphanedMovieIDs: orphanedIDs, Status: models.RescrapeStatusSuccess}
	auditRescrapeSuccess(inputs, movieID, filePath)
	return rescrapeResult, nil
}

// singleScrapeWork was removed. ScrapeSingle now calls
// wf.Scrape directly with panic recovery, eliminating the callback pattern.

// rescrapeLifecycle holds the cleanup context for a rescrape operation,
// enabling automatic rollback on failure via withRescrapeStatus.
type rescrapeLifecycle struct {
	inputs rescrapePhaseInputs
	lookup *resultstore.FileLookupResult
}

// withRescrapeStatus executes fn within a rescrape status-transition wrapper.
// If fn returns an error, or the outcome is Gone/Conflict/Failed, poster
// cleanup is performed automatically (rollback). On success, orphaned poster
// paths are cleaned up instead.
func withRescrapeStatus(lc rescrapeLifecycle, fn func() (*RescrapeResult, *resultstore.MovieResult, error)) (*RescrapeResult, error) {
	outcome, movieResult, err := fn()
	cleanupMovie := func() *models.Movie {
		if movieResult != nil {
			return movieResult.Movie
		}
		return nil
	}
	if err != nil {
		CleanupMoviePosters(lc.inputs.Fs, lc.inputs.TempDir, lc.inputs.JobID, cleanupMovie())
		if lc.inputs.HistoryRepo != nil {
			movieID := lc.lookup.OldMovieID
			if movieResult != nil && movieResult.Movie != nil {
				movieID = movieResult.Movie.ID
			}
			auditRescrapeFailure(lc.inputs, movieID, lc.lookup.FilePath, err)
		}
		return nil, err
	}

	if outcome == nil {
		return nil, nil
	}
	switch outcome.Status {
	case models.RescrapeStatusGone, models.RescrapeStatusConflict, models.RescrapeStatusFailed:
		CleanupMoviePosters(lc.inputs.Fs, lc.inputs.TempDir, lc.inputs.JobID, cleanupMovie())
		if lc.inputs.HistoryRepo != nil {
			movieID := lc.lookup.OldMovieID
			if movieResult != nil && movieResult.Movie != nil {
				movieID = movieResult.Movie.ID
			}
			errMsg := outcome.Error
			if errMsg == "" {
				errMsg = fmt.Sprintf("rescrape %s", outcome.Status)
			}
			auditRescrapeFailure(lc.inputs, movieID, lc.lookup.FilePath, errors.New(errMsg))
		}
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
func replaceRescrapeResult(outcome *RescrapeResult, filePath string, movieResult *resultstore.MovieResult, prov *resultstore.ProvenanceData) {
	if prov != nil {
		outcome.Movie = movieResult.Movie
		outcome.FieldSources = prov.FieldSources
		outcome.ActressSources = prov.ActressSources
		outcome.ScraperResults = prov.ScraperResults
	} else {
		outcome.Movie = movieResult.Movie
	}
	outcome.FilePath = filePath
}

// Rescrape performs the full rescrape lifecycle: file lookup, scrape,
// poster generation, result commit, and cleanup.
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
	var lookup *resultstore.FileLookupResult
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
		lookup = &resultstore.FileLookupResult{
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

	var prov *resultstore.ProvenanceData
	var movieResult *resultstore.MovieResult

	// Serialize the rescrape's poster-asset replacement + result commit
	// against the manual-crop, poster-from-URL, whole-movie PATCH, and
	// field-override edit paths via the shared per-(jobID, movieID)
	// poster-source lock. GeneratePoster rewrites the job's cached
	// {movieID}-full.jpg for the rescraped movie; without this lock an
	// overlapping manual crop can measure the newly scraped image while the
	// job state still references the old source, and the crop's state write
	// bumps the result revision, making the commit below lose its CAS —
	// leaving new-image bounds attached to the old URL. Holding the lock from
	// here through the commit also makes the revision and old-ID re-capture
	// atomic with the commit: a crop can no longer interleave between capture
	// and commit, so the CAS conflict now only surfaces for a lock-agnostic
	// state write, where a clean Conflict status is the correct outcome.
	// Keyed on the PRE-rescrape movie ID — the same key the crop/PATCH/
	// override paths use for the file being rescraped; when the rescrape
	// resolves a new ID, its freshly generated assets live under the new key —
	// and when ANOTHER result already uses that ID, the rescrape additionally
	// acquires the DESTINATION key once the scrape returns (see the
	// destination lock block inside the closure) so a crop or source edit on
	// the existing destination result cannot interleave with the asset
	// replacement. Lock ordering: the result-store locks inside
	// GetCurrentMovieID/GetRevision/CommitResult are taken while the poster
	// lock(s) are held, and no path acquires a poster-source lock while
	// holding one of those (the same cycle-free order overrideMu →
	// poster-source lock(s) → result-store locks that the other paths use);
	// when this path holds TWO poster locks they are taken in lexical key
	// order so opposite-direction rescrapes cannot deadlock.
	posterLockID := lookup.OldMovieID
	if posterLockID == "" && inputs.ResultMap != nil {
		posterLockID = inputs.ResultMap.GetCurrentMovieID(lookup.FilePath)
	}
	releasePosterLock := AcquirePosterSourceLock(inputs.JobID.String(), posterLockID)
	// Closure form: the destination-lock handoff inside the scrape closure
	// may release and RE-ACQUIRE the origin lock mid-flight (reassigning
	// releasePosterLock) — a deferred call of the original value would
	// double-release the first acquisition and leak the second.
	defer func() { releasePosterLock() }()
	// releaseDestPosterLock, when set, holds the DESTINATION movie ID's
	// poster-source lock for a rekeying (A→B) rescrape; see the destination
	// lock block inside the scrape closure. Held through withRescrapeStatus
	// so the failure/success poster cleanup is covered too.
	var releaseDestPosterLock func()
	defer func() {
		if releaseDestPosterLock != nil {
			releaseDestPosterLock()
		}
	}()
	if inputs.Finder != nil {
		lookup.CapturedRevision = inputs.Finder.GetRevision(lookup.FilePath)
	}
	if inputs.ResultMap != nil {
		lookup.OldMovieID = inputs.ResultMap.GetCurrentMovieID(lookup.FilePath)
	}

	lc := rescrapeLifecycle{inputs: inputs, lookup: lookup}

	var posterCacheRollback func() error
	outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
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

		// Construct the post-rescrape MovieResult. The authoritative FileMatchInfo
		// is the tracker's stored entry (the scanner output), which
		// CompleteRescrape.CommitResult restores onto this result.
		// Build a fallback here that carries Name + Extension so a tracker map-miss
		// (nil map or path-normalization mismatch) doesn't leak a MovieResult
		// with empty Extension — which would make the organize preview render the
		// video row without `.mp4`. Mirrors scrape_phase.go's backfill.
		fallbackFMI := models.FileMatchInfo{
			Path:      lookup.FilePath,
			Name:      filepath.Base(lookup.FilePath),
			Extension: filepath.Ext(lookup.FilePath),
		}
		movieResult, prov = scrapeResultToMovieResult(fallbackFMI, scrapeResult, meta, false)

		// Honor cancellation before any poster generation/commit work: ScrapeSingle
		// checks ctx, but once it returns this path would otherwise still generate
		// posters and CommitResult even if cancellation fired mid-scrape.
		if err := ctx.Err(); err != nil {
			return nil, movieResult, err
		}

		// The destination movie ID is known once the scrape completed (the
		// merge below preserves the scraped movie's ID).
		newMovieID := movieResult.FileMatchInfo.MovieID
		if movieResult.Movie != nil && movieResult.Movie.ID != "" {
			newMovieID = movieResult.Movie.ID
		}

		// A rekeying rescrape (A→B) where ANOTHER result already uses B
		// replaces B's shared assets too: GeneratePoster below rewrites the
		// job's cached {B}-full.jpg/preview, which a simultaneous crop or
		// source edit on the existing B result mutates while holding only B's
		// poster-source lock. Holding just the origin (A) lock lets the two
		// interleave — the crop records bounds measured against the rescraped
		// image while B's stored URL still names the previous source. So once
		// the destination ID is known, acquire B's lock as well and hold it
		// across the asset replacement and the commit (it releases with the
		// outer deferred cleanup, covering withRescrapeStatus's rollback too).
		//
		// Deadlock safety: this is the ONLY path that may hold two
		// poster-source locks at once — every other path holds at most one
		// (the crop and override re-resolve loops release before
		// re-acquiring) — so taking the pair in a stable order makes a lock
		// cycle impossible. Keys are acquired in lexical movie-ID order (the
		// shared jobID prefix on the composite key cancels): when B sorts
		// after the held origin key, B is acquired directly on top of A; when
		// B sorts BEFORE A, A is released first, then B and A are acquired in
		// order, and the origin-side under-lock state (revision, OldMovieID)
		// is re-captured again because an A-side edit could have landed in
		// the gap. Two opposite-direction rescrapes (A→B while B→A) therefore
		// cannot deadlock: whichever acquired its origin first is also the
		// one allowed to take the other key first.
		if newMovieID != "" && newMovieID != posterLockID {
			jobID := inputs.JobID.String()
			if posterLockID < newMovieID {
				releaseDestPosterLock = AcquirePosterSourceLock(jobID, newMovieID)
			} else {
				releasePosterLock()
				releaseDestPosterLock = AcquirePosterSourceLock(jobID, newMovieID)
				releasePosterLock = AcquirePosterSourceLock(jobID, posterLockID)
				if inputs.Finder != nil {
					lookup.CapturedRevision = inputs.Finder.GetRevision(lookup.FilePath)
				}
				if inputs.ResultMap != nil {
					lookup.OldMovieID = inputs.ResultMap.GetCurrentMovieID(lookup.FilePath)
				}
			}
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
		// existing result is present. MergeEnabled gates whether
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

		// Poster generation runs AFTER reconciliation, on the FINAL merged
		// movie — never on the raw scrape. A merge-enabled rescrape can retain
		// the existing effective poster source (e.g. PosterURL P kept because
		// the scraper returned only a new CoverURL C): generating from the raw
		// scraped movie would populate the shared {movieID}-full.jpg/preview
		// with C while the committed movie still references P, so a subsequent
		// manual crop is measured against C yet persisted alongside P and
		// Organize applies those coordinates to the wrong image. Generating
		// from the reconciled movie keeps the cache image == the effective
		// source the committed movie references.
		//
		// Snapshot the cached assets BEFORE the replacement so a post-commit
		// envelope-persist failure can restore them (RescrapeResult.
		// PosterCacheRollback — parity with RefreshPosterAssets'
		// snapshot/rollback): a restart would otherwise resurrect the
		// pre-rescrape job state against the freshly generated image. Taken
		// while the poster-source lock(s) are still held, so no crop/edit can
		// interleave between the snapshot and the replacement. A snapshot
		// failure degrades to no-rollback (logged) rather than rejecting the
		// rescrape: poster generation itself is already best-effort below.
		if inputs.PosterGen != nil && movieResult.Movie != nil {
			if snapshooter, ok := inputs.PosterGen.(posterAssetSnapshooter); ok {
				snap, snapErr := snapshooter.SnapshotPosterAssets(inputs.JobID.String(), movieResult.Movie.ID)
				if snapErr != nil {
					logging.Warnf("[rescrape] Failed to snapshot poster assets for %s before generation (no persist-failure rollback): %v", movieResult.Movie.ID, snapErr)
				} else {
					posterCacheRollback = func() error { return snapshooter.RestorePosterAssets(snap) }
				}
			}
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
		// The persist-failure rollback travels to the orchestrator only on
		// success — failure/gone/conflict outcomes already unwound the
		// generated assets via withRescrapeStatus's cleanup, so restoring the
		// snapshot there would resurrect assets the cleanup deliberately
		// removed.
		outcome.PosterCacheRollback = posterCacheRollback
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

	// Crop state (CropBounds + ShouldCropPoster) is measured against the
	// effective poster source (PosterURL ?? CoverURL — the same semantics as
	// field_override.go's effectivePosterSource). The scraped movie never
	// carries CropBounds, so the merge engine leaves merged nil; that is
	// correct only when reconciliation actually switched the source image.
	// When the merge keeps the existing effective source (the scraper
	// returned no image, or returned the same URL), an existing manual crop
	// was measured against that very image and stays valid: dropping it would
	// make Organize save the retained cover/poster without the user-approved
	// crop (the crop's ShouldCropPoster=false reset makes no crop intent
	// re-derive), and overwriting ShouldCropPoster would discard a deliberate
	// user crop decision. Invariant: unchanged effective source ⟹ the
	// currently stored CropBounds and ShouldCropPoster are kept; a changed
	// source invalidates crop state measured against the old image.
	// This must run AFTER the URL reconciliation above — it compares the
	// final merged source, and must override the takeScraperImages crop-intent
	// copy when the scraper merely re-found the identical source.
	if existing != nil {
		oldSource := effectivePosterSource(existing.Poster.PosterURL, existing.Poster.CoverURL)
		newSource := effectivePosterSource(merged.Merged.Poster.PosterURL, merged.Merged.Poster.CoverURL)
		if newSource == oldSource {
			if existing.Poster.CropBounds != nil {
				b := *existing.Poster.CropBounds // copy: merged must not alias existing
				merged.Merged.Poster.CropBounds = &b
			} else {
				merged.Merged.Poster.CropBounds = nil
			}
			merged.Merged.Poster.ShouldCropPoster = existing.Poster.ShouldCropPoster
		} else {
			merged.Merged.Poster.CropBounds = nil // crop was measured against the old image
		}
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
