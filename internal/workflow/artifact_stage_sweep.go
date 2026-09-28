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
	hostname, _ := artifactSweepHostname()
	manifest := artifactStageManifest{
		Version:   artifactStageManifestVer,
		Token:     artifactStageManifestToken(root),
		PID:       os.Getpid(),
		Hostname:  hostname,
		CreatedAt: time.Now().UTC(),
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
	if hostname, herr := artifactSweepHostname(); herr == nil && manifest.Hostname != "" && manifest.Hostname != hostname {
		return false
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
		quarantined := strings.Contains(name, artifactStageQuarantineMark)
		if !quarantined {
			if !artifactStageReclaimable(fs, candidate) {
				continue
			}
			target := artifactStageQuarantineName(candidate)
			if err := fs.Rename(candidate, target); err != nil {
				continue
			}
			candidate = target
		}
		if err := removeArtifactTreeWithRetry(fs, candidate); err != nil {
			logging.Warnf("artifact staging sweep retained %s: %v", candidate, err)
		}
	}
}
