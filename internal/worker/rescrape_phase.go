package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/spf13/afero"
)

// RescrapePhase handles single-file rescrape operations.
// Rescrape owns the full rescrape sequence (scrape + poster gen +
// commit + cleanup). ScrapeSingle and CompleteRescrape remain for backward compat.
type RescrapePhase interface {
	ScrapeSingle(ctx context.Context, inputs rescrapePhaseInputs, filePath string, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error)
	CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *resultstore.MovieResult, capturedRevision uint64, movieID string, oldMovieID string, prov *resultstore.ProvenanceData) (*RescrapeResult, error)
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

// provenanceLockedCommitter is implemented by familyKeyedResultMap: the
// result commit and the provenance publish share ONE family-locked section
// (codex r36 P1).
type provenanceLockedCommitter interface {
	CommitResultWithProvenance(filePath string, result *resultstore.MovieResult, expectedRevision uint64, prov *resultstore.ProvenanceData) error
}

type provenanceSetter interface {
	SetProvenance(filePath string, prov *resultstore.ProvenanceData)
}

// commitResultWithProvenance commits the rescrape result and publishes its
// provenance. On the production wrapper both happen inside the same keyed
// section; a bare ResultMapAccessor (tests, legacy seams) falls back to an
// unlocked commit+publish, matching the historical non-keyed behavior.
func commitResultWithProvenance(rm resultstore.ResultMapAccessor, filePath string, result *resultstore.MovieResult, rev uint64, prov *resultstore.ProvenanceData) error {
	if prov != nil {
		if pc, ok := rm.(provenanceLockedCommitter); ok {
			return pc.CommitResultWithProvenance(filePath, result, rev, prov)
		}
	}
	if err := rm.CommitResult(filePath, result, rev); err != nil {
		return err
	}
	// Zero-value provenance carries no attribution — skip the write entirely
	// (matches the retired controller tail's "any source non-nil" gate).
	if prov != nil && (prov.FieldSources != nil || prov.ActressSources != nil || prov.ScraperResults != nil) {
		if ps, ok := rm.(provenanceSetter); ok {
			ps.SetProvenance(filePath, prov)
		}
	}
	return nil
}

func (p *rescrapePhase) CompleteRescrape(inputs rescrapePhaseInputs, filePath string, result *resultstore.MovieResult, capturedRevision uint64, movieID string, oldMovieID string, prov *resultstore.ProvenanceData) (*RescrapeResult, error) {
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
	// Result + provenance commit as one keyed leg where the seam supports it
	// (codex r36 P1) — see commitResultWithProvenance.
	if commitErr := commitResultWithProvenance(inputs.ResultMap, filePath, result, capturedRevision, prov); commitErr != nil {
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
	// audit F-R15-1: carry the COMMITTED revision — the commit's update phase
	// mutated result.Revision in the keyed section, so this echo reflects OUR
	// landing, never whatever a racer did next.
	rv := result.Revision
	rescrapeResult.Revision = &rv
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

// rescrapeGenScope carries generation facts OUT of the scrape closure so the
// closeout can tell "bytes this op created" from "pre-existing/winner bytes"
// (audit R1: cleanup may only delete the former).
type rescrapeGenScope struct {
	// preExistedPair records whether canonical poster legs for the resolved
	// movie ID existed BEFORE this rescrape generated bytes.
	preExistedPair bool
	// parked holds the pre-generation canonical bytes (audit F-R3-2a): the
	// closeout restores them on any non-success outcome so committed state
	// never loses its poster bytes to a losing rescrape.
	parked *rescrapePosterBackup
	// genSHA fingerprints what THIS rescrape wrote at the canonical names
	// (audit F-R5-1): the closeout rewinds a leg only while it still holds
	// our bytes.
	genSHA map[string]string
}

// anyResultUsesMovieID is the SELF-INCLUSIVE ownership probe (audit F-R3-1):
// a winner's concurrent commit references the same ID — excluding the losing
// file alone still lets its closeout delete committed bytes. Any reference
// (Movie.ID or matcher alias, case-folded) ⇒ the bytes are owned, never ours.
func anyResultUsesMovieID(acc resultstore.ResultMapAccessor, movieID string) bool {
	if acc == nil || movieID == "" {
		return false
	}
	for _, r := range acc.SnapshotData().Results {
		if r == nil {
			continue
		}
		if r.Movie != nil && strings.EqualFold(strings.TrimSpace(r.Movie.ID), movieID) {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(r.FileMatchInfo.MovieID), movieID) {
			return true
		}
	}
	return false
}

// rescrapeOwnsPosterLegs reports whether THIS rescrape provably created the
// canonical pair legs (generation succeeded, no pre-existing bytes, NO row
// currently references the ID). Cleanup deletes them only then — never the
// bytes of a concurrent winner or an untouched older state (audit R1/R3-1).
func rescrapeOwnsPosterLegs(inputs rescrapePhaseInputs, scope *rescrapeGenScope, mr *resultstore.MovieResult, movieID string) bool {
	if mr == nil || movieID == "" || !mr.PosterGenerated || mr.PosterError != nil || scope == nil || scope.preExistedPair {
		return false
	}
	if inputs.ResultMap != nil && anyResultUsesMovieID(inputs.ResultMap, movieID) {
		return false
	}
	return true
}

// closeoutRescrapePosterBytes runs the parked-restore AND the ownership
// decision + delete UNDER the poster's family key (audit F-R3-1 + F-R4-2):
// the winner's commit holds the same key, so restore/delete can never
// interleave with it.
func closeoutRescrapePosterBytes(inputs rescrapePhaseInputs, scope *rescrapeGenScope, mr *resultstore.MovieResult, mv *models.Movie) {
	if mv == nil || mv.ID == "" {
		return
	}
	release := func() {}
	if inputs.EditLockFn != nil {
		release = inputs.EditLockFn(mv.ID)
	}
	defer release()
	// audit F-R5-1: rewind a leg only while it provably still holds THIS op's
	// generated bytes — a concurrent winner's committed bytes never get
	// rewound. Legs with no fingerprint keep legacy restore (PosterGen off).
	verify := func(legPath string) (bool, bool) {
		sha, ok := scope.genSHA[filepath.Base(legPath)]
		data, rerr := afero.ReadFile(inputs.Fs, legPath)
		if rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				return true, false // nothing there — restoring ours can't rewind anyone
			}
			// codex P2: UNREADABLE canon must fail closed — rewinding without
			// proof would risk discarding a winner's committed bytes; the
			// parked copy stays for the reconciler (in-flight fence holds).
			logging.Warnf("rescrape restore verify %s: %v — skipping rewind (undecidable)", legPath, rerr)
			return false, true
		}
		if !ok {
			// audit F-R17-1: no fingerprint and canon EXISTS with unproven
			// content — never rewind: dispose the obsolete parked copy.
			return false, false
		}
		return shaContentHex(data) == sha, false
	}
	scope.parked.restore(verify)
	if rescrapeOwnsPosterLegs(inputs, scope, mr, mv.ID) {
		if len(scope.genSHA) > 0 {
			// audit F-R19-2: remove ONLY legs whose CURRENT bytes provably
			// match OUR generation output — whatever else sits at the name is
			// a sibling's, never ours to delete.
			pdir := filepath.Join(inputs.TempDir, "posters", inputs.JobID.String())
			for _, sfx := range []string{"-full.jpg", ".jpg"} {
				base := mv.ID + sfx
				want, ok := scope.genSHA[base]
				if !ok {
					continue
				}
				lp := filepath.Join(pdir, base)
				data, rdErr := afero.ReadFile(inputs.Fs, lp)
				if rdErr != nil {
					continue
				}
				if shaContentHex(data) != want {
					continue
				}
				if rmErr := inputs.Fs.Remove(lp); rmErr != nil && !os.IsNotExist(rmErr) {
					logging.Warnf("rescrape own-leg delete %s: %v", lp, rmErr)
				}
			}
		} else {
			CleanupMoviePosters(inputs.Fs, inputs.TempDir, inputs.JobID, mv)
		}
	}
}

// withRescrapeStatus executes fn within a rescrape status-transition wrapper.
// If fn returns an error, or the outcome is Gone/Conflict/Failed, poster
// cleanup is performed automatically (rollback). On success, orphaned poster
// paths are cleaned up instead.
func withRescrapeStatus(lc rescrapeLifecycle, fn func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error)) (*RescrapeResult, error) {
	scope := &rescrapeGenScope{}
	outcome, movieResult, err := fn(scope)
	cleanupMovie := func() *models.Movie {
		if movieResult != nil {
			return movieResult.Movie
		}
		return nil
	}
	if err != nil {
		// audit F1+R1: fence rejections skip cleanup entirely (witness owns the
		// bytes); other failures delete ONLY legs this op provably created.
		// audit F-R4-3: fence errors skip restore entirely (the witness owns
		// those bytes); other failures restore (content-verified where a
		// generation fingerprint exists) + decide under the family key.
		var cfe *EditAdmissionConflictError
		if !errors.As(err, &cfe) {
			closeoutRescrapePosterBytes(lc.inputs, scope, movieResult, cleanupMovie())
		}
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
	case models.RescrapeStatusGone, models.RescrapeStatusFailed:
		// audit F-R4-1: keep the legacy purge and drop parked bytes with it —
		// restoring them after purging would resurrect the pre-op pair.
		scope.parked.discard()
		CleanupMoviePosters(lc.inputs.Fs, lc.inputs.TempDir, lc.inputs.JobID, cleanupMovie())
	case models.RescrapeStatusConflict:
		// audit R1/F-R3-1/F-R4-2: on CAS conflict the canonical pair belongs to
		// whoever won (or to the pre-op state) — under the family key: restore
		// parked pre-op bytes + delete ONLY legs this rescrape provably created.
		closeoutRescrapePosterBytes(lc.inputs, scope, movieResult, cleanupMovie())
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

	// Success: clean up orphaned poster paths. audit F-R3-3: the orphan list
	// was computed under the commit key but deletion ran unlocked — a PATCH
	// can legally move bytes INTO a stale orphan ID between the two. Revalidate
	// under the family keys: an ID any row now references is never orphaned.
	newMovieID := ""
	if movieResult != nil && movieResult.Movie != nil {
		newMovieID = movieResult.Movie.ID
	}
	// audit F-R4-1 + F-R16-1: restore fired already for the failure statuses
	// above; success discards — EXCEPT when generation failed or never ran
	// and the pair pre-existed: those bytes are the committed state, so route
	// through the keyed content-verify restore instead of discarding them.
	if outcome.Status == models.RescrapeStatusSuccess {
		// codex cloud P1 (@parked-arbitration): persist THIS op's commit token
		// before any closeout — a same-family revision bump cannot tell WHICH
		// overlapping rescrape won, so startup arbitration needs an op-scoped,
		// content-addressed marker to distinguish winner from stranded loser.
		if scope.parked != nil && scope.parked.fs != nil && scope.parked.commitPath != "" &&
			movieResult != nil && movieResult.Movie != nil && movieResult.Movie.ID != "" && lc.inputs.TempDir != "" {
			if wErr := writeCommitToken(scope.parked.fs, scope.parked.commitPath, movieResult.Movie.ID, scope.genSHA); wErr != nil {
				logging.Warnf("rescrape commit token write failed (backup retained for startup arbitration): %v", wErr)
			}
		}
		lostGeneration := movieResult != nil && (movieResult.PosterError != nil || !movieResult.PosterGenerated)
		if lostGeneration && scope.preExistedPair {
			closeoutRescrapePosterBytes(lc.inputs, scope, movieResult, cleanupMovie())
		} else if outcome != nil && scope.parked != nil && scope.parked.fs != nil {
			// codex cloud P1: the DURABLE commit (envelope persist) happens in the
			// CALLER after this phase returns — pass the teardown out instead of
			// discarding here, so a crash/failed persist keeps the arbitration set.
			outcome.PosterRecovery = NewRescapeRecoveryHandle(scope.parked.discard)
		} else if scope.parked != nil {
			scope.parked.discard()
		}
	} else if outcome.Status != models.RescrapeStatusGone && outcome.Status != models.RescrapeStatusFailed && outcome.Status != models.RescrapeStatusConflict {
		scope.parked.restore(nil)
	}
	if len(outcome.OrphanedMovieIDs) > 0 {
		release := func() {}
		if lc.inputs.EditLockFn != nil {
			release = lc.inputs.EditLockFn(outcome.OrphanedMovieIDs...)
		}
		stillOrphaned := make([]string, 0, len(outcome.OrphanedMovieIDs))
		var pdir string
		if lc.inputs.Fs != nil && lc.inputs.TempDir != "" {
			pdir = filepath.Join(lc.inputs.TempDir, "posters", lc.inputs.JobID.String())
		}
		for _, id := range outcome.OrphanedMovieIDs {
			if anyResultUsesMovieID(lc.inputs.ResultMap, id) {
				continue
			}
			if pdir != "" {
				// audit F-R18-1: in-flight generation marker — a sibling op's
				// uncommitted bytes sit at this ID's canonical names; row-only
				// orphan probing structurally cannot see them. Skip it.
				if inFlight, perr := rescrapeInFlightBackupPresent(lc.inputs.Fs, pdir, id); perr != nil {
					logging.Warnf("orphan sweep marker probe for %s failed (%v) — kept (undecidable)", id, perr)
					continue
				} else if inFlight {
					continue
				}
			}
			stillOrphaned = append(stillOrphaned, id)
		}
		CleanupPosterPaths(lc.inputs.Fs, OrphanedPosterPaths(stillOrphaned, newMovieID, lc.inputs.TempDir, lc.inputs.JobID, lc.inputs.FsCaseCache))
		release()
	}
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

	lc := rescrapeLifecycle{inputs: inputs, lookup: lookup}

	outcome, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
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

		// audit F-R10-1: witness probes + park + generation run UNDER the
		// family key — the keyless window let a committed relocation/promote
		// land between our witness probe and our byte overwrite (steal), or
		// let our generation overwrite a freshly promoted pair (clobber).
		// The commit leg keys up separately afterwards, as before.
		release := func() {}
		genID := ""
		if movieResult.Movie != nil {
			genID = strings.TrimSpace(movieResult.Movie.ID)
		}
		if inputs.EditLockFn != nil && genID != "" {
			release = inputs.EditLockFn(genID)
		}
		heldErr := func() error {
			// audit F-R11-1: a panicking generation (untrusted HTTP bytes →
			// decode/crop) must never leak the family mutex — release on ALL
			// exits, including panic unwind.
			defer release()
			// audit F1: a witness outstanding means canonical poster bytes are
			// mid-recovery (witness-holder owns them until restart reconcile) —
			// generating over them would clobber recoverable state. Decline early;
			// the commit-leg probe (familyKeyedResultMap) is the under-key net.
			if movieResult.Movie != nil {
				seen := map[string]struct{}{}
				for _, pid := range []string{strings.TrimSpace(lookup.OldMovieID), strings.TrimSpace(movieResult.Movie.ID)} {
					if pid == "" {
						continue
					}
					if _, dup := seen[strings.ToLower(pid)]; dup {
						continue
					}
					seen[strings.ToLower(pid)] = struct{}{}
					if cerr := posterWitnessConflictCore(inputs.Fs, inputs.TempDir, inputs.JobID.String(), pid); cerr != nil {
						return cerr
					}
				}
			}

			// audit F-R3-1: record byte-ownership BEFORE generating — cleanup may
			// only delete what this op created, and transient stat errors read as
			// PRE-EXISTING (fail closed), never absent.
			if movieResult.Movie != nil && inputs.Fs != nil && inputs.TempDir != "" {
				pdir := filepath.Join(inputs.TempDir, "posters", inputs.JobID.String())
				fullPath := filepath.Join(pdir, movieResult.Movie.ID+"-full.jpg")
				cropPath := filepath.Join(pdir, movieResult.Movie.ID+".jpg")
				_, fe := inputs.Fs.Stat(fullPath)
				_, ce := inputs.Fs.Stat(cropPath)
				if fe == nil || ce == nil ||
					(fe != nil && !os.IsNotExist(fe)) || (ce != nil && !os.IsNotExist(ce)) {
					scope.preExistedPair = true
				}
				// audit F-R3-2a: park pre-existing canonical bytes aside so the
				// closeout can restore committed state if this rescrape loses.
				// codex cloud P1: bind the sentinel's provenance to a POST-scrape
				// baseline read under THIS key — lookup.CapturedRevision predates
				// the scrape; an edit landing mid-scrape would otherwise read as
				// "advanced" after a post-generation crash and misjudge the op as
				// committed. The key is held across park→generate, so this row
				// snapshot IS the parked bytes' era.
				prevRev := lookup.CapturedRevision
				if inputs.ResultMap != nil {
					if row, ok := inputs.ResultMap.SnapshotData().Results[lookup.FilePath]; ok && row != nil {
						prevRev = row.Revision
					}
				}
				scope.parked = parkCanonicalPosterPair(inputs.Fs, pdir, movieResult.Movie.ID, prevRev)
				if scope.parked.parkErr != nil {
					// codex P2: unrecoverable bytes — abort BEFORE generation.
					return fmt.Errorf("poster backup park: %w", scope.parked.parkErr)
				}
			}

			// Poster generation
			if inputs.PosterGen != nil && movieResult.Movie != nil {
				if posterErr := inputs.PosterGen.GeneratePoster(ctx, inputs.JobID.String(), movieResult.Movie); posterErr != nil {
					s := posterErr.Error()
					movieResult.PosterError = &s
				}
				movieResult.PosterGenerated = true
			}

			// audit F-R5-1: fingerprint whatever the generation wrote at canonical —
			// the closeout's restore rewinds only while those exact bytes sit there.
			if movieResult.PosterGenerated && movieResult.Movie != nil && inputs.Fs != nil && inputs.TempDir != "" {
				pdir2 := filepath.Join(inputs.TempDir, "posters", inputs.JobID.String())
				scope.genSHA = map[string]string{}
				for _, lp := range []string{filepath.Join(pdir2, movieResult.Movie.ID+"-full.jpg"), filepath.Join(pdir2, movieResult.Movie.ID+".jpg")} {
					if data, rerr := afero.ReadFile(inputs.Fs, lp); rerr == nil {
						scope.genSHA[filepath.Base(lp)] = shaContentHex(data)
						continue
					} else if !os.IsNotExist(rerr) {
						// codex cloud P2: a TRANSIENT fingerprint read failure must
						// never record as a silent "missing" — the closeout would
						// read the gap as fresh-committed ownership and discard the
						// parked copy. Abort, restoring our own generation while
						// THIS op still holds the family key.
						scope.parked.restore(nil)
						return fmt.Errorf("poster fingerprint capture %s: %w", lp, rerr)
					}
					// ENOENT: the leg was never generated — nothing to fingerprint.
				}
			}
			return nil
		}()
		if heldErr != nil {
			return nil, movieResult, heldErr
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

		// A rescrape refreshes the poster source: any manual crop geometry
		// measured against the previous source is stale. Clear it explicitly
		// (including same-URL refreshes) rather than relying on the merge
		// engine incidentally dropping the runtime-only field.
		clearPosterCropGeometry(movieResult.Movie)

		// Commit result (provenance rides the same keyed section via prov).
		outcome, commitErr := p.CompleteRescrape(inputs, lookup.FilePath, movieResult, lookup.CapturedRevision, newMovieID, lookup.OldMovieID, prov)
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
