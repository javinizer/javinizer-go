package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/fscase"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// PosterEditor handles poster-related mutations on job results.
// Extracted from BatchJob to isolate the poster update concern —
// BatchJob no longer directly implements poster editing logic.
//
// POSTER-WRITE-HARDENING D1: every family-mutating operation serializes on a
// per-(job,movie) keyed lock acquired through WithMovieEditLock. Public
// methods (and the JobEditor adapter) are thin wrappers; the LockedMovieOps
// callback exposes ONLY the lock-free cores, so a handler-style callback
// composing two cores completes under one acquisition (no nesting traps,
// no double serialization).
//
// POSTER-WRITE-HARDENING D4: when the store wires a composite transaction
// seam (attachEnv with a non-nil committer), DB legs (movie upsert, actress
// renames, candidate-merged envelope) commit atomically through
// EditCommitter BEFORE any in-memory publication — any leg failing rolls all
// back and state stays untouched. Without the seam (CLI/test stores), the
// editor publishes first and flushes a best-effort envelope via persistFn.
type PosterEditor struct {
	lookup  resultstore.ResultReadFacade
	updater resultstore.ResultUpdater

	mu        sync.RWMutex
	movieRepo database.MovieRepositoryInterface // legacy best-effort DB write (fallback only)
	env       *posterEditEnv
	locks     *keyedMutexRegistry
}

// posterEditEnv bundles the JobStore-provided edit environment.
type posterEditEnv struct {
	committer   *EditCommitter // nil ⇒ legacy publish-then-persist path
	envelope    func(overrides map[string]*resultstore.MovieResult, provOverrides map[string]*resultstore.ProvenanceData, excluded map[string]bool) (*models.Job, error)
	persistFn   func() error
	lifecycle   *JobLifecycle
	actressRepo database.ActressRepositoryInterface // legacy rename leg (fallback path)
	// Selected per-job at attach time for poster-file rekey moves (codex r21):
	// a family rekey rename must move the on-disk poster pair alongside the
	// candidate publication.
	fs      afero.Fs
	tempDir string
	jobID   string
	// ciProbe reports whether dir sits on a case-insensitive fs; nil ⇒
	// default fscase probe (codex r43 P2 test seam).
	ciProbe func(dir string) bool
}

// NewPosterEditor creates a PosterEditor backed by a ResultReadFacade (for
// lookups) and a ResultUpdater (for atomic mutations). If movieRepo is
// non-nil, UpdatePosterFromURL will also persist the poster change to the
// database (best-effort: DB failures are logged, not returned).
func NewPosterEditor(lookup resultstore.ResultReadFacade, updater resultstore.ResultUpdater, movieRepo database.MovieRepositoryInterface) *PosterEditor {
	return &PosterEditor{lookup: lookup, updater: updater, movieRepo: movieRepo, locks: newKeyedMutexRegistry()}
}

// attachEnv wires the store-provided edit environment (composite tx seam,
// candidate envelope builder, persist fallback, lifecycle). Idempotent.
func (pe *PosterEditor) attachEnv(env *posterEditEnv) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.env = env
}

// setMovieRepo swaps the legacy movie repo — used by dep rehydration paths
// that previously rebuilt the whole editor (which would have orphaned the
// keyed-lock registry and attached env).
func (pe *PosterEditor) setMovieRepo(repo database.MovieRepositoryInterface) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.movieRepo = repo
}

// setLockRegistry shares the store-wide keyed registry across ALL jobs
// (POSTER-WRITE-HARDENING D15: movie/actress rows are process-shared, so two
// concurrent jobs editing the same movie must contend on ONE registry, not a
// per-job one). Called from JobStore.attachEditDeps for every job instance.
func (pe *PosterEditor) setLockRegistry(reg *keyedMutexRegistry) {
	if reg == nil {
		return
	}
	pe.mu.Lock()
	pe.locks = reg
	pe.mu.Unlock()
}

func (pe *PosterEditor) currentEnv() *posterEditEnv {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.env
}

func (pe *PosterEditor) currentMovieRepo() database.MovieRepositoryInterface {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.movieRepo
}

// WithMovieEditLock acquires the family lock for movieID and invokes fn with
// the cores view. The callback receives ONLY LockedMovieOps (lock-free cores)
// — well-formed callers cannot nest acquisitions (D1).
func (pe *PosterEditor) lockRegistry() *keyedMutexRegistry {
	pe.mu.RLock()
	reg := pe.locks
	pe.mu.RUnlock()
	if reg == nil {
		pe.mu.Lock()
		if pe.locks == nil {
			pe.locks = newKeyedMutexRegistry()
		}
		reg = pe.locks
		pe.mu.Unlock()
	}
	return reg
}

// WithMovieEditLock acquires the FULL identity set for this op (matcher
// alias + canonical Movie.ID when they disagree + the stored content-id) and
// invokes fn with the cores view. Every family operation — edit, poster
// from-URL, override, rescrape commit — contends on this set so aliases on
// BOTH identities interlock (codex r34).
func (pe *PosterEditor) WithMovieEditLock(movieID string, fn func(m *LockedMovieOps) error) error {
	keys := pe.identityKeysFor(movieID)
	release := pe.lockRegistry().AcquireMany(keys)
	defer release()
	return fn(&LockedMovieOps{pe: pe, movieID: movieID})
}

// identityKeysFor returns every process-shared key this family may be
// contacted by: the caller-supplied matcher-alias, the canonical Movie.ID,
// and its stored content-id (PK). Deduplication lives in AcquireMany.
func (pe *PosterEditor) identityKeysFor(movieID string) []string {
	keys := []string{movieID}
	if r, err := pe.lookup.FindMovieResultForMovieID(movieID); err == nil && r != nil && r.Movie != nil {
		if id := strings.TrimSpace(r.Movie.ID); id != "" && !strings.EqualFold(id, strings.TrimSpace(movieID)) {
			keys = append(keys, id)
		}
		if cid := strings.TrimSpace(r.Movie.ContentID); cid != "" {
			keys = append(keys, "cid:"+cid)
		}
	}
	return keys
}

// LockedMovieOps exposes the lock-free mutation cores for the movie family
// guarded by WithMovieEditLock. Cores re-resolve the family fresh under the
// held key so any pre-lock resolution (resultID→movieID) is revalidated — a
// stale pre-lock read can never skip a concurrent rescrape rekey or crop.
type LockedMovieOps struct {
	pe      *PosterEditor
	movieID string
}

// MovieID returns the family key these ops are locked on.
func (m *LockedMovieOps) MovieID() string { return m.movieID }

// familyFilePaths re-resolves the family under the held edit lock.
// Combines POSTER-WRITE-HARDENING typed empty-family error (404-able).
func (m *LockedMovieOps) familyFilePaths() ([]string, error) {
	filePaths := m.pe.lookup.FindFilePathsForMovieID(m.movieID)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, m.movieID)
	}
	return filePaths, nil
}

// mutateCandidates applies mutator to a fresh clone of each family file's
// current result. Files without a Movie are skipped (legacy behavior).
// Returns the candidate map keyed by file path (pre-publication).
func (m *LockedMovieOps) mutateCandidates(filePaths []string, mutator func(movie *models.Movie)) map[string]*resultstore.MovieResult {
	candidates := make(map[string]*resultstore.MovieResult, len(filePaths))
	for _, fp := range filePaths {
		current, err := m.pe.lookup.GetMovieResult(fp)
		if err != nil || current == nil || current.Movie == nil {
			continue
		}
		cand := current.Clone()
		mutator(cand.Movie)
		// codex P3-A: clones keep the stored matcher alias; ONLY the whole-movie
		// save stamps the new Movie.ID onto FileMatchInfo (explicit rekey site).
		// Predict the post-publication revision (codex r15): the committed
		// envelope snapshot and the in-memory AtomicUpdateFileResult bump
		// must expose the SAME revision — otherwise a restart reads one less
		// than live, and revision-based conflict detection regresses.
		cand.Revision = current.Revision + 1
		candidates[fp] = cand
	}
	return candidates
}

// publishCandidates commits the prepared candidates to in-memory state.
// Reused as an EditCommitPlan.Publish (post-commit) or as the direct
// publication in the legacy no-committer path. AtomicUpdateFileResult bumps
// revisions to match legacy per-op revision behavior. A rekeyed candidate
// also rewrites the live FileMatchInfo map (not only the per-result field)
// so later lookups never resolve the stale family (codex r15).
func (m *LockedMovieOps) publishCandidates(candidates map[string]*resultstore.MovieResult) error {
	type matchWriter interface {
		SetFileMatchInfo(string, models.FileMatchInfo)
	}
	st, _ := m.pe.updater.(matchWriter)
	for fp, cand := range candidates {
		if err := m.pe.updater.AtomicUpdateFileResult(fp, func(_ *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			// Revision was already predicted on the candidate (envelope
			// parity); AtomicUpdateFileResult reassigns rev+1 itself — same
			// value, no drift.
			return cand.Clone(), nil
		}); err != nil {
			return fmt.Errorf("publish %s: %w", fp, err)
		}
		if st != nil && cand.FileMatchInfo.MovieID != "" {
			if cur, ok := m.pe.lookup.GetFileMatchInfo(fp); ok && cur.MovieID != cand.FileMatchInfo.MovieID {
				fm := cur
				fm.MovieID = cand.FileMatchInfo.MovieID
				st.SetFileMatchInfo(fp, fm)
			}
		}
	}
	return nil
}

// commitCandidate routes DB legs + envelope + publish through the composite
// transaction when the env is attached; otherwise it publishes and flushes a
// best-effort envelope through the legacy persistFn.
func (m *LockedMovieOps) commitCandidate(ctx context.Context, candidates map[string]*resultstore.MovieResult, provOverrides map[string]*resultstore.ProvenanceData, legs func(plan *EditCommitPlan)) error {
	env := m.pe.currentEnv()
	publish := func() error { return m.publishCandidates(candidates) }
	if env != nil && env.committer != nil && env.envelope != nil {
		plan := &EditCommitPlan{
			EnvelopeFn: func() (*models.Job, error) {
				return env.envelope(candidates, provOverrides, nil)
			},
			// Provenance publication rides INSIDE plan.Publish so it lands
			// while the envelope lock is still held (Commit's lock spans
			// publish): a concurrent other-family commit can never snapshot
			// an envelope missing this op's provenance.
			Publish: func() error {
				if err := publish(); err != nil {
					return err
				}
				m.publishProvenance(provOverrides)
				return nil
			},
		}
		if legs != nil {
			legs(plan)
		}
		if err := env.committer.Commit(ctx, plan); err != nil {
			return err
		}
		return nil
	}

	// Legacy best-effort: DB legs via direct repo calls (movie upsert first).
	plan := &EditCommitPlan{}
	if legs != nil {
		legs(plan)
	}
	if err := m.legacyDBLegs(ctx, plan); err != nil {
		return err
	}
	if err := publish(); err != nil {
		return err
	}
	m.publishProvenance(provOverrides)
	if env != nil && env.persistFn != nil {
		if err := env.persistFn(); err != nil {
			logging.Warnf("envelope persist failed after edit on %s: %v (state remains committed in memory)", m.movieID, err)
		}
	}
	return nil
}

func (m *LockedMovieOps) publishProvenance(provOverrides map[string]*resultstore.ProvenanceData) {
	for fp, prov := range provOverrides {
		m.pe.updater.SetProvenance(fp, prov)
	}
}

// legacyDBLegs performs the non-transactional fallback DB writes used when no
// EditCommitter seam exists (in-memory stores, tests). Renames precede the
// movie upsert (pre-hardening order) so the upserter's fill-merge surfaces
// edited names in memory. Asset/lookup branches stay best-effort.
func (m *LockedMovieOps) legacyDBLegs(ctx context.Context, plan *EditCommitPlan) error {
	repo := m.pe.currentMovieRepo()
	if plan.UpsertMovie != nil && repo == nil {
		plan.Renames = nil // no DB row ⇒ actress renames would be discarded anyway
	}
	// Renames BEFORE the upsert (pre-hardening order): the upserter's
	// fill-merge looks up actress rows by ID/name, so the renamed record must
	// exist first or the in-memory movie loses the edit.
	if plan.UpsertMovie != nil && repo != nil {
		if err := m.legacyRenames(ctx, plan); err != nil {
			return err
		}
		if _, err := repo.Upsert(ctx, plan.UpsertMovie); err != nil {
			return fmt.Errorf("persist movie update: %w", err)
		}
	}
	if plan.MutateMovie != nil && plan.MutateMovieID != "" && repo != nil {
		existing, err := repo.FindByID(ctx, plan.MutateMovieID)
		switch {
		case err != nil && !database.IsNotFound(err):
			// Best-effort parity with the pre-hardening from-URL leg: a
			// lookup failure on the optional movie-row mutation is logged
			// and skipped, never propagated (the composite-tx path IS
			// strict — writeEditOpError maps it to 5xx with rollback).
			logging.Warnf("Failed to find movie %s for poster update: %v", plan.MutateMovieID, err)
		case err == nil && existing != nil:
			plan.MutateMovie(existing)
			if _, err := repo.Upsert(ctx, existing); err != nil {
				logging.Warnf("Failed to update movie poster in database: %v", err)
			}
		default:
			logging.Warnf("movie %s not found for mutation; skipping DB leg", plan.MutateMovieID)
		}
	}
	return nil
}

// legacyRenames runs the rename leg for the non-transactional fallback.
func (m *LockedMovieOps) legacyRenames(ctx context.Context, plan *EditCommitPlan) error {
	if len(plan.Renames) == 0 {
		return nil
	}
	var actressRepo database.ActressRepositoryInterface
	if env := m.pe.currentEnv(); env != nil {
		actressRepo = env.actressRepo
	}
	if actressRepo == nil {
		return nil
	}
	for _, rn := range plan.Renames {
		existing, err := actressRepo.FindByID(ctx, rn.ID)
		if err != nil {
			if database.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("load actress for rename: %w", err)
		}
		if existing == nil || (existing.FirstName == rn.FirstName && existing.LastName == rn.LastName && existing.JapaneseName == rn.JapaneseName) {
			continue
		}
		if err := actressRepo.RenameNameFields(ctx, rn.ID, rn.FirstName, rn.LastName, rn.JapaneseName); err != nil {
			return fmt.Errorf("persist actress name edit: %w", err)
		}
	}
	return nil
}

// fileFirstMovieResult returns the first family file's non-nil result.
func fileFirstMovieResult(m *LockedMovieOps, filePaths []string) *resultstore.MovieResult {
	for _, fp := range filePaths {
		if r, err := m.pe.lookup.GetMovieResult(fp); err == nil && r != nil && r.Movie != nil {
			return r
		}
	}
	return nil
}

// isSafePosterFileID reports whether id is a bare single-component file
// stem — safe for filepath.Join under the job poster directory (codex r33:
// both the incoming rekey ID AND the stored canonical ID must pass this
// before any filesystem operation touches the pair).
func isSafePosterFileID(id string) bool {
	return id != "" && id != "." && id != ".." &&
		filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

// hasUnresolvedPromoteWitness reports whether a promote witness for posterID
// exists on disk — the apply write-back fence's probe (codex P2): the
// write-back bumps the result revision, and startup arbitration reads
// revision>prev_revision as "promote committed", so the bump is poisonous
// while recovery state can still exist.
func (pe *PosterEditor) hasUnresolvedPromoteWitness(posterID string) bool {
	env := pe.currentEnv()
	if env == nil || posterID == "" {
		return false
	}
	// audit R2: ALL witness kinds fence the write-back — a crop witness's
	// arbitration discriminator degenerates to revision-only once the row
	// carries the deterministic crop URL, so a bumped revision would flip an
	// UNCOMMITTED crop to committed. Probe errors fence conservatively too
	// (audit R3-4), but get a DISTINCT warning so a skipped apply write-back
	// is distinguishable from a genuine outstanding-witness fence.
	err := posterWitnessConflict(env.fs, env.tempDir, env.jobID, posterID)
	if err == nil {
		return false
	}
	var cfe *EditAdmissionConflictError
	if !errors.As(err, &cfe) {
		logging.Warnf("poster witness probe failed for %s (%v) — fencing write-back conservatively", posterID, err)
	}
	return true
}

// posterWitnessFence refuses edits while a promote or crop witness for
// posterID is unresolved (codex P2 arbitration-integrity): an UNRELATED
// same-family PATCH would advance the result revision while the durable row
// still names the witness's URL, so at restart the reconciler would
// misclassify the pending promote/crop as committed and sweep the witness,
// preserving uncommitted (or deleting recoverable staged) bytes. Fence ALL
// revision-advancing edits uniformly; scan errors fail closed.
func posterWitnessFence(env *posterEditEnv, posterID string) error {
	if env == nil {
		return nil
	}
	return posterWitnessConflict(env.fs, env.tempDir, env.jobID, posterID)
}

// rekeyWitnessIDsFor matches pending rekey witnesses by CONTENT (audit
// F-R6-1): a transition touches BOTH identities — a fence that probes only
// the OLD-spelled filename leaves the NEW side unprotected (foreign
// rescrape/download resolve into it, park its stranded legs, discard them).
// Reads fail closed only on scan IO errors; corrupt payloads skip to the
// reconciler's ownership.
func rekeyWitnessIDsFor(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if errors.Is(err, afero.ErrFileNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("rekey witness scan %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, rekeyWitnessPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return false, fmt.Errorf("rekey witness scan %s: %w", name, rerr)
		}
		var w rekeyWitness
		if json.Unmarshal(data, &w) != nil {
			continue
		}
		if strings.EqualFold(w.OldID, posterID) || strings.EqualFold(w.NewID, posterID) {
			return true, nil
		}
	}
	return false, nil
}

// posterWitnessConflict is the raw-seams core of posterWitnessFence (audit
// F1): rescrape plumbing carries fs/tempDir/jobID without a posterEditEnv.
// posterWitnessConflict is the full edit-side witness fence: the three
// witness kinds (core) PLUS in-flight rescrape park markers (F-R10-3).
func posterWitnessConflict(fs afero.Fs, tempDir, jobID, posterID string) error {
	if err := posterWitnessConflictCore(fs, tempDir, jobID, posterID); err != nil {
		return err
	}
	if fs == nil || tempDir == "" || jobID == "" || posterID == "" {
		return nil
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	if parked, perr := rescrapeInFlightBackupPresent(fs, dir, posterID); perr != nil {
		return fmt.Errorf("poster rekey backup-scan: %w", perr)
	} else if parked {
		return &EditAdmissionConflictError{Message: fmt.Sprintf("poster %s has an in-flight rescrape — retry after it completes", posterID)}
	}
	return nil
}

// promoteWitnessPendingCore reports whether a promote witness names this
// poster — by CONTENT (fold-cased payload ID) first, with a name-fallback
// for legacy contentless payloads (codex cloud P1: probes must fence by family
// identity regardless of the ID's byte spelling).
func promoteWitnessPendingCore(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if errors.Is(err, afero.ErrFileNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("promote witness scan %s: %w", dir, err)
	}
	want := strings.TrimSpace(posterID)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, promoteWitnessPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return false, fmt.Errorf("promote witness scan %s: %w", name, rerr)
		}
		var w promoteWitness
		if json.Unmarshal(data, &w) == nil && w.PosterID != "" {
			if strings.EqualFold(strings.TrimSpace(w.PosterID), want) {
				return true, nil
			}
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(name, promoteWitnessPrefix), ".json")
		if id, uerr := url.PathUnescape(raw); uerr == nil {
			raw = id
		}
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return true, nil
		}
	}
	return false, nil
}

// removeWithRetry retries a transient fs removal a bounded number of times.
// A wedged sweep of a COMMITTED witness used to strand it until the next
// process start — poisoning every admission fence for the family meanwhile
// (codex cloud P2).
const witnessSweepRetries = 3

func removeWithRetry(fs afero.Fs, path string) error {
	var err error
	for i := 0; i < witnessSweepRetries; i++ {
		if err = fs.Remove(path); err == nil || errors.Is(err, afero.ErrFileNotFound) {
			return nil
		}
	}
	return err
}

// posterWitnessConflictCore probes promote/rekey/crop witnesses only — the
// rescrape pipeline's own probes use it (rescrape-vs-rescrape last-writer-
// wins stays legal: F-R10-1 keys the generation byte windows).
func posterWitnessConflictCore(fs afero.Fs, tempDir, jobID, posterID string) error {
	if fs == nil || tempDir == "" || jobID == "" || posterID == "" {
		return nil
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	// codex cloud P1 (case-fold probes): an exact-name Stat missed witnesses
	// written under a case-variant spelling — scan contents, fold identity.
	if hit, pwErr := promoteWitnessPendingCore(fs, dir, posterID); pwErr != nil {
		return fmt.Errorf("poster promote witness check: %w", pwErr)
	} else if hit {
		return &EditAdmissionConflictError{Message: fmt.Sprintf("poster %s promote witness unresolved: restart to reconcile", posterID)}
	}
	// audit F5+F-R6-1: rekey witnesses fence by CONTENT at BOTH identities —
	// a plain PATCH's post-commit eviction could delete the old-ID pair while
	// a leg is stranded mid-relocation, and a foreign rescue of the NEW id
	// would overwrite the stranded bytes.
	if hit, serr := rekeyWitnessIDsFor(fs, dir, posterID); serr != nil {
		return fmt.Errorf("poster rekey witness check: %w", serr)
	} else if hit {
		return &EditAdmissionConflictError{Message: fmt.Sprintf("poster rekey witness unresolved for %s: the previous rekey left stranded moves — restart to reconcile before rekeying again", posterID)}
	}
	entries, err := afero.ReadDir(fs, dir)
	if err != nil && !errors.Is(err, afero.ErrFileNotFound) {
		return fmt.Errorf("poster crop witness scan %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".crop-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return fmt.Errorf("poster crop witness scan %s: %w", name, rerr)
		}
		var cw struct {
			PosterID string `json:"poster_id"`
		}
		if json.Unmarshal(data, &cw) == nil && strings.EqualFold(strings.TrimSpace(cw.PosterID), strings.TrimSpace(posterID)) {
			return &EditAdmissionConflictError{Message: fmt.Sprintf("poster %s crop witness unresolved: restart to reconcile", posterID)}
		}
	}
	// codex cloud P2: an outstanding eviction witness fences further poster
	// ops too — the surviving canon may predate the durable row until
	// reconcile runs, so any edit must not commit geometry measured on it.
	if pendingEvict, evErr := pendingEvictWitnessCore(fs, dir, posterID); evErr != nil {
		return fmt.Errorf("poster eviction witness check: %w", evErr)
	} else if pendingEvict {
		return &EditAdmissionConflictError{Message: fmt.Sprintf("poster %s has unresolved eviction — restart to reconcile", posterID)}
	}
	return nil
}

// rescrapeInFlightBackupPresent reports whether the dir holds a rescrape
// park backup for posterID (F-R8-2's in-flight marker). Dir-read errors fail
// CLOSED (error out) — a wedged listing must never silently drop the fence.
func rescrapeInFlightBackupPresent(fs afero.Fs, dir, posterID string) (bool, error) {
	rEntries, rerr := afero.ReadDir(fs, dir)
	if rerr != nil {
		if errors.Is(rerr, afero.ErrFileNotFound) {
			return false, nil
		}
		return false, rerr
	}
	inflightPrefix := ".inflight-" + url.PathEscape(posterID) + "."
	loFull := strings.ToLower(posterID + "-full.jpg.rsbak.")
	loCrop := strings.ToLower(posterID + ".jpg.rsbak.")
	loMark := strings.ToLower(inflightPrefix)
	for _, e := range rEntries {
		n := e.Name()
		nl := strings.ToLower(n)
		// audit codex P2: fold at the probe — a same-file marker parked under
		// the scraper's case variant registers on case-insensitive filesystems.
		if strings.HasPrefix(nl, loCrop) || strings.HasPrefix(nl, loFull) {
			return true, nil
		}
		// audit F-R20-2: marker probes are nonce-anchored — canonical files of
		// an ID beginning ".inflight-" never read as an in-flight sentinel.
		if markerAnchored(n) && strings.HasPrefix(nl, loMark) {
			return true, nil
		}
	}
	return false, nil
}

// rewriteTempPosterURL rewrites a /api/v1/temp/posters/<job>/<id>.jpg URL to
// the rekeyed identity (codex r39 P2): the bytes moved with the relocation,
// so the stored crop URL must follow or the review UI shows a broken poster
// until the next manual crop.
func rewriteTempPosterURL(rawURL, jobID, oldID, newID string) string {
	if rawURL == "" {
		return rawURL
	}
	path, suffix := rawURL, ""
	if i := strings.Index(rawURL, "?"); i >= 0 {
		path, suffix = rawURL[:i], rawURL[i:]
	}
	oldSegment := "/" + url.PathEscape(jobID) + "/" + url.PathEscape(oldID) + ".jpg"
	if !strings.HasSuffix(path, oldSegment) {
		return rawURL
	}
	return path[:len(path)-len(oldSegment)] + "/" + url.PathEscape(jobID) + "/" + url.PathEscape(newID) + ".jpg" + suffix
}

// evictStalePosterPair removes the installed preview pair for a source that
// just changed (D7-lite). Post-commit only; misses are no-ops.
func (m *LockedMovieOps) evictStalePosterPair(posterID, wpath string) {
	if !isSafePosterFileID(posterID) {
		// codex r33: an unsafe legacy/scraper ID would resolve the removal
		// outside the job poster directory — never join it.
		logging.Warnf("stale poster evict skipped: unsafe poster ID %q", posterID)
		return
	}
	env := m.pe.currentEnv()
	if env == nil || env.tempDir == "" || env.jobID == "" || env.fs == nil {
		return
	}
	dir := filepath.Join(env.tempDir, "posters", env.jobID)
	// codex cloud P2 (@rrHy): a failed leg remove keeps the witness — swept
	// post-commit records orphan it if written over the remaining files.
	failed := false
	for _, name := range []string{posterID + "-full.jpg", posterID + ".jpg"} {
		if err := env.fs.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, afero.ErrFileNotFound) {
			failed = true
			logging.Warnf("stale poster evict %s: %v (witness kept for startup)", name, err)
		}
	}
	if !failed && wpath != "" {
		if err := removeWithRetry(env.fs, wpath); err != nil {
			logging.Warnf("stale poster evict witness sweep %s: %v", wpath, err)
		}
	}
}

// writeFileAtomicForEvict mirrors the witness write pattern: tmp + rename.
func writeFileAtomicForEvict(fs afero.Fs, path string, payload []byte) error {
	tmp := path + ".tmp"
	if wErr := afero.WriteFile(fs, tmp, payload, 0o644); wErr != nil {
		return wErr
	}
	if rErr := fs.Rename(tmp, path); rErr != nil {
		_ = fs.Remove(tmp)
		return rErr
	}
	return nil
}

// sweepEvictionWitness removes a witness whose commit never landed — the
// bounded retry covers transient wedges (codex cloud P2); any durable failure
// leaves the witness for startup reconciler clean-up.
func sweepEvictionWitness(fs afero.Fs, wpath string) {
	if err := removeWithRetry(fs, wpath); err != nil {
		logging.Warnf("eviction witness sweep %s: %v", wpath, err)
	}
}

// writeEvictWitness persists the eviction record BEFORE the metadata commit —
// a crash between the durable write and the physical removals still leaves the
// reconcile-complete marker on disk (codex cloud P2).
// writeEvictWitness persists the eviction record inline-BEFORE the tag on the ✔
func writeEvictWitness(fs afero.Fs, dir, posterID, newSourceURL, forFilePath string) (string, error) {
	wpath := filepath.Join(dir, ".evict-"+url.PathEscape(posterID)+".json")
	// codex cloud P2: a never-postered job has NO posters dir — MkdirAll allows
	// the source-change witness to persist even when nothing was downloaded yet.
	if mErr := fs.MkdirAll(dir, 0o755); mErr != nil {
		return "", fmt.Errorf("evict witness dir %s: %w", dir, mErr)
	}
	payload, _ := json.Marshal(evictWitness{OldID: posterID, NewSourceURL: newSourceURL, FilePath: forFilePath})
	if err := writeFileAtomicForEvict(fs, wpath, payload); err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return wpath, nil
}

// --- Cores (exported for adapter + handler composition; must ONLY run under the family key) ---

// UpdatePosterCrop updates the cropped poster URL and manual crop geometry
// for the whole family. Called under WithMovieEditLock.
func (m *LockedMovieOps) UpdatePosterCrop(croppedURL string, bounds *models.CropBounds, sourceFull bool) error {
	filePaths, err := m.familyFilePaths()
	if err != nil {
		return err
	}
	candidates := m.mutateCandidates(filePaths, func(movie *models.Movie) {
		backupPosterOriginals(movie)
		movie.Poster.CroppedPosterURL = croppedURL
		movie.Poster.ShouldCropPoster = false
		movie.Poster.PosterCropBounds = bounds
		movie.Poster.PosterCropSourceFull = sourceFull
	})
	if len(candidates) == 0 {
		return fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, m.movieID)
	}
	return m.commitCandidate(context.Background(), candidates, nil, nil)
}

// UpdatePosterFromURL updates the poster URL + cropped URL for the family and
// persists the poster change to the movie row. Called under WithMovieEditLock.
func (m *LockedMovieOps) UpdatePosterFromURL(ctx context.Context, posterURL string, croppedURL string) error {
	filePaths, err := m.familyFilePaths()
	if err != nil {
		return err
	}
	candidates := m.mutateCandidates(filePaths, func(movie *models.Movie) {
		backupPosterOriginals(movie)
		movie.Poster.PosterURL = posterURL
		movie.Poster.CroppedPosterURL = croppedURL
		movie.Poster.ShouldCropPoster = false
		clearPosterCropGeometry(movie) // new source: stored geometry is stale
	})
	if len(candidates) == 0 {
		return fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, m.movieID)
	}
	posterID := m.movieID
	if mr, _ := m.pe.lookup.FindMovieResultForMovieID(m.movieID); mr != nil && mr.Movie != nil && mr.Movie.ID != "" {
		posterID = mr.Movie.ID
	}
	return m.commitCandidate(ctx, candidates, nil, func(plan *EditCommitPlan) {
		plan.MutateMovieID = posterID
		plan.MutateMovie = func(mv *models.Movie) {
			mv.Poster.PosterURL = posterURL
			mv.Poster.CroppedPosterURL = croppedURL
		}
	})
}

// UpdateMovieFamily applies a whole-movie save to every file in the family
// with the PATCH ordering contract (movie-row upsert before actress renames;
// renames execute from a pre-Upsert name snapshot). Called under
// WithMovieEditLock. Geometry sanitization runs against the FIRST family
// file's current state (deterministic, family-scoped).
func (m *LockedMovieOps) UpdateMovieFamily(ctx context.Context, movie *models.Movie) error {
	if movie == nil {
		return fmt.Errorf("movie is required")
	}
	// codex P1-F: an empty ID would restamp every part with a blank matcher
	// key — the family index stops resolving the family entirely and the
	// envelope persists poison. Reject before any candidate leg runs.
	if strings.TrimSpace(movie.ID) == "" {
		return &EditAdmissionConflictError{Message: "movie ID must not be empty — identity changes belong to rescrape/clear flows"}
	}
	filePaths, err := m.familyFilePaths()
	if err != nil {
		return err
	}
	if err := m.rejectIdentityChangeLocked(filePaths, movie); err != nil {
		return err
	}

	// Pre-Upsert snapshot of requested actress name fields (D4): Upsert may
	// normalize movie.Actresses in place, so renames capture intent first.
	renames := make([]ActressRenamePlan, 0, len(movie.Actresses))
	for _, a := range movie.Actresses {
		if a.ID == 0 {
			continue
		}
		renames = append(renames, ActressRenamePlan{ID: a.ID, FirstName: a.FirstName, LastName: a.LastName, JapaneseName: a.JapaneseName})
	}

	// Family-scoped sanitize against the first file WITH a movie (legacy
	// behavior keys cover-original backup and crop-geometry invalidation off
	// the file being saved; the family shares one movie row, so one baseline
	// suffices).
	var have bool
	var curPosterURL, curCoverURL string
	var curShouldCrop bool
	for _, fp := range filePaths {
		cur, err := m.pe.lookup.GetMovieResult(fp)
		if err != nil || cur == nil || cur.Movie == nil {
			continue
		}
		// Preserve server-owned baseline snapshot fields (codex r22): clients
		// never send Original* — sanitize would otherwise let a payload with
		// empty originals clobber the Reset baseline on every part.
		if movie.Poster.OriginalPosterURL == "" && movie.Poster.OriginalShouldCropPoster == nil {
			movie.Poster.OriginalPosterURL = cur.Movie.Poster.OriginalPosterURL
			movie.Poster.OriginalCroppedPosterURL = cur.Movie.Poster.OriginalCroppedPosterURL
			movie.Poster.OriginalShouldCropPoster = cur.Movie.Poster.OriginalShouldCropPoster
		}
		backupCoverOriginal(cur.Movie, movie)
		// Preserve the stored content-id on omitted code (codex r29): an absent
		// code in the payload means "keep", not "clear the DB primary key".
		if movie.ContentID == "" && cur.Movie.ContentID != "" {
			movie.ContentID = cur.Movie.ContentID
		}
		have = true
		curPosterURL = cur.Movie.Poster.PosterURL
		curCoverURL = cur.Movie.Poster.CoverURL
		curShouldCrop = cur.Movie.Poster.ShouldCropPoster
		break
	}
	sanitizePosterCropGeometry(movie, have, curPosterURL, curCoverURL, curShouldCrop)

	candidates := m.mutateCandidates(filePaths, func(mv *models.Movie) {})
	if len(candidates) == 0 {
		return fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, m.movieID)
	}
	// Capture the canonical PRE-EDIT poster identity before any mutation of
	// the movie pointer — candidates alias theincoming movie below, so post-
	// commit reads would see the NEW identity and relocation would no-op (codex r25).
	canonicalOldPosterID := m.movieID
	prevRevision := uint64(0)
	ownerResultID := ""
	if res := fileFirstMovieResult(m, filePaths); res != nil && res.Movie != nil && res.Movie.ID != "" {
		canonicalOldPosterID = res.Movie.ID
		prevRevision = res.Revision
		// audit F-R8-1: pin, canonical identity, and revision ALL come from the
		// same result object — a movie-less sibling part (failed scrape) can
		// never supply a divergent pin that would make startup arbitration
		// reverse a committed rekey.
		ownerResultID = res.ResultID
	}
	// D6-lite eviction (codex r22): when the PATCH changed the effective
	// poster source (sanitize already cleared the geometry), the installed
	// preview pair now describes the OLD image. Evict it so later crops can't
	// commit geometry measured on stale bytes.
	sourceChanged := have && effectivePosterSourceOf(movie.Poster.PosterURL, movie.Poster.CoverURL) != effectivePosterSourceOf(curPosterURL, curCoverURL)
	// Evict at the PRE-COMMIT canonical identity (the installed pair lives
	// there — candidates alias the NEW movie after commit, so reading them
	// Pre-commit path traversal hygiene (codex r26): an accepted family rekey
	// writes ⟨newID⟩-full.jpg etc. under the job's poster dir — validate the
	// filename shape HERE, before any DB/envelope state moves, so an unsafe
	// rekey ID is rejected before the transaction starts.
	if movie != nil {
		// codex r33: validate UNCONDITIONALLY — an incoming ID that merely
		// equals the matcher alias skips the earlier conditional check yet
		// still lands in the relocation join when it differs from the stored
		// canonical ID.
		newID := strings.TrimSpace(movie.ID)
		if newID != "" && !isSafePosterFileID(newID) {
			return &EditAdmissionConflictError{Message: fmt.Sprintf("movie ID %q is not a safe file name for rekey relocation", newID)}
		}
		// codex r48 P2: keep ONE spelling everywhere — validation, relocation,
		// witness URLs AND the committed row use the trimmed identity, never
		// an un-normalized payload (whitespace-drifting IDs would move bytes
		// under "NEW" while persisting " NEW ").
		movie.ID = newID
	}
	stalePosterID := ""
	if sourceChanged {
		if mr, _ := m.pe.lookup.FindMovieResultForMovieID(m.movieID); mr != nil && mr.Movie != nil && mr.Movie.ID != "" {
			stalePosterID = mr.Movie.ID
		} else {
			stalePosterID = m.movieID
		}
		// Codex r24: when the pair is about to evicted, the persisted cropped
		// URL would point at the deleted bytes — clear it with the geometry.
		if movie != nil && movie.Poster.CroppedPosterURL != "" && movie.Poster.PosterCropBounds == nil {
			movie.Poster.CroppedPosterURL = ""
		}
	}

	// Whole-movie save: every part's candidate aliases the LIVE movie
	// pointer (not a pre-prepare clone) — Upsert normalizes Actresses in
	// place inside the tx, and both the envelope encode and the post-commit
	// publish must see those normalized IDs (stale-ID anti-resurrection).
	for _, cand := range candidates {
		cand.Movie = movie
		retainMovieAlias(cand, movie.ID)
	}
	// Relocate the poster pair BEFORE the commit (codex r27): a failed
	// relocation means no DB/envelope mutation at all; a failed COMMIT after
	// a successful relocation rolls the pair back via reverse renames.
	// codex r33 P1: the STORED canonical ID feeds the source joins below —
	// validate it unconditionally before any filesystem operation. An unsafe
	// stored ID (legacy/scraper-produced) skips relocation: the save proceeds
	// and the pair stays at its existing name.
	canonicalPairSafe := isSafePosterFileID(canonicalOldPosterID)
	if !canonicalPairSafe {
		logging.Warnf("poster rekey relocation skipped: stored canonical ID %q is not a safe file name", canonicalOldPosterID)
	}
	// codex P2 arbitration fence: refuse EVERY same-family commit (plain PATCH
	// included) while a promote/crop witness for this poster is unresolved —
	// otherwise the revision advance would make a pending witness arbitrate as
	// committed at the next startup. The relocation block keeps only the
	// rekey-specific self-witness check.
	if err := posterWitnessFence(m.pe.currentEnv(), canonicalOldPosterID); err != nil {
		return err
	}
	var relocatedPosterPair []struct{ src, dst string }
	relocatedNewID, relocatedJobID := "", ""
	rekeyWitnessPath := ""
	trOld := strings.TrimSpace(canonicalOldPosterID)
	relocateArmed := func(env *posterEditEnv, dir, newID string) bool {
		// codex r43 P2a: byte-different fold-EQUAL rekeys (pure capitalization
		// change) must still relocate on case-SENSITIVE filesystems — the
		// committed row now names the new spelling, and crops/stat lookups are
		// case-exact there. On case-insensitive filesystems both spellings
		// resolve to the same entry: nothing to move.
		if strings.EqualFold(newID, trOld) {
			ciProbe := env.ciProbe
			if ciProbe == nil {
				ciProbe = fscase.NewFSCaseCache(env.fs).IsCaseInsensitive
			}
			if ciProbe(dir) {
				return false
			}
		}
		return true
	}
	if newID := strings.TrimSpace(movie.ID); newID != "" && canonicalPairSafe && newID != trOld {
		if env := m.pe.currentEnv(); env != nil && env.fs != nil && env.tempDir != "" && env.jobID != "" {
			dir := filepath.Join(env.tempDir, "posters", env.jobID)
			if relocateArmed(env, dir, newID) {
				// codex r40 P2: durable witness BEFORE the first rename — a crash
				// between rename and commit leaves the pair at the NEW identity
				// while the durable row still references the old one; the startup
				// reconciler (ReconcileRekeyWitnesses) arbitrates from the job row.
				witnessPath := filepath.Join(dir, rekeyWitnessPrefix+canonicalOldPosterID+".json")
				// codex r50 P2: an UNRESOLVED witness means a past mid-relocation
				// crash (or incomplete rollback) already moved some legs to another
				// identity — overwriting it would orphan those bytes beyond
				// recovery. The startup reconciler clears witnesses; reject until then.
				// audit F-R6-1: the NEW identity must be free of ANY pending
				// rekey witness (as its Old OR New leg) — a stranded foreign
				// relocation's reversal would otherwise steal this pair.
				if hit, derr := rekeyWitnessIDsFor(env.fs, dir, newID); derr != nil {
					return fmt.Errorf("poster rekey destination check: %w", derr)
				} else if hit {
					return &EditAdmissionConflictError{Message: fmt.Sprintf("poster rekey destination %s has an unresolved witness — restart to reconcile", newID)}
				}
				// audit F-R7-1: refuse the relocation when a SIBLING family
				// shares the old canonical ID — its parts share the same bytes
				// and the pinned arbitration gates would misread forever.
				for _, sib := range m.pe.lookup.SnapshotData().Results {
					if sib == nil || sib.Movie == nil || !strings.EqualFold(strings.TrimSpace(sib.Movie.ID), canonicalOldPosterID) {
						continue
					}
					famAlias := strings.TrimSpace(sib.FileMatchInfo.MovieID)
					if famAlias == "" || strings.EqualFold(famAlias, m.movieID) {
						continue
					}
					return &EditAdmissionConflictError{Message: fmt.Sprintf("poster pair %s is shared with family %s — rekey refused while a sibling references it", canonicalOldPosterID, famAlias)}
				}
				// audit F-R8-2/F-R10-3: the SOURCE side is covered by the
				// witness fence's parked probe (posterWitnessConflict runs
				// BEFORE this block); here we probe only the DESTINATION —
				// an in-flight rescrape of a DIFFERENT family may have litter
				// parked at the destination whose losing closeout would
				// restore over our freshly committed pair.
				inFlight2, bErr2 := rescrapeInFlightBackupPresent(env.fs, dir, newID)
				if bErr2 != nil {
					return fmt.Errorf("poster rekey backup-scan %s: %w", dir, bErr2)
				}
				if inFlight2 {
					return &EditAdmissionConflictError{Message: fmt.Sprintf("poster rekey target %s has an in-flight rescrape — retry after it completes", newID)}
				}
				wBytes, _ := json.Marshal(rekeyWitness{OldID: canonicalOldPosterID, NewID: newID, PrevRevision: prevRevision, ResultID: ownerResultID})
				tmpPath := witnessPath + ".tmp"
				if err := afero.WriteFile(env.fs, tmpPath, wBytes, 0o644); err != nil {
					return fmt.Errorf("poster rekey witness write %s: %w", tmpPath, err)
				}
				if err := env.fs.Rename(tmpPath, witnessPath); err != nil {
					_ = env.fs.Remove(tmpPath)
					return fmt.Errorf("poster rekey witness rename %s: %w", witnessPath, err)
				}
				rekeyWitnessPath = witnessPath
				failedErr := error(nil)
				for _, suffix := range []string{"-full.jpg", ".jpg"} {
					src := filepath.Join(dir, canonicalOldPosterID+suffix)
					dst := filepath.Join(dir, newID+suffix)
					if _, err := env.fs.Stat(src); err != nil {
						if errors.Is(err, afero.ErrFileNotFound) {
							continue // leg never existed — nothing to move
						}
						// codex r43 P2b: a TRANSIENT stat error must not silently
						// drop the leg — abort coherently with any partial renames
						// rolled back below.
						failedErr = fmt.Errorf("poster rekey source stat %s: %w", src, err)
						break
					}
					if _, err := env.fs.Stat(dst); err == nil {
						failedErr = fmt.Errorf("poster target %s already exists", dst)
						break
					} else if !errors.Is(err, afero.ErrFileNotFound) {
						// codex r46 P2c: only absence permits the rename — a transient
						// destination error fed into a rename-REPLACES fs would
						// destroy existing new-ID bytes with no witness or backup.
						failedErr = fmt.Errorf("poster rekey target stat %s: %w", dst, err)
						break
					}
					if err := env.fs.Rename(src, dst); err != nil {
						failedErr = fmt.Errorf("poster rekey move %s→%s: %w", src, dst, err)
						break
					}
					relocatedPosterPair = append(relocatedPosterPair, struct{ src, dst string }{src, dst})
				}
				if failedErr != nil {
					// Roll back partially moved pairs before any DB write; the state
					// stays on the old ID, coherently rejected. The witness survives
					// an INCOMPLETE rollback so the reconciler can finish it (r40).
					rollbackComplete := true
					for _, mv := range relocatedPosterPair {
						if rbErr := env.fs.Rename(mv.dst, mv.src); rbErr != nil {
							rollbackComplete = false
							logging.Warnf("poster rekey rollback %s→%s failed: %v", mv.dst, mv.src, rbErr)
						}
					}
					if rollbackComplete {
						if rmErr := env.fs.Remove(witnessPath); rmErr != nil && !errors.Is(rmErr, afero.ErrFileNotFound) {
							logging.Warnf("poster rekey witness sweep %s: %v", witnessPath, rmErr)
						}
					}
					return failedErr
				}
				if len(relocatedPosterPair) > 0 {
					relocatedNewID, relocatedJobID = newID, env.jobID
				}
			}
		}
	}
	// codex r39 P2: the bytes moved to the new identity — rewrite the stored
	// temp-poster URLs NOW (pre-commit) so a saved + restarted session shows
	// the crop instead of a dangling pointer at the removed old-ID path.
	if relocatedNewID != "" {
		movie.Poster.CroppedPosterURL = rewriteTempPosterURL(movie.Poster.CroppedPosterURL, relocatedJobID, canonicalOldPosterID, relocatedNewID)
		movie.Poster.OriginalCroppedPosterURL = rewriteTempPosterURL(movie.Poster.OriginalCroppedPosterURL, relocatedJobID, canonicalOldPosterID, relocatedNewID)
	}

	// codex cloud P2 (eviction durability): the eviction witness must exist
	// BEFORE commitCandidate commits the new metadata — a crash between commit
	// and witness-write would otherwise strand the old canonical pair beneath
	// the new source with zero recovery record.
	evictWitnessPath := ""
	if stalePosterID != "" {
		if env := m.pe.currentEnv(); env != nil && env.fs != nil && env.tempDir != "" && env.jobID != "" {
			dir := filepath.Join(env.tempDir, "posters", env.jobID)
			// codex cloud P2 (@SJPt): rekeyed saves move the pair BEFORE the commit;
			// the witness must name the post-relocation identity, not the stale one.
			evictID := stalePosterID
			if len(relocatedPosterPair) > 0 && strings.TrimSpace(movie.ID) != "" {
				evictID = strings.TrimSpace(movie.ID)
			}
			evictWitnessPath, err = writeEvictWitness(env.fs, dir, evictID, effectivePosterSourceOf(movie.Poster.PosterURL, movie.Poster.CoverURL), filePaths[0])
			if err != nil {
				return fmt.Errorf("stale poster eviction witness %s: %w", evictWitnessPath, err)
			}
		}
	}

	err = m.commitCandidate(ctx, candidates, nil, func(plan *EditCommitPlan) {
		plan.UpsertMovie = movie
		plan.Renames = renames
	})
	if err != nil && evictWitnessPath != "" {
		// commit never landed — no eviction happened and none is due: the record
		// is pure litter; sweep it (bounded retry covers transient wedges).
		if env := m.pe.currentEnv(); env != nil && env.fs != nil {
			sweepEvictionWitness(env.fs, evictWitnessPath)
		}
	}
	if err != nil && len(relocatedPosterPair) > 0 {
		// Commit failed → state stays on the old identity; return the pair.
		// The witness survives an INCOMPLETE rollback (codex r40 P2) so the
		// startup reconciler can arbitrate the leftover new-ID files.
		if env := m.pe.currentEnv(); env != nil && env.fs != nil {
			rollbackComplete := true
			for _, mv := range relocatedPosterPair {
				if rbErr := env.fs.Rename(mv.dst, mv.src); rbErr != nil {
					rollbackComplete = false
					logging.Warnf("poster rekey rollback %s→%s failed: %v", mv.dst, mv.src, rbErr)
				}
			}
			if rollbackComplete && rekeyWitnessPath != "" {
				if rmErr := removeWithRetry(env.fs, rekeyWitnessPath); rmErr != nil && !errors.Is(rmErr, afero.ErrFileNotFound) {
					logging.Warnf("poster rekey witness sweep %s: %v", rekeyWitnessPath, rmErr)
				}
			}
		}
		return err
	}

	if err != nil && rekeyWitnessPath != "" {
		// codex P2 zero-legs: the commit failed with NO pair moved, so nothing
		// exists for the startup reconciler to arbitrate. A lingered witness
		// would poison every retry as an unresolved rekey until restart —
		// sweep it unconditionally here.
		if env := m.pe.currentEnv(); env != nil && env.fs != nil {
			if rmErr := removeWithRetry(env.fs, rekeyWitnessPath); rmErr != nil && !errors.Is(rmErr, afero.ErrFileNotFound) {
				logging.Warnf("poster rekey witness sweep %s: %v", rekeyWitnessPath, rmErr)
			}
		}
	}
	if err == nil && rekeyWitnessPath != "" {
		// Commit landed: the witness's job is done (files remain at the new
		// identity, coherent with the durable row).
		if env := m.pe.currentEnv(); env != nil && env.fs != nil {
			if rmErr := removeWithRetry(env.fs, rekeyWitnessPath); rmErr != nil && !errors.Is(rmErr, afero.ErrFileNotFound) {
				logging.Warnf("poster rekey witness sweep %s: %v", rekeyWitnessPath, rmErr)
			}
		}
	}
	if err == nil && stalePosterID != "" {
		evictTarget := stalePosterID
		if len(relocatedPosterPair) > 0 && strings.TrimSpace(movie.ID) != "" {
			evictTarget = strings.TrimSpace(movie.ID)
		}
		m.evictStalePosterPair(evictTarget, evictWitnessPath)
	}
	return err
}

// ApplyFieldOverride applies a single-field cherry-pick from a scraper source
// to the target result and re-attributes provenance. Called under
// WithMovieEditLock — the per-result overrideMu sync.Map is retired (D1's
// family key subsumes it). content_id/id override keys are identity keys and
// are rejected without a transactional rekey (D17).
func (m *LockedMovieOps) ApplyFieldOverride(ctx context.Context, resultID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
	if fieldKey == "content_id" || fieldKey == "id" {
		return nil, nil, &EditAdmissionConflictError{JobID: "", Message: "content-id / id changes must go through rescrape flows"}
	}
	result, filePath, found := m.pe.lookup.GetFileResultByResultID(resultID)
	if !found || result == nil || result.Movie == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, resultID)
	}
	// Revalidate the family key inside the lock (codex P1-B): a concurrent
	// PATCH may have rekeyed this result to a different movie after the
	// caller resolved the lock target pre-lock. If so, we hold the WRONG
	// family key — refuse; the wrapper retries under the fresh key.
	if result.FileMatchInfo.MovieID != "" && !strings.EqualFold(result.FileMatchInfo.MovieID, m.movieID) {
		return nil, nil, fmt.Errorf("%w: %s now belongs to %s", ErrFamilyRekeyed, resultID, result.FileMatchInfo.MovieID)
	}
	// codex P2 arbitration fence (mirrors UpdateMovieFamily): a field-level
	// cherry-pick advances this result's revision too — refuse it while a
	// promote/crop witness for its poster is unresolved.
	if err := posterWitnessFence(m.pe.currentEnv(), result.Movie.ID); err != nil {
		return nil, nil, err
	}
	prov := m.pe.lookup.GetProvenance(filePath)
	if prov == nil {
		prov = &resultstore.ProvenanceData{}
	}
	movie := result.Movie.Clone()
	if err := applyFieldOverride(movie, prov, fieldKey, source); err != nil {
		return nil, nil, err
	}
	sanitizePosterCropGeometry(movie, true, result.Movie.Poster.PosterURL, result.Movie.Poster.CoverURL, result.Movie.Poster.ShouldCropPoster)

	// P2 (D6/R13): an override that changed the EFFECTIVE poster source
	// carries the PATCH path's eviction contract — the witness is journaled
	// BEFORE the commit, the stale pair is evicted after it lands, and a
	// failed commit sweeps the never-armed witness. (The committed cropped
	// URL is already cleared by the mutator.)
	stalePosterID := ""
	evictWitnessPath := ""
	if effectivePosterSourceOf(movie.Poster.PosterURL, movie.Poster.CoverURL) != effectivePosterSourceOf(result.Movie.Poster.PosterURL, result.Movie.Poster.CoverURL) {
		stalePosterID = strings.TrimSpace(result.Movie.ID)
		if stalePosterID == "" {
			stalePosterID = m.movieID
		}
		if !isSafePosterFileID(stalePosterID) {
			// codex r33 parity: an unsafe legacy ID must never reach a join — but
			// the state-side clearing above already keeps the row coherent.
			logging.Warnf("override source-change eviction skipped: unsafe poster ID %q", stalePosterID)
			stalePosterID = ""
		}
		// codex PR#211 rounds 5+6: the canonical pair is IDENTITY-keyed, but
		// only a sibling whose rows still reference the OLD effective source
		// reads those bytes. A share-by-ID alone must not pin the pair forever
		// (already-migrated siblings would keep the stale bytes alive),
		// while an untouched sibling mustn't lose its preview.
		if stalePosterID != "" && m.pe.lookup != nil {
			oldEffective := effectivePosterSourceOf(result.Movie.Poster.PosterURL, result.Movie.Poster.CoverURL)
			snap := m.pe.lookup.SnapshotData()
			bysider := ""
			for fp, row := range snap.Results {
				if fp == filePath || row == nil || row.Movie == nil {
					continue
				}
				// codex PR#211 round 9: legacy rows can carry an EMPTY canonical
				// Movie.ID — their shared identity lives on the matcher alias
				// (FileMatchInfo.MovieID); compare the effective row identity.
				rowID := strings.TrimSpace(row.Movie.ID)
				if rowID == "" {
					rowID = strings.TrimSpace(row.FileMatchInfo.MovieID)
				}
				if !strings.EqualFold(rowID, stalePosterID) {
					continue
				}
				if effectivePosterSourceOf(row.Movie.Poster.PosterURL, row.Movie.Poster.CoverURL) == oldEffective {
					bysider = fp
					break
				}
			}
			if bysider != "" {
				logging.Infof("override source-change eviction skipped for %s: %s still uses the old source", stalePosterID, bysider)
				stalePosterID = ""
			}
		}
		if stalePosterID != "" {
			if env := m.pe.currentEnv(); env != nil && env.fs != nil && env.tempDir != "" && env.jobID != "" {
				dir := filepath.Join(env.tempDir, "posters", env.jobID)
				wp, werr := writeEvictWitness(env.fs, dir, stalePosterID, effectivePosterSourceOf(movie.Poster.PosterURL, movie.Poster.CoverURL), filePath)
				if werr != nil {
					return nil, nil, fmt.Errorf("stale poster eviction witness %s: %w", wp, werr)
				}
				evictWitnessPath = wp
			}
		}
	}

	cand := result.Clone()
	cand.Movie = movie
	retainMovieAlias(cand, movie.ID)
	// Predict the publication revision (codex r16): the envelope already
	// encoded by commit time would otherwise store revision N while
	// publication bumps memory to N+1 — after restart, conflict state
	// regresses by one.
	cand.Revision = result.Revision + 1
	candidates := map[string]*resultstore.MovieResult{filePath: cand}

	renames := make([]ActressRenamePlan, 0, len(movie.Actresses))
	for _, a := range movie.Actresses {
		if a.ID == 0 {
			continue
		}
		renames = append(renames, ActressRenamePlan{ID: a.ID, FirstName: a.FirstName, LastName: a.LastName, JapaneseName: a.JapaneseName})
	}
	if err := m.commitCandidate(ctx, candidates, map[string]*resultstore.ProvenanceData{filePath: prov}, func(plan *EditCommitPlan) {
		plan.UpsertMovie = movie
		plan.Renames = renames
	}); err != nil {
		if evictWitnessPath != "" {
			// Commit never landed ⇒ the eviction never armed; the record is pure
			// litter, swept best-effort (a durable failure leaves it for startup).
			if env := m.pe.currentEnv(); env != nil && env.fs != nil {
				sweepEvictionWitness(env.fs, evictWitnessPath)
			}
		}
		return nil, nil, fmt.Errorf("persist field override: %w", err)
	}
	if evictWitnessPath != "" && stalePosterID != "" {
		// Post-commit, always under this same locked section: remove the stale
		// pair; a failed leg keeps the witness for startup reconcile.
		m.evictStalePosterPair(stalePosterID, evictWitnessPath)
	}
	updated, _, _ := m.pe.lookup.GetFileResultByResultID(resultID)
	updatedProv := m.pe.lookup.GetProvenance(filePath)
	return updated, updatedProv, nil
}

// ApplyFieldOverrideWithRevisions performs the same locked override and
// captures the family revision map before the keyed section is released.
func (m *LockedMovieOps) ApplyFieldOverrideWithRevisions(ctx context.Context, resultID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, map[string]uint64, error) {
	result, prov, err := m.ApplyFieldOverride(ctx, resultID, fieldKey, source)
	if err != nil {
		return nil, nil, nil, err
	}
	return result, prov, m.familyRevisionSnapshot(), nil
}

func (m *LockedMovieOps) familyRevisionSnapshot() map[string]uint64 {
	paths := m.pe.lookup.FindFilePathsForMovieID(m.movieID)
	if len(paths) == 0 {
		return nil
	}
	revisions := make(map[string]uint64, len(paths))
	for _, path := range paths {
		result, err := m.pe.lookup.GetMovieResult(path)
		if err != nil || result == nil || result.ResultID == "" {
			continue
		}
		revisions[result.ResultID] = result.Revision
	}
	return revisions
}

// allExcludedTerminal reports whether the exclusion set covers EVERY known
// result while the job is still cancellable (codex P5-A): the committed
// envelope for such an exclusion must carry Cancelled atomically — a
// Pending+all-excluded row surviving a crash is resurrected as Failed by
// recoverOrphanedJobs.
//
// codex r33 P2: the terminal status is encoded in the CANDIDATE envelope
// ONLY — never on the live lifecycle pre-commit. AcquireEditAccess admits
// edits for Cancelled jobs; a transient live flip would admit a racing PATCH
// mid-commit, and persist that status even when the commit later fails (the
// old flip's rollback restored memory only).
func allExcludedTerminal(lc *JobLifecycle, excluded map[string]bool, lookup resultstore.ResultReadFacade) bool {
	if lc == nil || len(excluded) == 0 {
		return false
	}
	st := lc.GetJobStatus()
	if st != models.JobStatusPending && st != models.JobStatusRunning {
		return false
	}
	ra, ok := lookup.(resultstore.ResultMapAccessor)
	if !ok {
		return false
	}
	snap := ra.SnapshotData()
	if len(snap.Results) == 0 {
		return false
	}
	for fp := range snap.Results {
		if !excluded[fp] {
			return false
		}
	}
	return true
}

// ExcludeFamily marks every file of the family excluded and auto-cancels the
// job when nothing remains (legacy ExcludeFile semantics, family-scoped).
// Called under WithMovieEditLock.
func (m *LockedMovieOps) ExcludeFamily(ctx context.Context) error {
	filePaths, err := m.familyFilePaths()
	if err != nil {
		return err
	}
	env := m.pe.currentEnv()
	// Exclusion is an envelope-only mutation: no movie-row legs, but the
	// excluded map flips ride the same composite tx path when available.
	publish := func() {
		for _, fp := range filePaths {
			m.pe.updater.MarkExcluded(fp)
		}
	}
	cancelIfAll := func() bool {
		var lc *JobLifecycle
		if env != nil {
			lc = env.lifecycle
		}
		if lc == nil {
			return false
		}
		if ra, ok := m.pe.lookup.(resultstore.ResultMapAccessor); ok && ra.IsAllExcluded() {
			// codex P1-F2 follow-up: the P5-A flip marks the job Cancelled in the
			// envelope ENCODING while leaving the lifecycle transition unfinished
			// (done channel open, completedAt unset). When the marker flip left
			// status Cancelled-but-not-cancelled, Cancel() now also completes the
			// real transition rather than reading terminal-skipping the guard.
			lc.mu.RLock()
			markerFlipped := lc.Status == models.JobStatusCancelled && !lc.cancelled
			lc.mu.RUnlock()
			status := lc.GetJobStatus()
			if status == models.JobStatusPending || status == models.JobStatusRunning || markerFlipped {
				lc.Cancel()
				return true
			}
		}
		return false
	}
	if env != nil && env.committer != nil && env.envelope != nil {
		// codex P5-A: all-excluded jobs must ATOMICALLY carry the cancelled
		// status in the committed envelope — the tx's row would otherwise sit
		// as still-Pending post-cancel and restart-converts to Failed.
		// codex r33 P2: encode Cancelled in the candidate envelope WITHOUT
		// publishing it to the live lifecycle; the real transition is the
		// post-commit Cancel() below.
		err := env.committer.Commit(ctx, &EditCommitPlan{
			EnvelopeFn: func() (*models.Job, error) {
				// audit R4: capture the exclusion map INSIDE the envelope-locked
				// closure — a concurrent exclusion commit on another family holds
				// a different family key, so a pre-captured stale map would erase
				// its entry from the durable envelope.
				excludedSnap := m.excludedSnapshot(filePaths)
				terminal := allExcludedTerminal(env.lifecycle, excludedSnap, m.pe.lookup)
				row, err := env.envelope(nil, nil, excludedSnap)
				if err != nil || row == nil {
					return row, err
				}
				if terminal {
					row.Status = models.JobStatusCancelled
				}
				return row, nil
			},
			Publish: func() error { publish(); return nil },
		})
		if err != nil {
			return err
		}
		// Auto-cancel (legacy semantic) fires AFTER the exclusion commit. If
		// it transitioned the lifecycle, the envelope row above is
		// status-stale (a restart would flip pending→failed via
		// recoverOrphanedJobs) — re-persist so the durable row carries the
		// cancelled status; surface a repersist failure as an actionable 5xx
		// (retry: the op is idempotent and the second persist leaves the
		// envelope coherent).
		cancelledNow := cancelIfAll()
		wasCancelled := env.lifecycle != nil && env.lifecycle.GetJobStatus() == models.JobStatusCancelled
		// codex r32: a retry after a failed first persist sees NO new cancel
		// event (job already cancelled) — persist anyway so the durable row
		// converges with the in-memory cancelled state.
		if (cancelledNow || wasCancelled) && env.persistFn != nil {
			if err := env.persistFn(); err != nil {
				return fmt.Errorf("post-exclusion lifecycle persist: %w", err)
			}
		}
		return nil
	}
	publish()
	// Auto-cancel (legacy semantic) fires AFTER the exclusion commit. A failed
	// persistFn leaves the cancellation in-memory only; surface it so the
	// caller retries (idempotent) rather than silently de-hardening it.
	cancelled := cancelIfAll()
	if env != nil && env.persistFn != nil {
		// The legacy store's persistFn snapshots post-cancel state — but if
		// it FAILS, the cancellation never hardened on disk. Surface it so
		// the caller returns 5xx and the UI retries (exclusion+re-cancel are
		// idempotent).
		if err := env.persistFn(); err != nil {
			_ = cancelled
			return fmt.Errorf("post-exclusion persist: %w", err)
		}
	}
	return nil
}

// excludedSnapshot clones the live excluded map and marks each family path.
func (m *LockedMovieOps) excludedSnapshot(filePaths []string) map[string]bool {
	excluded := map[string]bool{}
	if ra, ok := m.pe.lookup.(resultstore.ResultMapAccessor); ok {
		for k, v := range ra.SnapshotData().Excluded {
			excluded[k] = v
		}
	}
	for _, fp := range filePaths {
		excluded[fp] = true
	}
	return excluded
}

// updateMovieSingleLocked applies a whole-movie save to ONE file only
// (legacy per-file UpdateMovie semantics), under the held family key. The
// prepare/sanitize/DB-leg ordering matches UpdateMovieFamily; candidates
// cover just the target file.
// rejectIdentityChangeLocked enforces D17's DB-PK constraint inside the
// held family key.
//
//   - Foreign-family rekey (codex r11): if the payload's movie ID names an
//     ID an EXISTING different family already occupies, reject — never fold
//     families by silently reindexing.
//   - content_id drift (codex r10/r13): empty incoming means "unchanged";
//     non-empty incoming is accepted only when it equals the stored PK or —
//     for a stored-empty row — echoes the movie's own ID. Anything else would
//     bind or create a detached primary-key row.
//
// D15's dual-key lock already covers renames; rescrape remains the
// canonical surface for identity changes.
func (m *LockedMovieOps) rejectIdentityChangeLocked(filePaths []string, movie *models.Movie) error {
	// Foreign-family rekey check FIRST — must not sit behind the per-file
	// early-return below or it becomes unreachable when a family result
	// exists (codex r15).
	// codex r28: "foreign" means paths outside the CURRENT family — when the
	// matcher alias (m.movieID) differs from the canonical movie ID the same
	// family may still be the owner of the resolved paths (seen via the
	// canonical ID). Walk each hit and confirm a real other-family binding
	// before rejecting; otherwise normal saves on the aliased shape 409.
	if movie != nil && movie.ID != "" && !strings.EqualFold(strings.TrimSpace(movie.ID), strings.TrimSpace(m.movieID)) {
		lockedNorm := strings.ToUpper(strings.TrimSpace(m.movieID))
		foreign := false
		for _, fp := range m.pe.lookup.FindFilePathsForMovieID(movie.ID) {
			o, err := m.pe.lookup.GetMovieResult(fp)
			if err != nil || o == nil {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(o.FileMatchInfo.MovieID), lockedNorm) {
				foreign = true
				break
			}
		}
		if foreign {
			return &EditAdmissionConflictError{Message: fmt.Sprintf("target movie ID %q already belongs to a different family in this job", movie.ID)}
		}
	}

	for _, fp := range filePaths {
		cur, err := m.pe.lookup.GetMovieResult(fp)
		if err != nil || cur == nil || cur.Movie == nil {
			continue
		}
		in := strings.TrimSpace(movie.ContentID)
		if in != "" {
			stored := strings.TrimSpace(cur.Movie.ContentID)
			// codex r46 P2: EXACT compare — the ContentID column is a case-
			// sensitive TEXT PRIMARY KEY: a capitalization-only "change" would
			// otherwise pass as same-row while the upsert writes a NEW row,
			// stranding associations on the old key.
			sameRow := stored != "" && in == stored
			echoOnEmptyStored := stored == "" && strings.EqualFold(in, strings.TrimSpace(cur.Movie.ID))
			if !sameRow && !echoOnEmptyStored {
				return &EditAdmissionConflictError{Message: "content_id is the movie primary key; identity changes belong to rescrape flows (HTTP 409)"}
			}
		}
		return nil // first family result is authoritative
	}
	return nil
}

func (m *LockedMovieOps) updateMovieSingleLocked(ctx context.Context, filePath string, movie *models.Movie) error {
	if movie == nil {
		return fmt.Errorf("movie is required")
	}
	if strings.TrimSpace(movie.ID) == "" {
		return &EditAdmissionConflictError{Message: "movie ID must not be empty — identity changes belong to rescrape/clear flows"}
	}
	renames := make([]ActressRenamePlan, 0, len(movie.Actresses))
	for _, a := range movie.Actresses {
		if a.ID == 0 {
			continue
		}
		renames = append(renames, ActressRenamePlan{ID: a.ID, FirstName: a.FirstName, LastName: a.LastName, JapaneseName: a.JapaneseName})
	}
	current, err := m.pe.lookup.GetMovieResult(filePath)
	if err != nil || current == nil {
		return fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, filePath)
	}
	if err := m.rejectIdentityChangeLocked([]string{filePath}, movie); err != nil {
		return err
	}
	// audit F7+R7-3: the single-save surface advances the revision exactly
	// like UpdateMovieFamily — fence it behind outstanding poster witnesses
	// for BOTH identities: the incoming ID (preserves family-mirror semantics)
	// and the STORED one (a rekey-via-single-save would otherwise skip the
	// pending-witness fence at the old identity).
	if err := posterWitnessFence(m.pe.currentEnv(), movie.ID); err != nil {
		return err
	}
	if current.Movie != nil && !strings.EqualFold(strings.TrimSpace(current.Movie.ID), strings.TrimSpace(movie.ID)) {
		if err := posterWitnessFence(m.pe.currentEnv(), current.Movie.ID); err != nil {
			return err
		}
	}
	backupCoverOriginal(current.Movie, movie)
	var have bool
	var curPosterURL, curCoverURL string
	var curShouldCrop bool
	if current.Movie != nil {
		have = true
		curPosterURL = current.Movie.Poster.PosterURL
		curCoverURL = current.Movie.Poster.CoverURL
		curShouldCrop = current.Movie.Poster.ShouldCropPoster
	}
	sanitizePosterCropGeometry(movie, have, curPosterURL, curCoverURL, curShouldCrop)
	cand := current.Clone()
	cand.Movie = movie // alias the live pointer: post-Upsert normalization must flow to envelope + publish
	retainMovieAlias(cand, movie.ID)
	cand.Revision = current.Revision + 1
	return m.commitCandidate(ctx, map[string]*resultstore.MovieResult{filePath: cand}, nil, func(plan *EditCommitPlan) {
		plan.UpsertMovie = movie
		plan.Renames = renames
	})
}

// --- Public wrappers (thin: acquire the family key, delegate to cores) ---

// UpdatePosterCrop updates the cropped poster URL and the manual crop geometry.
func (pe *PosterEditor) UpdatePosterCrop(movieID string, croppedURL string, bounds *models.CropBounds, sourceFull bool) error {
	return pe.WithMovieEditLock(movieID, func(m *LockedMovieOps) error {
		return m.UpdatePosterCrop(croppedURL, bounds, sourceFull)
	})
}

// UpdatePosterFromURL updates the poster/cropped poster URL for the family,
// persisting the movie row change when a DB seam is wired.
func (pe *PosterEditor) UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error {
	return pe.WithMovieEditLock(movieID, func(m *LockedMovieOps) error {
		return m.UpdatePosterFromURL(ctx, posterURL, croppedURL)
	})
}

// FamilySaveOptions carries the soft guards on a family save: the
// omitted-bounds carry (geometry re-read under the family key) and an
// optional result-revision CAS (D12: stale clients 409 instead of clobbering
// a newer committed edit).
type FamilySaveOptions struct {
	CarryCropGeometry      bool
	ExpectedResultRevision *uint64
	// ExpectedResultRevisions — multipart CAS: EVERY listed result must be at
	// the mapped revision or the save 409s before ANY write (codex r39).
	ExpectedResultRevisions map[string]uint64
}

// UpdateMovieFamily saves a whole-movie review edit for every file of the
// family under one acquisition (D1 family op). A PATCH that CHANGES the
// movie ID rekeys the family: the old AND new keys are held atomically in
// lexical order (D1 dual-key rule) so a concurrent op on either identity
// can never interleave a partially-published rekey (codex P3-B).
func (pe *PosterEditor) UpdateMovieFamily(ctx context.Context, movieID, resultID string, movie *models.Movie, opts FamilySaveOptions) error {
	_, _, err := pe.UpdateMovieFamilyWithEcho(ctx, movieID, resultID, movie, opts)
	return err
}

// UpdateMovieFamilyWithEcho is UpdateMovieFamily plus the CAS echo — the
// revisions are captured post-commit INSIDE the keyed section (audit
// F-R15-1), so an echo never reflects a racer's commit landing in the
// release→read gap.
func (pe *PosterEditor) UpdateMovieFamilyWithEcho(ctx context.Context, movieID, resultID string, movie *models.Movie, opts FamilySaveOptions) (*uint64, map[string]uint64, error) {
	var echoRev *uint64
	var echoFam map[string]uint64
	err := pe.withKeyedSection(movieID, movie, func(m *LockedMovieOps) error {
		// codex cloud P1: rekey race — a concurrent rescrape can move the
		// target result to another family BETWEEN the handler's pre-key
		// resolution and this acquisition; without CAS nothing re-checked the
		// mapping. Refuse to commit to the old family's siblings while the
		// target lives elsewhere (regardless of CAS being supplied).
		if cur, _, ok := m.pe.lookup.GetFileResultByResultID(resultID); ok && cur != nil {
			curFam := strings.TrimSpace(cur.FileMatchInfo.MovieID)
			if curFam == "" && cur.Movie != nil {
				curFam = strings.TrimSpace(cur.Movie.ID)
			}
			if curFam != "" && !strings.EqualFold(curFam, strings.TrimSpace(movieID)) {
				return &EditAdmissionConflictError{Message: fmt.Sprintf("result %s moved to family %s during save; retry", resultID, curFam)}
			}
		}
		if opts.ExpectedResultRevision != nil {
			if cur, _, ok := m.pe.lookup.GetFileResultByResultID(resultID); ok && cur.Revision != *opts.ExpectedResultRevision {
				return &EditAdmissionConflictError{Message: fmt.Sprintf("result revision stale: expected %d, found %d", *opts.ExpectedResultRevision, cur.Revision)}
			}
		}
		// codex r39: multipart CAS — every listed part's revision must match,
		// or the save 409s before ANY candidate write.
		for rid, expected := range opts.ExpectedResultRevisions {
			cur, _, ok := m.pe.lookup.GetFileResultByResultID(rid)
			if !ok || cur == nil {
				return &EditAdmissionConflictError{Message: fmt.Sprintf("result %s vanished for CAS check", rid)}
			}
			if cur.Revision != expected {
				return &EditAdmissionConflictError{Message: fmt.Sprintf("result %s revision stale: expected %d, found %d", rid, expected, cur.Revision)}
			}
		}
		// codex cloud P1: membership drift — CAS covers only the result IDs the
		// caller listed. A concurrent rescrape can JOIN another result into this
		// family after the client's snapshot; the commit would then overwrite a
		// movie the client never saw. When any revision context was supplied,
		// every current member must be accounted for (guarded set = target ∪
		// map keys); callers without revision context keep documented LWW.
		if opts.ExpectedResultRevision != nil || len(opts.ExpectedResultRevisions) > 0 {
			// codex cloud P2 (@poster_editor): a multipart map must AUTH its target —
			// letting the target ride into the guard set unvalidated means a stale
			// target part is never CAS-checked.
			if len(opts.ExpectedResultRevisions) > 0 {
				if _, ok := opts.ExpectedResultRevisions[resultID]; !ok {
					return &EditAdmissionConflictError{Message: "multipart expected_revisions omits the target result — no part goes unverified"}
				}
			}
			guardSet := make(map[string]struct{}, len(opts.ExpectedResultRevisions)+1)
			guardSet[resultID] = struct{}{}
			for rid := range opts.ExpectedResultRevisions {
				guardSet[rid] = struct{}{}
			}
			for _, fp := range m.pe.lookup.FindFilePathsForMovieID(strings.TrimSpace(movieID)) {
				cur, gerr := m.pe.lookup.GetMovieResult(fp)
				if gerr != nil || cur == nil || cur.ResultID == "" {
					continue // unreadable or ID-less: not committable membership evidence
				}
				if _, ok := guardSet[cur.ResultID]; !ok {
					return &EditAdmissionConflictError{Message: fmt.Sprintf("result set of family %s changed during save (unexpected member %s); retry", movieID, cur.ResultID)}
				}
			}
		}
		if opts.CarryCropGeometry && movie != nil && movie.Poster.PosterCropBounds == nil {
			// Revalidate the omitted-bounds carry INSIDE the locked section
			// (R29/D1): read the CURRENT stored geometry from the target
			// result here — never the handler's pre-lock read.
			if cur, _, ok := m.pe.lookup.GetFileResultByResultID(resultID); ok && cur.Movie != nil {
				if stored := cur.Movie.Poster.PosterCropBounds; stored != nil {
					b := *stored
					movie.Poster.PosterCropBounds = &b
					movie.Poster.PosterCropSourceFull = cur.Movie.Poster.PosterCropSourceFull
				}
			}
		}
		if err := m.UpdateMovieFamily(ctx, movie); err != nil {
			return err
		}
		// in-key echo capture (audit F-R15-1): result + folded-family map at
		// their post-commit values — never a racer that already landed.
		if stored, _, ok := m.pe.lookup.GetFileResultByResultID(resultID); ok && stored != nil {
			rv := stored.Revision
			echoRev = &rv
			famID := strings.TrimSpace(stored.FileMatchInfo.MovieID)
			if famID == "" && stored.Movie != nil {
				famID = strings.TrimSpace(stored.Movie.ID)
			}
			fam := map[string]uint64{}
			for _, r2 := range m.pe.lookup.GetMovieResultsForMovieID(famID) {
				if r2 != nil && r2.ResultID != "" {
					fam[r2.ResultID] = r2.Revision
				}
			}
			echoFam = fam
		}
		return nil
	})
	return echoRev, echoFam, err
}

// withKeyedSection acquires the family key — plus the incoming movie's new
// identity key when the edit rekeys the family — in lexical order, then runs
// fn under the held keys.
// lockKeysFor computes the full D15 key set for a family edit: the family
// matcher ID, the incoming movie ID (rekey), and the movie-row identity
// (stored + incoming ContentID) — movie rows are PK'd by ContentID, and
// cross-job edits share the registry, so ALL identity surfaces contend on
// the same keys (codex r13 P1-B).
func (pe *PosterEditor) lockKeysFor(movieID string, movie *models.Movie) []string {
	keys := []string{movieID}
	if movie != nil {
		if movie.ID != "" && !strings.EqualFold(strings.TrimSpace(movie.ID), strings.TrimSpace(movieID)) {
			keys = append(keys, movie.ID)
		}
		if c := strings.TrimSpace(movie.ContentID); c != "" {
			keys = append(keys, "cid:"+c)
		}
	}
	// Stored content-id of the current family — the row we're about to write.
	if res, _ := pe.lookup.FindMovieResultForMovieID(movieID); res != nil && res.Movie != nil {
		if c := strings.TrimSpace(res.Movie.ContentID); c != "" {
			keys = append(keys, "cid:"+c)
		}
		// audit F-R12-1: the STORED canonical movie ID keys the same byte set a
		// rescrape's key-held generation window names — omitting it let a rekey
		// PATCH steal an in-flight rescrape's bytes onto the committed row
		// (alias≠canonical families make this live). identityKeysFor /
		// commitKeys already include it; dedup folds repetition in AcquireMany.
		if id := strings.TrimSpace(res.Movie.ID); id != "" && !strings.EqualFold(id, strings.TrimSpace(movieID)) {
			keys = append(keys, id)
		}
	}
	return keys
}

func (pe *PosterEditor) withKeyedSection(movieID string, movie *models.Movie, fn func(m *LockedMovieOps) error) error {
	reg := pe.lockRegistry()
	keys := pe.lockKeysFor(movieID, movie)
	if len(keys) == 1 {
		release := reg.Acquire(movieID)
		defer release()
		return fn(&LockedMovieOps{pe: pe, movieID: movieID})
	}
	release := reg.AcquireMany(keys)
	defer release()
	return fn(&LockedMovieOps{pe: pe, movieID: movieID})
}

// UpdateMovieSingle saves a whole-movie edit to ONE file, keyed by that
// file's resolved family (plus the incoming ID's key on a rekey).
func (pe *PosterEditor) UpdateMovieSingle(ctx context.Context, filePath string, movie *models.Movie) error {
	movieID := ""
	if res, err := pe.lookup.GetMovieResult(filePath); err == nil && res != nil {
		movieID = res.FileMatchInfo.MovieID
	}
	if movieID == "" {
		movieID = filePath
	}
	return pe.withKeyedSection(movieID, movie, func(m *LockedMovieOps) error {
		return m.updateMovieSingleLocked(ctx, filePath, movie)
	})
}

// ExcludeMovieFamily excludes every file of the family as one admitted
// editor operation (D1/D16: handlers gate on the admission lease first).
func (pe *PosterEditor) ExcludeMovieFamily(ctx context.Context, movieID string) error {
	return pe.WithMovieEditLock(movieID, func(m *LockedMovieOps) error {
		return m.ExcludeFamily(ctx)
	})
}

// ApplyFieldOverride applies a source cherry-pick under the family key.
// Retries on ErrFamilyRekeyed (bounded): a concurrent PATCH can rekey the
// result between the caller's pre-lock resolution and the lock section;
// re-resolution and re-acquisition under the CURRENT key converges.
func (pe *PosterEditor) ApplyFieldOverride(ctx context.Context, resultID, movieID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
	out, prov, _, err := pe.ApplyFieldOverrideWithRevisions(ctx, resultID, movieID, fieldKey, source)
	return out, prov, err
}

// ApplyFieldOverrideWithRevisions returns the override result and the family
// revision snapshot captured while the same keyed section is still held.
func (pe *PosterEditor) ApplyFieldOverrideWithRevisions(ctx context.Context, resultID, movieID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, map[string]uint64, error) {
	// codex r29: lock on the matcher alias AND the result's canonical Movie.ID
	// where they differ — rescrape commits pair-lock on those identities, and
	// a family-matched edit must collide with them.
	key := movieID
	if key == "" {
		key = resultID
	}
	var out *resultstore.MovieResult
	var prov *resultstore.ProvenanceData
	var revisions map[string]uint64
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		keySet := []string{key}
		if res, _, found := pe.lookup.GetFileResultByResultID(resultID); found && res != nil && res.Movie != nil {
			if res.Movie.ID != "" && !strings.EqualFold(strings.TrimSpace(res.Movie.ID), strings.TrimSpace(key)) {
				keySet = append(keySet, res.Movie.ID)
			}
			// codex r43 P2c: overrides upsert the SAME primary-key movie row as
			// whole-movie PATCHes / rescrape commits, which fold the stored
			// content-id ("cid:") into their key sets — without it two jobs
			// sharing a ContentID (different matcher IDs) acquire disjoint
			// locks and race competing transactions.
			if c := strings.TrimSpace(res.Movie.ContentID); c != "" {
				keySet = append(keySet, "cid:"+c)
			}
		}
		reg := pe.lockRegistry()
		release := reg.AcquireMany(keySet)
		err = func(innerErr error) error { return innerErr }(func() error {
			defer release()
			var e error
			out, prov, revisions, e = (&LockedMovieOps{pe: pe, movieID: key}).ApplyFieldOverrideWithRevisions(ctx, resultID, fieldKey, source)
			return e
		}())
		if !errors.Is(err, ErrFamilyRekeyed) {
			return out, prov, revisions, err
		}
		if res, _, found := pe.lookup.GetFileResultByResultID(resultID); found && res != nil && res.FileMatchInfo.MovieID != "" {
			key = res.FileMatchInfo.MovieID
		} else {
			return nil, nil, nil, fmt.Errorf("%w: %s", ErrMovieFamilyEmpty, resultID)
		}
	}
	return nil, nil, nil, fmt.Errorf("%w (retry budget exhausted): %s", ErrFamilyRekeyed, resultID)
}

// EditAdmissionConflictError marks identity-rejection edits (D17) — the API
// layer maps it to 409 Conflict.
type EditAdmissionConflictError struct {
	JobID   string
	Message string
}

func (e *EditAdmissionConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "edit conflicts with job state"
}

// clearPosterCropGeometry drops persisted manual crop geometry from m.
// Called at every flow that replaces the poster source or crop intent so a
// stale crop can never be applied to a different image.
func clearPosterCropGeometry(m *models.Movie) {
	if m == nil {
		return
	}
	m.Poster.PosterCropBounds = nil
	m.Poster.PosterCropSourceFull = false
}

// sanitizePosterCropGeometry enforces the manual-crop invalidation contract
// when a whole movie is stored (UpdateMovie): carried geometry survives only
// if it is valid AND the movie's EFFECTIVE poster source (poster_url, falling
// back to cover_url — the same selection the downloader and the apply boundary
// use) and crop intent are unchanged. A fanart-only edit (cover_url changes
// while poster_url selects the source) must not discard a still-valid crop.
// A nil next-bounds means "no geometry" (the batch PATCH handler resolves
// omitted-vs-explicit-null upstream) — only normalize the flag in that case.
func sanitizePosterCropGeometry(next *models.Movie, haveCurrent bool, curPosterURL, curCoverURL string, curShouldCrop bool) {
	if next == nil {
		return
	}
	if next.Poster.PosterCropBounds == nil {
		next.Poster.PosterCropSourceFull = false
		return
	}
	if !next.Poster.PosterCropBounds.Valid() {
		clearPosterCropGeometry(next)
		return
	}
	if !haveCurrent {
		return
	}
	if effectivePosterSourceOf(next.Poster.PosterURL, next.Poster.CoverURL) != effectivePosterSourceOf(curPosterURL, curCoverURL) ||
		next.Poster.ShouldCropPoster != curShouldCrop {
		clearPosterCropGeometry(next)
	}
}

// effectivePosterSourceOf mirrors the downloader's poster source selection:
// poster_url when present, otherwise cover_url.
func effectivePosterSourceOf(posterURL, coverURL string) string {
	if posterURL != "" {
		return posterURL
	}
	return coverURL
}

// backupPosterOriginals preserves the original poster URLs before they are overwritten.
//
// The sentinel for "baseline already captured" is EITHER field present:
// — OriginalPosterURL non-empty covers legacy envelopes (URL-only backups
// predate the eager baseline) — while OriginalShouldCropPoster non-nil covers
// cover-fallback movies whose baseline legitimately has an empty poster URL;
// URL-only sentinel would re-snapshot on the SECOND crop of such a movie and
// store the first manual crop as the "original".
func backupPosterOriginals(movie *models.Movie) {
	if movie.Poster.OriginalPosterURL == "" && movie.Poster.OriginalShouldCropPoster == nil {
		shouldCrop := movie.Poster.ShouldCropPoster
		movie.Poster.OriginalPosterURL = movie.Poster.PosterURL
		movie.Poster.OriginalCroppedPosterURL = movie.Poster.CroppedPosterURL
		movie.Poster.OriginalShouldCropPoster = &shouldCrop
	}
}

// backupCoverOriginal preserves the original cover URL so the cover/fanart
// reset survives server restarts. The existing movie (current) holds the
// authoritative original snapshot; the incoming movie (next) is what the
// client wants to persist. If an original was already captured on the
// existing movie, carry it forward. Otherwise, if the cover is changing,
// snapshot the existing cover as the original.
func backupCoverOriginal(current, next *models.Movie) {
	if current == nil || next == nil {
		return
	}
	if orig := current.Poster.OriginalCoverURL; orig != "" {
		next.Poster.OriginalCoverURL = orig
		return
	}
	if current.Poster.CoverURL != "" && current.Poster.CoverURL != next.Poster.CoverURL {
		next.Poster.OriginalCoverURL = current.Poster.CoverURL
	}
}

// establishScrapedBaseline sets the poster-original revert group on target
// from source's current poster fields, establishing the scraper's value as
// the Reset baseline. Called by both the initial scrape phase and the
// rescrape phase (merge + non-merge paths) so the review UI's Reset always
// returns to what the scraper produced — never a stale prior-content value
// carried across a content-id change. The baseline may legitimately be empty
// when the scraper found no image; the frontend falls back to the current
// field, so an empty baseline makes Reset a no-op rather than wiping a valid
// image.
//
// URL fields are trimmed so the baseline matches the display field's
// trimming in mergeRescrapeMovie (a whitespace-only scraper value should
// not become a non-empty baseline that falsely enables the Reset button).
//
// This is the eager counterpart to backupPosterOriginals: backupPosterOriginals
// snapshots the pre-edit state lazily on the first manual edit, while
// establishScrapedBaseline snapshots the scraped state eagerly at scrape time.
// Mirrors backupPosterOriginals' field grouping (PosterURL/CroppedPosterURL/
// ShouldCropPoster) and extends it to CoverURL, which the lazy backup handles
// separately via backupCoverOriginal.
func establishScrapedBaseline(target, source *models.Movie) {
	if target == nil || source == nil {
		return
	}
	posterURL := strings.TrimSpace(source.Poster.PosterURL)
	croppedURL := strings.TrimSpace(source.Poster.CroppedPosterURL)
	target.Poster.OriginalPosterURL = posterURL
	target.Poster.OriginalCroppedPosterURL = croppedURL
	// Only anchor the crop baseline when there's a real poster baseline. When
	// the scraper found no image, leave OriginalShouldCropPoster nil so the
	// frontend falls back to the current field (matching the empty-URL
	// fallback) instead of a non-nil false that could spuriously enable Reset.
	if posterURL != "" || croppedURL != "" {
		shouldCrop := source.Poster.ShouldCropPoster
		target.Poster.OriginalShouldCropPoster = &shouldCrop
	} else {
		target.Poster.OriginalShouldCropPoster = nil
	}
	target.Poster.OriginalCoverURL = strings.TrimSpace(source.Poster.CoverURL)
}

// retainMovieAlias writes the canonical movie ID over the matcher alias ONLY
// when a canonical ID exists (codex P3 R17-2): committing an empty ID over a
// legacy row would erase the alias the eviction-witness recovery arbitrates
// on (uncommitted classification → witness deleted with the pair orphaned).
func retainMovieAlias(cand *resultstore.MovieResult, movieID string) {
	if strings.TrimSpace(movieID) != "" {
		cand.FileMatchInfo.MovieID = movieID
	}
}
