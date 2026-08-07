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

// mustMarshal panics on marshal failure -- these witness structs are simple
// types that never fail to marshal, so the error check is dead code.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal failure: %v", err))
	}
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
	p := filepath.Join(tempDir, "posters", jobID, promoteWitnessName(posterID))
	if _, err := fs.Stat(p); err == nil {
		return "", fmt.Errorf("%w for %s — restart to reconcile before retrying", errPromoteWitnessPending, posterID)
	} else if !os.IsNotExist(err) {
		// codex r51 P2c: any OTHER stat error may still mean the witness file
		// exists — overwriting it would blind the reconciler to the pending
		// state. Fail closed.
		return "", fmt.Errorf("promote witness check %s: %w", p, err)
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
	if err := fs.Remove(p); err != nil && !os.IsNotExist(err) {
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
	if err := fs.Remove(p); err != nil && !os.IsNotExist(err) {
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
			return nil // manager never produced the leg: nothing to promote
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
