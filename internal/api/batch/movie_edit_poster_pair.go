// movie_edit_poster_pair.go — poster preview-pair lifecycle & identity helpers,
// extracted from movie_edit.go to keep that handler file under the 700-line
// internal/api size guardrail. Concern: full/cropped poster file bytes plus
// result-identity resolution around staged promote/rollback (D4 durable-bytes).
package batch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// posterPairBackup snapshots the temp poster pair (<posterID>.jpg and
// <posterID>-full.jpg) so a failed commit can restore the previous bytes
// (POSTER-WRITE-HARDENING D4 applies to served asset bytes too — codex P4-B).
// Plain os ops: the poster manager writes these paths via OsFs in production
// and the crop tests exercise them through the test chdir trick.
type posterPairBackup struct {
	fs            afero.Fs
	dir           string
	fullPath      string
	croppedPath   string
	fullBytes     []byte
	croppedBytes  []byte
	fullExisted   bool
	croppedExists bool

	// unreadable marks files that exist but could not be snapshotted (perm /
	// I/O errors). Restore NEVER deletes them (codex r12): remove-if-absent
	// semantics apply only to files that were genuinely absent pre-op.
	fullUnreadable    bool
	croppedUnreadable bool
}

// fs must be the same afero.Fs the PosterManager writes through (codex
// P9-A: a host-os os.Open reads nothing when an injected fs backs the
// manager); callers pass rt.Deps().GetFs().
func backupPosterPair(fs afero.Fs, tempDir, jobID, posterID string) *posterPairBackup {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	b := &posterPairBackup{
		fs:          fs,
		dir:         filepath.Join(tempDir, "posters", jobID),
		fullPath:    filepath.Join(tempDir, "posters", jobID, fmt.Sprintf("%s-full.jpg", posterID)),
		croppedPath: filepath.Join(tempDir, "posters", jobID, fmt.Sprintf("%s.jpg", posterID)),
	}
	fs = b.fs
	if data, err := afero.ReadFile(fs, b.fullPath); err == nil {
		b.fullBytes = data
		b.fullExisted = true
	} else if !os.IsNotExist(err) {
		b.fullUnreadable = true
		logging.Warnf("poster rollback: %s unreadable (%v) — restore will leave it untouched", b.fullPath, err)
	}
	if data, err := afero.ReadFile(fs, b.croppedPath); err == nil {
		b.croppedBytes = data
		b.croppedExists = true
	} else if !os.IsNotExist(err) {
		b.croppedUnreadable = true
		logging.Warnf("poster rollback: %s unreadable (%v) — restore will leave it untouched", b.croppedPath, err)
	}
	return b
}

// restore rewinds the two poster files to their pre-op bytes: existing files
// are rewritten, previously-absent ones are removed. Reports TRUE only when
// every required leg succeeded (codex r48-followup P2): callers must not
// reap the .bak parking or the recovery witness on a partial restore — the
// startup reconciler retries from those markers.
func (b *posterPairBackup) restore() bool {
	complete := true
	if !b.fullExisted && !b.fullUnreadable {
		if err := b.fs.Remove(b.fullPath); err != nil && !os.IsNotExist(err) {
			complete = false
			logging.Warnf("poster rollback: remove %s: %v", b.fullPath, err)
		}
	} else if b.fullExisted {
		if err := afero.WriteFile(b.fs, b.fullPath, b.fullBytes, 0o644); err != nil {
			complete = false
			logging.Warnf("poster rollback: restore %s: %v", b.fullPath, err)
		}
	} else {
		complete = false // unreadable bytes can never be restored
	}
	if !b.croppedExists && !b.croppedUnreadable {
		if err := b.fs.Remove(b.croppedPath); err != nil && !os.IsNotExist(err) {
			complete = false
			logging.Warnf("poster rollback: remove %s: %v", b.croppedPath, err)
		}
	} else if b.croppedExists {
		if err := afero.WriteFile(b.fs, b.croppedPath, b.croppedBytes, 0o644); err != nil {
			complete = false
			logging.Warnf("poster rollback: restore %s: %v", b.croppedPath, err)
		}
	} else {
		complete = false
	}
	return complete
}

// promoteStagedPosterPair atomically renames the staged poster files into
// the canonical <posterID> names (codex r18): callers run this inside the
// family key; a backupPosterPair taken just before covers commit-failure
// rollback.
// promoteStagedPosterPair relocates the staged poster files into the
// canonical <posterID> names and returns `finalize`; callers MUST run
// finalize only AFTER the state commit lands (codex r22: .bak rotation
// survives until the commit witness, so a crash can be reconciled).
func promoteStagedPosterPair(fs afero.Fs, tempDir, jobID, stageID, posterID string) (finalize func(), err error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	srcs := []struct{ src, dst string }{
		{filepath.Join(dir, stageID+"-full.jpg"), filepath.Join(dir, posterID+"-full.jpg")},
		{filepath.Join(dir, stageID+".jpg"), filepath.Join(dir, posterID+".jpg")},
	}
	// Promote: park canonical → staged-rename; .bak files persist until the
	// caller's finalize runs at the commit witness. Mid-promote failure
	// reverses whatever was moved (unpark + un-promote) so a partial error
	// leaves the canonical pair untouched and no .bak litter (codex r19+r28).
	var parked []string
	var promoted []string
	rollbackPromote := func() {
		// un-promote the already-installed new bytes (they were never committed)
		for i := len(promoted) - 1; i >= 0; i-- {
			if rbErr := fs.Remove(promoted[i]); rbErr != nil && !os.IsNotExist(rbErr) {
				logging.Warnf("poster promote unpromote %s: %v", promoted[i], rbErr)
			}
		}
		for _, bak := range parked {
			orig := strings.TrimSuffix(bak, ".bak")
			if rbErr := fs.Rename(bak, orig); rbErr != nil {
				logging.Warnf("poster promote un->park %s: %v", bak, rbErr)
			}
		}
	}
	for _, mv := range srcs {
		if _, err := fs.Stat(mv.src); err != nil {
			if os.IsNotExist(err) {
				continue // manager may not have produced this leg
			}
			rollbackPromote()
			return nil, err
		}
		bak := mv.dst + ".bak"
		_ = fs.Remove(bak)
		if _, err := fs.Stat(mv.dst); err == nil {
			if err := fs.Rename(mv.dst, bak); err != nil {
				rollbackPromote()
				return nil, fmt.Errorf("park previous poster %s: %w", mv.dst, err)
			}
			parked = append(parked, bak)
		} else if !os.IsNotExist(err) {
			// codex r51 P2: only ABSENCE permits the rename — on overwrite-
			// replacing filesystems a transient stat error otherwise skips the
			// park and the rename destroys the old bytes with no .bak.
			rollbackPromote()
			return nil, fmt.Errorf("promote target stat %s: %w", mv.dst, err)
		}
		if err := fs.Rename(mv.src, mv.dst); err != nil {
			rollbackPromote()
			return nil, fmt.Errorf("promote staged poster %s: %w", mv.src, err)
		}
		promoted = append(promoted, mv.dst)
	}
	return func() {
		for _, bak := range parked {
			if err := fs.Remove(bak); err != nil && !os.IsNotExist(err) {
				logging.Warnf("poster promote finalize %s: %v", bak, err)
			}
		}
	}, nil
}

// promoteWitness is the recovery record for a staged-pair promotion that
// crashed AFTER promote but BEFORE the state commit (codex r48 P2): the
// canonical names hold uncommitted new bytes, the previous pair exists only
// as .bak, and the durable row still describes the old poster. The wire
// format is read by worker.TempDirCleaner.ReconcileRekeyWitnesses.
const promoteWitnessPrefix = ".promote-"

type promoteWitness struct {
	PosterID string `json:"poster_id"`
	URL      string `json:"url"`
	// ResultID pins the arbitration to the TARGET result — URL-global matching
	// misfires when another family legitimately shares the URL.
	ResultID string `json:"result_id"`
	// PrevRevision is the row's revision captured pre-op: the commit is
	// provably durable only when the row's revision MOVED past it (codex r49
	// P2 — same-URL refreshes can't be told apart without a commit token).
	PrevRevision uint64 `json:"prev_revision"`
	// OldSHA are the pre-promotion content hashes per leg ("full"/"crop");
	// absent key ⇒ the canonical had NO existing bytes. Hash-matched canon =
	// already-restored, mismatched canon = uncommitted new bytes — no
	// cross-retry bookkeeping that itself needs atomic persistence (r49 P2b).
	OldSHA map[string]string `json:"old_sha,omitempty"`
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// promoteWitnessHashes captures the pre-op content identities from the
// already-snapshotted posterPairBackup.
func promoteWitnessHashes(b *posterPairBackup) map[string]string {
	m := map[string]string{}
	if b == nil {
		return m
	}
	if b.fullExisted {
		m["full"] = sha256Hex(b.fullBytes)
	}
	if b.croppedExists {
		m["crop"] = sha256Hex(b.croppedBytes)
	}
	return m
}

// mustMarshal serializes simple witness structs. These types contain only
// strings, uint64s, and maps -- json.Marshal cannot fail on them, so the
// error path is eliminated entirely.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// promoteWitnessName keeps the filename inside the job dir AND collision-free
// (codex r50 P2): PathEscape is injective — "A.B" stays distinct from "A_B",
// and traversal attempts become opaque %2F paths under the job dir (the
// reconciler parses the poster ID from the CONTENT, never the filename).
func promoteWitnessName(posterID string) string {
	return promoteWitnessPrefix + url.PathEscape(posterID) + ".json"
}

// errPromoteWitnessPending marks a retry against an UNRESOLVED promote
// witness (a prior op left .bak + witness for the startup reconciler) —
// the handler maps it to HTTP 409 (codex r51 P2).
var errPromoteWitnessPending = errors.New("promote witness outstanding")

// writePromoteWitnessGuarded refuses to overwrite an outstanding witness:
// the prior operation might have restored SOME legs already — re-snapshotting
// the half-restored pair as "old" would corrupt the reconciliation baseline.
func writePromoteWitnessGuarded(fs afero.Fs, tempDir, jobID, posterID, srcURL, resultID string, prevRevision uint64, backup *posterPairBackup) (string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	// audit F2: a leg that EXISTS but is unreadable never reaches OldSHA —
	// the reconciler would later misread "no key" as "no pre-op bytes" and
	// delete a canon the failed promote never touched (killing manual crops).
	// Refuse to witness such a promote at all.
	if backup != nil && (backup.fullUnreadable || backup.croppedUnreadable) {
		return "", fmt.Errorf("poster pair unreadable at backup (full=%v crop=%v) — refusing to witness an unrecoverable promote; retry when the files are readable", backup.fullUnreadable, backup.croppedUnreadable)
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	p := filepath.Join(dir, promoteWitnessName(posterID))
	// codex cloud P1 (case-fold probes): an exact-name Stat misses a pending
	// witness written under a case-variant spelling of this poster. Content
	// scan (with a name-fold fallback for legacy empty payloads) fences by
	// identity, not by byte-spelling; read errors still fail closed.
	if pwErr := promoteWitnessConflict(fs, dir, posterID); pwErr != nil {
		if errors.Is(pwErr, errPromoteWitnessPending) {
			return "", fmt.Errorf("%w for %s — restart to reconcile before retrying", errPromoteWitnessPending, posterID)
		}
		return "", fmt.Errorf("promote witness check %s: %w", p, pwErr)
	}
	// codex P2: an unresolved REKEY witness (.rekey-<rawID>.json, matching the
	// worker writer) means one poster leg may live under another ID; a
	// download recreating the old-ID leg beside the stranded new one would
	// corrupt the next startup's rekey reconciliation.
	if hit, serr := rekeyWitnessIDsFor(fs, dir, posterID); serr != nil {
		return "", fmt.Errorf("rekey witness check: %w", serr)
	} else if hit {
		return "", fmt.Errorf("%w for %s (rekey witness) — restart to reconcile before retrying", errPromoteWitnessPending, posterID)
	}
	if parked, perr := parkedBackupConflictFor(fs, dir, posterID); perr != nil {
		return "", perr
	} else if parked {
		return "", fmt.Errorf("%w for %s (in-flight rescrape) — retry after it completes", errPromoteWitnessPending, posterID)
	}
	// codex P2: an unresolved CROP witness also fences the download. When the
	// crop committed but its promote exhausted retries, promoting new bytes
	// at the same canonical URL makes startup reconciliation misclassify the
	// older crop as committed and promote its STALE staged bytes over the new
	// poster.
	if cropName, cerr := cropWitnessConflict(fs, dir, posterID); cerr != nil {
		return "", cerr
	} else if cropName != "" {
		return "", fmt.Errorf("%w for %s (crop witness %s) — restart to reconcile before retrying", errPromoteWitnessPending, posterID, cropName)
	}
	// codex cloud P2 (@snFs): same for the from-URL download admission — a
	// retained eviction witness means canon content is undecidable-vs-durable.
	if pending, perr := pendingEvictFromDir(fs, dir, posterID); perr != nil {
		return "", fmt.Errorf("eviction witness check %s: %w", posterID, perr)
	} else if pending {
		return "", fmt.Errorf("%w for %s (eviction witness) — restart to reconcile before retrying", errPromoteWitnessPending, posterID)
	}
	return writePromoteWitness(fs, tempDir, jobID, posterID, srcURL, resultID, prevRevision, backup)
}

func writePromoteWitness(fs afero.Fs, tempDir, jobID, posterID, srcURL, resultID string, prevRevision uint64, backup *posterPairBackup) (string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	p := filepath.Join(dir, promoteWitnessName(posterID))
	payload := mustMarshal(promoteWitness{PosterID: posterID, URL: srcURL, ResultID: resultID, PrevRevision: prevRevision, OldSHA: promoteWitnessHashes(backup)})
	// codex r53 P2: atomic write via temp+rename so a partial write never
	// leaves truncated JSON at the final path (which would permanently
	// block retries via the guarded check).
	tmp := p + ".tmp"
	if err := afero.WriteFile(fs, tmp, payload, 0o644); err != nil {
		return "", fmt.Errorf("promote witness write %s: %w", tmp, err)
	}
	if err := fs.Rename(tmp, p); err != nil {
		_ = fs.Remove(tmp)
		return "", fmt.Errorf("promote witness rename %s: %w", p, err)
	}
	return p, nil
}

func removePromoteWitness(fs afero.Fs, p string) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	// codex cloud P2: retry the sweep of a COMMITTED witness — a transient wedge
	// stranded it once, and every family fence poisoned poster edits till restart.
	if err := removeWithRetry(fs, p); err != nil {
		logging.Warnf("promote witness sweep %s: %v", p, err)
	}
}

// cropWitness is the crash-recovery record for the staged manual-crop flow
// (codex r51 P2): the manager writes preview bytes to a STAGE name first;
// only AFTER the state commit lands does promotion move the bytes over the
// canonical crop. A crash mid-way leaves either an untouched canonical
// (pre-commit — nothing to repair) or committed-state + staged leftovers
// (the startup reconciler completes the promote). Wire format is read by
// worker.TempDirCleaner.
const cropWitnessPrefix = ".crop-"

type cropWitness struct {
	PosterID     string `json:"poster_id"`
	ResultID     string `json:"result_id"`
	StageID      string `json:"stage_id"`
	CroppedURL   string `json:"cropped_url"`
	PrevRevision uint64 `json:"prev_revision"`
}

func cropWitnessName(stageID string) string {
	return cropWitnessPrefix + url.PathEscape(stageID) + ".json"
}

// rekeyWitnessIDsFor matches pending rekey witnesses by CONTENT (audit
// F-R6-1 mirrors the worker-side helper): a transition touches BOTH
// identities, so a fence probing only the OLD-spelled filename leaves the
// NEW side unprotected. Scan IO errors fail closed; corrupt payloads skip.
func rekeyWitnessIDsFor(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("rekey witness scan %s: %w", dir, err)
	}
	var w struct {
		OldID string `json:"old_id"`
		NewID string `json:"new_id"`
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".rekey-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return false, fmt.Errorf("rekey witness scan %s: %w", name, rerr)
		}
		w.OldID, w.NewID = "", ""
		if json.Unmarshal(data, &w) != nil {
			continue
		}
		if strings.EqualFold(w.OldID, posterID) || strings.EqualFold(w.NewID, posterID) {
			return true, nil
		}
	}
	return false, nil
}

// hexLowerHexTail mirrors the worker's anchored marker tail check (audit
// F-R20-2, batch side): ".<lowhex>.<lowhex>".
func hexLowerHexTail(s string) bool {
	i1 := strings.LastIndexByte(s, '.')
	if i1 < 2 || i1 == len(s)-1 {
		return false
	}
	i0 := strings.LastIndexByte(s[:i1], '.')
	if i0 < 1 {
		return false
	}
	for _, part := range []string{s[i0+1 : i1], s[i1+1:]} {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
				return false
			}
		}
	}
	return true
}

// markerAnchoredBatch matches the in-flight sentinel shape only when its
// tail carries the hex.hex nonce — plain-canonical ".inflight-*" names are
// never markers. Both ends of the PREFIX match compare lowercase (codex P2):
// a probe with ID "ABC-1" must hit a marker written by a variant "abc-1" —
// same bytes on a case-insensitive filesystem.
func markerAnchoredBatch(name, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) &&
		len(name) > len(prefix) && hexLowerHexTail(name)
}

// parkedBackupConflictFor reports whether a rescrape's parked backup legs
// (probe suffix .rsbak.) exist for posterID — admission signals for the
// download/crop guards (audit F-R9-2: an in-flight rescrape's losing closeout
// can restore parked litter OVER freshly committed review bytes). Scan errors
// fail closed.
func parkedBackupConflictFor(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("rescrape backup scan %s: %w", dir, err)
	}
	inflightPrefix := ".inflight-" + url.PathEscape(posterID) + "."
	loFull := strings.ToLower(posterID + "-full.jpg.rsbak.")
	loCrop := strings.ToLower(posterID + ".jpg.rsbak.")
	loMark := strings.ToLower(inflightPrefix)
	for _, e := range entries {
		n := e.Name()
		nl := strings.ToLower(n)
		// audit codex P2: fold-case marker probing — case-insensitive fs + a
		// case-variant scraper ID names the same bytes.
		if strings.HasPrefix(nl, loCrop) || strings.HasPrefix(nl, loFull) {
			return true, nil
		}
		if strings.HasPrefix(nl, loMark) && markerAnchoredBatch(n, inflightPrefix) {
			return true, nil
		}
	}
	return false, nil
}

// cropWitnessConflict scans the poster dir for crop witnesses naming
// posterID, returning the conflicting witness filename ("" when none). Both
// admission guards (crop write + from-URL promote) share this scan so a
// committed-but-unpromoted crop blocks BOTH follow-up mutations. Corrupt
// payloads defer to the reconciler; scan errors fail CLOSED (codex P2).
func cropWitnessConflict(fs afero.Fs, dir, posterID string) (string, error) {
	entries, err := afero.ReadDir(fs, dir)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return "", nil
	default:
		return "", fmt.Errorf("crop witness scan %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, cropWitnessPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return "", fmt.Errorf("crop witness scan %s: %w", name, rerr)
		}
		var foreign cropWitness
		if jerr := json.Unmarshal(data, &foreign); jerr != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(foreign.PosterID), strings.TrimSpace(posterID)) {
			// codex cloud P1 (case-fold fences): family resolution folds case —
			// a pending witness written under a case-variant spelling of the
			// same poster must fence identically.
			return name, nil
		}
	}
	return "", nil
}

// errCropWitnessPending fences a crop when an EARLIER crop witness for the
// same poster is still unresolved (codex P2: the promote-failure branch
// retains its witness for the startup reconciler; production runs no periodic
// reconciliation). Without the fence a second crop could commit and promote
// successfully, after which startup would treat the older witness as
// committed (identical canonical URL) and promote its STALE staged bytes
// over the newer crop. The handler maps this to HTTP 409 — restart to
// reconcile before retrying.
var errCropWitnessPending = errors.New("crop witness outstanding")

// writeCropWitnessGuarded refuses to create a crop witness while one for the
// same poster is already outstanding, mirroring writePromoteWitnessGuarded.
// Unknown/corrupt foreign witnesses are skipped (the reconciler owns them);
// transient scan errors fail CLOSED so a half-counted fence is impossible.
func writeCropWitnessGuarded(fs afero.Fs, tempDir, jobID string, w cropWitness) (string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	// codex P2: rekey/promote witnesses for this poster also fence a new crop.
	// A rekey that failed mid-relocation can leave one leg under the NEW id —
	// admitting an old-id crop beside the stranded leg corrupts later rekey
	// reconciliation. Probe the exact names the writers use (promote escapes
	// via PathEscape; rekey concatenates raw — mirror each) and fail CLOSED on
	// stat errors.
	// codex cloud P1 (case-fold probes): pending-state keyed by exact name
	// missed case-variant spellings — promoteWitnessConflict folds.
	if pwErr := promoteWitnessConflict(fs, dir, w.PosterID); pwErr != nil {
		if errors.Is(pwErr, errPromoteWitnessPending) {
			return "", fmt.Errorf("%w for %s (fence: promote witness) — restart to reconcile before retrying", errCropWitnessPending, w.PosterID)
		}
		return "", fmt.Errorf("crop witness scan %s: %w", promoteWitnessName(w.PosterID), pwErr)
	}
	if hit, serr := rekeyWitnessIDsFor(fs, dir, w.PosterID); serr != nil {
		return "", fmt.Errorf("crop witness scan: %w", serr)
	} else if hit {
		return "", fmt.Errorf("%w for %s (fence: rekey witness) — restart to reconcile before retrying", errCropWitnessPending, w.PosterID)
	}

	// codex cloud P2 (@snFs): a retained eviction record for this poster means
	// the committed edit's physical removals aren't done — refuse further ops.
	if pending, perr := pendingEvictFromDir(fs, dir, w.PosterID); perr != nil {
		return "", fmt.Errorf("crop witness scan: eviction probe: %w", perr)
	} else if pending {
		return "", fmt.Errorf("%w for %s (fence: eviction witness) — restart to reconcile before retrying", errCropWitnessPending, w.PosterID)
	}
	if parked, perr := parkedBackupConflictFor(fs, dir, w.PosterID); perr != nil {
		return "", perr
	} else if parked {
		return "", fmt.Errorf("%w for %s (fence: in-flight rescrape) — retry after it completes", errCropWitnessPending, w.PosterID)
	}
	conflict, cerr := cropWitnessConflict(fs, dir, w.PosterID)
	if cerr != nil {
		return "", cerr
	}
	if conflict != "" {
		return "", fmt.Errorf("%w for %s — restart to reconcile before retrying", errCropWitnessPending, w.PosterID)
	}
	return writeCropWitness(fs, tempDir, jobID, w)
}

// cropPromoteMaxAttempts bounds the immediate promote retry so a transient
// rename failure never strands a crop behind the witness fence.
const cropPromoteMaxAttempts = 3

// promoteCroppedLegWithRetry retries the post-commit promote a bounded number
// of times before yielding to the witness+fence path (codex P2: retry
// immediately instead of deferring every transient failure to restart).
func promoteCroppedLegWithRetry(fs afero.Fs, tempDir, jobID, stageID, posterID string) error {
	err := promoteCroppedLeg(fs, tempDir, jobID, stageID, posterID)
	for attempt := 1; err != nil && attempt < cropPromoteMaxAttempts; attempt++ {
		err = promoteCroppedLeg(fs, tempDir, jobID, stageID, posterID)
	}
	return err
}

func writeCropWitness(fs afero.Fs, tempDir, jobID string, w cropWitness) (string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	p := filepath.Join(tempDir, "posters", jobID, cropWitnessName(w.StageID))
	payload := mustMarshal(w)
	tmp := p + ".tmp"
	if err := afero.WriteFile(fs, tmp, payload, 0o644); err != nil {
		return "", fmt.Errorf("crop witness write %s: %w", tmp, err)
	}
	if err := fs.Rename(tmp, p); err != nil {
		_ = fs.Remove(tmp)
		return "", fmt.Errorf("crop witness rename %s: %w", p, err)
	}
	return p, nil
}

func removeCropWitness(fs afero.Fs, p string) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	// codex cloud P2: same bounded retry as the promote sweep.
	if err := removeWithRetry(fs, p); err != nil {
		logging.Warnf("crop witness sweep %s: %v", p, err)
	}
}

// promoteCroppedLeg moves the staged cropped poster over the canonical name
// (rename replaces the destination on both OS and in-memory filesystems). It
// runs AFTER the state commit — the byte swap is the observable tail of the
// commit, never its precursor.
func promoteCroppedLeg(fs afero.Fs, tempDir, jobID, stageID, posterID string) error {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	src := filepath.Join(dir, stageID+".jpg")
	dst := filepath.Join(dir, posterID+".jpg")
	if _, err := fs.Stat(src); err != nil {
		if os.IsNotExist(err) {
			// local codex review P1: the crop manager produced this leg BEFORE the
			// commit, so absence here means the staged bytes vanished — never
			// report success while canonical stays stale behind a committed crop.
			return fmt.Errorf("crop promote source %s missing after commit: %w", src, err)
		}
		return fmt.Errorf("crop promote source stat %s: %w", src, err)
	}
	if err := fs.Rename(src, dst); err != nil {
		return fmt.Errorf("crop promote %s→%s: %w", src, dst, err)
	}
	return nil
}

// cleanupStagedPosterPair removes leftover staged files after a failed
// promote/commit. Callers own the stage namespace (unique per request), so
// no lock is needed.
func cleanupStagedPosterPair(fs afero.Fs, tempDir, jobID, stageID string) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	for _, name := range []string{stageID + "-full.jpg", stageID + ".jpg"} {
		if err := fs.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			logging.Warnf("staged poster cleanup %s: %v", name, err)
		}
	}
}

// resolvePosterID resolves the effective poster identifier for a movie within a
// batch job. It starts with the URL parameter movieID, then looks up the movie
// result to use the canonical Movie.ID if available. Returns an error if the
// resolved ID fails safe-filename validation (path traversal check).
func resolvePosterID(lookup resultstore.MovieLookup, movieID string) (string, error) {
	posterID := movieID
	movieResult, _ := lookup.FindMovieResultForMovieID(movieID)
	if movieResult != nil && movieResult.Movie != nil && movieResult.Movie.ID != "" {
		posterID = movieResult.Movie.ID
	}
	if posterID != filepath.Base(posterID) || posterID == "" || posterID == "." {
		return "", fmt.Errorf("invalid movie ID for poster operation")
	}
	return posterID, nil
}

// posterStageSeq makes same-tick stage IDs unique — a process-wide atomic
// suffix, mirroring the worker's rescrapeBackupSeq (codex cloud P2: same-tick
// twin downloads would otherwise write into each other's staged pair, and the
// first lock winner would promote the OTHER request's bytes).
var posterStageSeq atomic.Int64

// nextPosterStageID builds the staged name for an out-of-key poster
// fetch/crop: <posterID>.<kind>-<unixnano-hex>.<seq-hex>.
func nextPosterStageID(posterID, kind string) string {
	return posterID + "." + kind + "-" + fmt.Sprintf("%x.%x", time.Now().UnixNano(), posterStageSeq.Add(1))
}
