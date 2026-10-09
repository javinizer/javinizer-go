package workflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// Sweep ownership sealing (codex P1, PR #276, finding ntCe1 — "require
// authentic ownership before sweeping staging trees").
//
// The staging manifest's evidence fields — the token derived from the
// directory name, the local hostname, any positive PID, a nonzero
// completed_unix_nano — are all PUBLIC to any writer sharing the destination
// filesystem: another process or SMB client can create or rename a directory
// to .javinizer-apply-* and drop a manifest restating them, and an unsealed
// manifest then authorized recursive deletion of a tree Javinizer never
// created. Authentic ownership therefore rides an HMAC-SHA256 seal over the
// manifest's canonical encoding, keyed by a Javinizer-local secret that lives
// OUTSIDE the managed tree (the user cache dir, mode 0600 — never the
// possibly-shared artifact filesystem, so a filesystem-level attacker cannot
// read or mint seals). The seal binds every evidence field: token (replayed
// seals from a sibling tree never match, since each tree's token differs),
// hostname, PID, and both lifecycle stamps.
//
// Backwards compatibility (the round-27 retention precedent): pre-fix trees
// carry no seal and are RETAINED, never destroyed — an unverifiable root is
// recoverable residue, a wrong delete is not. The same fail-closed posture
// applies when the secret itself is unavailable: manifests are written
// unsealed (warned) and every later verification refuses them.
//
// Cross-process scope matches the sweep's pre-existing hostname binding: only
// a same-host Javinizer instance ever reclaimed a root, and the per-machine
// secret file is readable by exactly that same local user — so one user's
// Javinizer processes seal and verify against one key durable across process
// restarts.

const (
	// artifactStageSecretName is the per-user cache-dir file holding the
	// sweep sealing key.
	artifactStageSecretName = "artifact-stage-sweep.key"
	// artifactStageSecretBytes is the raw key length (HMAC-SHA256).
	artifactStageSecretBytes = 32
)

// Sweep sealing seams ride the artifactSweep* seam family: tests pin a fixed
// key (never the developer's real cache dir) and replay the loader's failure
// legs deterministically.
var (
	artifactSweepSecretPath   = defaultArtifactSweepSecretPath
	artifactSweepSecretKey    = defaultArtifactSweepSecretKey
	artifactSweepUserCacheDir = os.UserCacheDir
	artifactSweepUserHomeDir  = os.UserHomeDir
	artifactSweepMkdirAll     = os.MkdirAll
	artifactSweepCreateKey    = defaultArtifactSweepCreateKey
)

type artifactSweepKeyFile interface {
	Write(p []byte) (int, error)
	Close() error
}

func defaultArtifactSweepCreateKey(path string) (artifactSweepKeyFile, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// defaultArtifactSweepSecretPath locates the sealing key under the per-user
// cache dir ("Javinizer" rides the desktop package's dir-name convention),
// falling back to ~/.javinizer when the OS cache dir is unresolvable — the
// same fallback chain desktop.UserDataDir applies for the config dir.
func defaultArtifactSweepSecretPath() (string, error) {
	base, err := artifactSweepUserCacheDir()
	if err != nil || base == "" {
		home, herr := artifactSweepUserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("locate artifact sweep key dir: %w (home fallback: %v)", err, herr)
		}
		base = filepath.Join(home, ".javinizer")
	}
	return filepath.Join(base, "Javinizer", artifactStageSecretName), nil
}

// defaultArtifactSweepSecretKey loads the local sealing key, creating it on
// first use. Creation is O_EXCL with a read-back on collision, so concurrent
// Javinizer processes converging on first use share ONE key (the loser's
// draw is discarded). The key never rides the caller's afero filesystem: it
// must live on storage the staged tree's other writers cannot reach.
func defaultArtifactSweepSecretKey() ([]byte, error) {
	path, err := artifactSweepSecretPath()
	if err != nil {
		return nil, err
	}
	key, err := readArtifactSweepSecret(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if mkErr := artifactSweepMkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return nil, fmt.Errorf("create artifact sweep key dir: %w", mkErr)
	}
	drawn := make([]byte, artifactStageSecretBytes)
	if _, randErr := artifactSweepRand(drawn); randErr != nil {
		return nil, fmt.Errorf("draw artifact sweep key: %w", randErr)
	}
	handle, createErr := artifactSweepCreateKey(path)
	if createErr != nil {
		if os.IsExist(createErr) {
			// A concurrent process won the create; its key is the key.
			return readArtifactSweepSecret(path)
		}
		return nil, fmt.Errorf("create artifact sweep key %s: %w", path, createErr)
	}
	if _, writeErr := handle.Write(drawn); writeErr != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("write artifact sweep key %s: %w", path, writeErr)
	}
	if closeErr := handle.Close(); closeErr != nil {
		return nil, fmt.Errorf("close artifact sweep key %s: %w", path, closeErr)
	}
	return drawn, nil
}

// readArtifactSweepSecret reads and validates the key file. A corrupt
// (wrong-length) or unreadable file is a hard failure: sealing/verifying
// against a degraded key would silently split the ownership ring.
func readArtifactSweepSecret(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != artifactStageSecretBytes {
		return nil, fmt.Errorf("artifact sweep key %s corrupt (want %d bytes, got %d)", path, artifactStageSecretBytes, len(key))
	}
	return key, nil
}

// sealArtifactStageManifest MACs the manifest's canonical encoding (the
// struct serialized with Seal cleared) and records the hex digest in Seal.
func sealArtifactStageManifest(manifest *artifactStageManifest) error {
	key, err := artifactSweepSecretKey()
	if err != nil {
		return err
	}
	manifest.Seal = ""
	body, err := artifactSweepMarshal(manifest)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	manifest.Seal = hex.EncodeToString(mac.Sum(nil))
	return nil
}

// artifactStageSealed reports whether the manifest carries a VALID seal over
// its exact evidence fields, recomputed through the same canonical encoding
// (any field tampering — token, hostname, PID, lifecycle stamps — invalidates
// it, as does a seal copied from a differently-tokencarrying tree). A missing
// key fails closed: unverifiable roots are retained.
func artifactStageSealed(manifest *artifactStageManifest) bool {
	if manifest.Seal == "" {
		return false
	}
	got, err := hex.DecodeString(manifest.Seal)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	key, err := artifactSweepSecretKey()
	if err != nil {
		return false
	}
	recomputed := *manifest
	recomputed.Seal = ""
	body, err := artifactSweepMarshal(&recomputed)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// sealArtifactStageManifestBestEffort stamps the ownership seal for a
// manifest the caller is about to persist. A sealing failure downgrades the
// manifest to unsealed — never an apply-blocking error — because the
// verification gates retain unsealed trees by contract.
func sealArtifactStageManifestBestEffort(root string, manifest *artifactStageManifest) {
	if err := sealArtifactStageManifest(manifest); err != nil {
		logging.Warnf("artifact staging manifest seal unavailable for %s: %v — the unsealed tree is never auto-swept", root, err)
	}
}
