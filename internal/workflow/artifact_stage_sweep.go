package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const (
	artifactStageDirPrefix      = ".javinizer-apply-"
	artifactStageQuarantineMark = ".rm."
	artifactStageManifestName   = ".javinizer-apply-manifest.json"
	artifactStageManifestVer    = 1

	artifactRemoveAttempts = 6
)

// artifactStageManifest marks a staging root as owned by this process so a
// later sweep can tell reclaimable residue (dead owner, same host) apart from
// a foreign or still-running apply.
type artifactStageManifest struct {
	Version              int       `json:"version"`
	Token                string    `json:"token"`
	PID                  int       `json:"pid"`
	Hostname             string    `json:"hostname"`
	ProcessStartUnixNano int64     `json:"process_start_unix_nano,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	// CompletedUnixNano is stamped when the owner finished with the root
	// (cleanup exhausted its retries): the residue is trash regardless of
	// owner liveness, so a long-lived process never blocks its own sweeps.
	CompletedUnixNano int64 `json:"completed_unix_nano,omitempty"`
}

// Sweep seams stay injectable: reclaim decisions must be testable without
// owning real PIDs or sleeping real backoff.
var (
	artifactSweepHostname  = os.Hostname
	artifactSweepLiveness  = fsutil.ProbeProcessLiveness
	artifactSweepStartTime = fsutil.ProbeProcessStartTime
	artifactSweepSleep     = time.Sleep
	artifactSweepRand      = rand.Read
	artifactSweepMarshal   = json.Marshal
)

func artifactStageManifestToken(root string) string {
	name := filepath.Base(root)
	token := strings.TrimPrefix(name, artifactStageDirPrefix)
	if i := strings.Index(token, artifactStageQuarantineMark); i >= 0 {
		token = token[:i]
	}
	return token
}

// writeArtifactStageManifest stamps an owned staging root. Best-effort: a
// failed write must not block the apply (an unstamped root is simply never
// swept — the conservative default).
func writeArtifactStageManifest(fs afero.Fs, root string) {
	writeArtifactStageManifestState(fs, root, 0)
}

// markArtifactStageCompleted stamps the finished lifecycle state so later
// sweeps can reclaim residue even while the owning process stays alive.
func markArtifactStageCompleted(fs afero.Fs, root string) {
	writeArtifactStageManifestState(fs, root, time.Now().UTC().UnixNano())
}

func writeArtifactStageManifestState(fs afero.Fs, root string, completedUnixNano int64) {
	hostname, _ := artifactSweepHostname()
	manifest := artifactStageManifest{
		Version:           artifactStageManifestVer,
		Token:             artifactStageManifestToken(root),
		PID:               os.Getpid(),
		Hostname:          hostname,
		CreatedAt:         time.Now().UTC(),
		CompletedUnixNano: completedUnixNano,
	}
	if start := artifactSweepStartTime(manifest.PID); start != nil {
		manifest.ProcessStartUnixNano = start.UnixNano()
	}
	body, err := artifactSweepMarshal(manifest)
	if err != nil {
		logging.Warnf("artifact staging manifest encode failed for %s: %v", root, err)
		return
	}
	if err := afero.WriteFile(fs, filepath.Join(root, artifactStageManifestName), body, 0o600); err != nil {
		logging.Warnf("artifact staging manifest write failed for %s: %v", root, err)
	}
}

// artifactStageReclaimable decides whether the staging root at path may be
// reclaimed. Anything unverifiable (missing/malformed manifest, token
// mismatch, foreign host, live or undecidable owner) is retained — residue is
// recoverable, a wrong delete is not.
func artifactStageReclaimable(fs afero.Fs, path string) bool {
	if !artifactStageManifestReclaimable(fs, path) {
		// Manifest-level claims failed: an external sidecar proof is the
		// last-remaining ownership evidence. It applies to any staging-named tree
		// (quarantined or left under its original name after a failed carry),
		// whose token binding proves nobody else could have written the marker.
		return readArtifactStageProof(fs, path)
	}
	return true
}

// artifactStageManifestReclaimable enforces the manifest-backed ownership
// rules: valid marker, matching token, same hostname, and either a completed
// lifecycle stamp or a provably-dead (or provably-reused) owner PID.
func artifactStageManifestReclaimable(fs afero.Fs, path string) bool {
	body, err := afero.ReadFile(fs, filepath.Join(path, artifactStageManifestName))
	if err != nil {
		return false
	}
	var manifest artifactStageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return false
	}
	if manifest.Version != artifactStageManifestVer || manifest.PID <= 0 {
		return false
	}
	if manifest.Token == "" || manifest.Token != artifactStageManifestToken(path) {
		return false
	}
	hostname, herr := artifactSweepHostname()
	if herr != nil || manifest.Hostname == "" {
		return false
	}
	if manifest.Hostname != hostname {
		return false
	}
	if manifest.CompletedUnixNano != 0 {
		return true
	}
	switch artifactSweepLiveness(manifest.PID) {
	case fsutil.ProcessAlive:
		if manifest.ProcessStartUnixNano == 0 {
			return false
		}
		start := artifactSweepStartTime(manifest.PID)
		if start == nil {
			return false
		}
		return start.UnixNano() != manifest.ProcessStartUnixNano
	case fsutil.ProcessDead:
		return true
	default:
		return false
	}
}

func artifactStageQuarantineName(path string) string {
	b := make([]byte, 4)
	if _, err := artifactSweepRand(b); err != nil {
		return path + artifactStageQuarantineMark + "0"
	}
	return path + artifactStageQuarantineMark + hex.EncodeToString(b)
}

const artifactStageProofSuffix = ".proof"

// artifactStageProofPath names the ownership sidecar kept OUTSIDE the staging
// tree. RemoveAll deletes the in-tree manifest first whenever a payload file
// refuses, so the sidecar preserves the completed-owner evidence for later
// sweeps of the quarantined residue.
func artifactStageProofPath(path string) string { return path + artifactStageProofSuffix }

// writeArtifactStageProof restates ownership (and completion, if set) beside
// the tree so proof survives a partial RemoveAll. Best-effort.
func writeArtifactStageProof(fs afero.Fs, path string) {
	body, err := afero.ReadFile(fs, filepath.Join(path, artifactStageManifestName))
	if err != nil {
		return
	}
	var manifest artifactStageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return
	}
	// A completed cleanup stamps complete; an in-flight failure stamps the
	// marker only when removal had already been decided by the caller.
	proof := artifactStageManifest{
		Version:              manifest.Version,
		Token:                manifest.Token,
		PID:                  manifest.PID,
		Hostname:             manifest.Hostname,
		ProcessStartUnixNano: manifest.ProcessStartUnixNano,
		CreatedAt:            manifest.CreatedAt,
		CompletedUnixNano:    manifest.CompletedUnixNano,
	}
	encoded, err := artifactSweepMarshal(proof)
	if err != nil {
		return
	}
	if err := afero.WriteFile(fs, artifactStageProofPath(path), encoded, 0o600); err != nil {
		logging.Warnf("artifact staging proof write failed for %s: %v", path, err)
	}
}

// readArtifactStageProof validates a proof sidecar written by
// writeArtifactStageProof. Quarantined names already prove an own-process
// claim, so a sidecar only needs to be well-formed and completed on this host.
func readArtifactStageProof(fs afero.Fs, path string) bool {
	body, err := afero.ReadFile(fs, artifactStageProofPath(path))
	if err != nil {
		return false
	}
	var manifest artifactStageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return false
	}
	if manifest.Version != artifactStageManifestVer || manifest.PID <= 0 {
		return false
	}
	if manifest.Token == "" || manifest.Token != artifactStageManifestToken(path) {
		return false
	}
	if manifest.CompletedUnixNano == 0 {
		return false
	}
	hostname, herr := artifactSweepHostname()
	if herr != nil || manifest.Hostname == "" {
		return false
	}
	return manifest.Hostname == hostname
}

// removeArtifactTreeWithRetry retries recursive removal with bounded backoff:
// SMB shares, oplocks and antivirus scanners routinely refuse a first delete
// of just-closed files. A final failure keeps the tree and reports it.
func removeArtifactTreeWithRetry(fs afero.Fs, path string) error {
	var err error
	delay := 50 * time.Millisecond
	for attempt := 0; attempt < artifactRemoveAttempts; attempt++ {
		err = fs.RemoveAll(path)
		if err == nil {
			return nil
		}
		if os.IsNotExist(err) {
			return nil
		}
		if attempt+1 < artifactRemoveAttempts {
			artifactSweepSleep(delay)
			delay *= 2
		}
	}
	return err
}

// sweepArtifactStaging removes orphan artifact-staging roots under parent
// (same level as the destination root, where prepareArtifact creates them).
// Best-effort: every failure retains the candidate so a later sweep retries.
func sweepArtifactStaging(fs afero.Fs, parent string) {
	if fs == nil || strings.TrimSpace(parent) == "" {
		return
	}
	entries, err := afero.ReadDir(fs, parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, artifactStageDirPrefix) {
			continue
		}
		candidate := filepath.Join(parent, name)
		// Already-quarantined names re-prove ownership too: a prefix-and-marker
		// match alone never authorizes deletion (foreign plants, PID reuse).
		if !artifactStageReclaimable(fs, candidate) {
			continue
		}
		if !strings.Contains(name, artifactStageQuarantineMark) {
			target := artifactStageQuarantineName(candidate)
			if err := fs.Rename(candidate, target); err != nil {
				continue
			}
			// The pre-quarantine proof sidecar from an earlier cleanup failure
			// belongs to this same tree: carry it so the removal deletes both.
			if _, statErr := fs.Stat(artifactStageProofPath(candidate)); statErr == nil {
				_ = fs.Rename(artifactStageProofPath(candidate), artifactStageProofPath(target))
			}
			candidate = target
		}
		// Stamp the completed lifecycle on the in-tree manifest, then mirror it
		// outside the tree: RemoveAll can consume the manifest before a locked
		// payload refuses, so the proof must exist before removal starts.
		markArtifactStageCompleted(fs, candidate)
		writeArtifactStageProof(fs, candidate)
		// Both markers are best-effort writes: on Windows/SMB either can fail
		// while the removal below half-succeeds, stranding a payload whose
		// manifest is already gone and whose proof is missing or still carries a
		// zero completion stamp — residue no later sweep could reclaim. Mirror
		// the cleanup-path gate: open the destructive leg only with a
		// re-readable COMPLETED sidecar proof beside the current name, else
		// retain the tree for a retry-safe later sweep.
		if !readArtifactStageProof(fs, candidate) {
			logging.Warnf("artifact staging sweep retained %s: completed ownership proof unavailable beside the staging root", candidate)
			continue
		}
		if err := removeArtifactTreeWithRetry(fs, candidate); err != nil {
			logging.Warnf("artifact staging sweep retained %s: %v", candidate, err)
		} else {
			_ = fs.Remove(artifactStageProofPath(candidate))
		}
	}
}
