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
	// Seal is the HMAC-SHA256 ownership proof over every other field (codex
	// P1, PR #276, finding ntCe1): the evidence above is PUBLIC to any writer
	// sharing the filesystem — a directory renamed to .javinizer-apply-* plus
	// a manifest restating the visible token, local hostname, any positive
	// PID, and a nonzero completed stamp would otherwise authorize recursive
	// deletion of a tree Javinizer never created. The seal is keyed by a
	// Javinizer-local secret stored outside the tree (artifact_stage_seal.go);
	// pre-fix trees carry no seal and are retained, never swept.
	Seal string `json:"seal,omitempty"`
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
	// Sealed ownership (finding ntCe1): the manifest alone is forgeable by
	// any writer sharing the filesystem, so the authority to sweep comes
	// from the MAC, never from the evidence fields. A sealing failure
	// downgrades to the pre-fix unsealed shape — retention-guaranteed by
	// the verification gates — rather than blocking the apply.
	sealArtifactStageManifestBestEffort(root, &manifest)
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
// reclaimed. Anything unverifiable (missing/malformed manifest, a forged or
// unsealed marker, token mismatch, foreign host, live or undecidable owner)
// is retained — residue is recoverable, a wrong delete is not.
func artifactStageReclaimable(fs afero.Fs, path string) bool {
	_, ok := artifactStageReclaimableIdentity(fs, path)
	return ok
}

type artifactStageDirIdentity struct {
	known     bool
	hasDevIno bool
	dev       uint64
	ino       uint64
	size      int64
	modTime   time.Time
}

func artifactStageReclaimableIdentity(fs afero.Fs, path string) (artifactStageDirIdentity, bool) {
	// Capture the directory identity FIRST, and prove it again after the
	// authentication read (codex P1, PRRT_kwDORn9KaM6qLkJz): that read opens the
	// proof and manifest, and each open is a window where a writer can swap the
	// directory — so the identity who passed authentication must be the one the
	// name carried beforehand and still carries afterwards.
	preInfo, preErr := lstatArtifactStageDir(fs, path)
	if preErr != nil || preInfo == nil || preInfo.Mode()&os.ModeSymlink != 0 || !preInfo.IsDir() {
		return artifactStageDirIdentity{}, false
	}
	captured := captureArtifactStageDirIdentity(fs, path, preInfo)
	reclaimable := artifactStageManifestReclaimable(fs, path)
	if !reclaimable {
		// Manifest-level claims failed: an external sidecar proof is the
		// last-remaining ownership evidence. It applies to any staging-named tree
		// (quarantined or left under its original name after a failed carry).
		// The token binding alone never proved origin — any writer can derive it
		// from the directory name — so the sidecar must pass the same MAC seal
		// gate as the manifest (finding ntCe1): nobody without the local sweep
		// secret could have written it.
		reclaimable = readArtifactStageProof(fs, path)
	}
	if !reclaimable {
		return artifactStageDirIdentity{}, false
	}
	if !artifactStageDirMatches(fs, path, captured) {
		// The identity moved while we authenticated: refuse rather than deleting
		// whatever stands at the name now.
		return artifactStageDirIdentity{}, false
	}
	return captured, true
}

func lstatArtifactStageDir(fs afero.Fs, path string) (os.FileInfo, error) {
	if lst, ok := fs.(afero.Lstater); ok {
		info, _, err := lst.LstatIfPossible(path)
		return info, err
	}
	return fs.Stat(path)
}

func captureArtifactStageDirIdentity(fs afero.Fs, path string, info os.FileInfo) artifactStageDirIdentity {
	id := artifactStageDirIdentity{known: true, size: info.Size(), modTime: info.ModTime()}
	if dev, ino, ok := fsutil.BoundObjectIdentity(fs, path, info); ok {
		id.hasDevIno = true
		id.dev, id.ino = dev, ino
	}
	return id
}

func (id artifactStageDirIdentity) matches(fs afero.Fs, path string, info os.FileInfo) bool {
	if !id.known || info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	if id.hasDevIno {
		dev, ino, ok := fsutil.BoundObjectIdentity(fs, path, info)
		return ok && dev == id.dev && ino == id.ino
	}
	return info.Size() == id.size && info.ModTime().Equal(id.modTime)
}

func artifactStageDirMatches(fs afero.Fs, path string, id artifactStageDirIdentity) bool {
	info, err := lstatArtifactStageDir(fs, path)
	return err == nil && id.matches(fs, path, info)
}

func artifactStageDirStillNamed(fs afero.Fs, path string) bool {
	info, err := lstatArtifactStageDir(fs, path)
	return err == nil && info != nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir()
}

// artifactStageManifestReclaimable enforces the manifest-backed ownership
// rules: a MAC-sealed marker (the ONLY authentic origin proof — every other
// field is forgeable by a filesystem-sharing writer, finding ntCe1), valid
// version, matching token, same hostname, and either a completed lifecycle
// stamp or a provably-dead (or provably-reused) owner PID.
func artifactStageManifestReclaimable(fs afero.Fs, path string) bool {
	body, err := afero.ReadFile(fs, filepath.Join(path, artifactStageManifestName))
	if err != nil {
		return false
	}
	var manifest artifactStageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return false
	}
	// Authentic ownership FIRST: a fabricated manifest restating the visible
	// token, local hostname, a positive PID, and a completed stamp is rejected
	// here before any structural claim is consulted (pre-fix unsealed trees
	// take the same refusal and are retained, the round-27 precedent).
	if !artifactStageSealed(&manifest) {
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
		// The seal MACs exactly these fields through one canonical encoding,
		// so the in-tree seal transfers to the byte-identical sidecar — but
		// only a VALID seal does: a forged manifest without a verifiable seal
		// never reaches this writer (the reclaim gates refuse it first).
		Seal: manifest.Seal,
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
// writeArtifactStageProof. A sidecar must be MAC-sealed by the local sweep
// secret (finding ntCe1): the pre-fix gate — well-formed, completed, same
// host, token matching the directory name — was forgeable end-to-end by any
// writer sharing the filesystem, so the name+token pairing alone never
// authorizes the destructive removal this reader gates.
func readArtifactStageProof(fs afero.Fs, path string) bool {
	body, err := afero.ReadFile(fs, artifactStageProofPath(path))
	if err != nil {
		return false
	}
	var manifest artifactStageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return false
	}
	if !artifactStageSealed(&manifest) {
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
		identity, reclaimable := artifactStageReclaimableIdentity(fs, candidate)
		if !reclaimable {
			continue
		}
		if !strings.Contains(name, artifactStageQuarantineMark) {
			target := artifactStageQuarantineName(candidate)
			if err := fs.Rename(candidate, target); err != nil {
				continue
			}
			// Bind quarantine to the directory authenticated above (codex P1,
			// PRRT_kwDORn9KaM6qLkJz): a rename-swap that plants a different tree
			// at the quarantine name, or replants the original root name, refuses
			// before any completion mark or RemoveAll can touch foreign bytes.
			if !artifactStageDirMatches(fs, target, identity) || artifactStageDirStillNamed(fs, candidate) {
				continue
			}
			// The pre-quarantine proof sidecar from an earlier cleanup failure
			// belongs to this same tree: carry it so the removal deletes both.
			if _, statErr := fs.Stat(artifactStageProofPath(candidate)); statErr == nil {
				_ = fs.Rename(artifactStageProofPath(candidate), artifactStageProofPath(target))
			}
			candidate = target
		} else if !artifactStageDirMatches(fs, candidate, identity) {
			continue
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
		if identity.hasDevIno && !artifactStageDirMatches(fs, candidate, identity) {
			logging.Warnf("artifact staging sweep retained %s: staging root identity changed before removal", candidate)
			continue
		}
		if err := removeArtifactTreeWithRetry(fs, candidate); err != nil {
			logging.Warnf("artifact staging sweep retained %s: %v", candidate, err)
		} else {
			_ = fs.Remove(artifactStageProofPath(candidate))
		}
	}
}
