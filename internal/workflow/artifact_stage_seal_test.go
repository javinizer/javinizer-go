package workflow

// codex P1 (PR #276, finding ntCe1) — "require authentic ownership before
// sweeping staging trees". The staging manifest's evidence is forgeable
// end-to-end by any writer sharing the destination filesystem: the token
// derives from the directory name, the hostname is public, and any positive
// PID with a nonzero completed stamp used to authorize recursive deletion.
// Sweep authority now rides an HMAC-SHA256 seal keyed by a Javinizer-local
// secret stored outside the tree; these tests prove a forged manifest is
// retained while a legitimately sealed one is still reclaimed.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// artifactStageTestSecret pins the package-wide sweep key: sweep tests run
// almost entirely on virtual filesystems and must never create or read a
// key in the developer's real cache dir.
var artifactStageTestSecret = []byte("0123456789abcdef-ARTIFACT-SWEEP-KEY")

func TestMain(m *testing.M) {
	old := artifactSweepSecretKey
	artifactSweepSecretKey = func() ([]byte, error) { return artifactStageTestSecret, nil }
	code := m.Run()
	artifactSweepSecretKey = old
	os.Exit(code)
}

// foreignSweepKey pins a DIFFERENT key, standing in for a Javinizer instance
// outside the ownership ring (or any other holder of the manifest format).
func foreignSweepKey(t *testing.T) {
	t.Helper()
	old := artifactSweepSecretKey
	artifactSweepSecretKey = func() ([]byte, error) { return []byte("fedcba9876543210-a-FOREIGN-sweep-key."), nil }
	t.Cleanup(func() { artifactSweepSecretKey = old })
}

// forgedStageRoot plants the finding's attack shape byte-for-byte: a
// staging-named directory whose manifest restates every publicly observable
// evidence field — the token derived from the directory name, the local
// hostname, a positive PID, and a nonzero completed stamp — without (or with
// an invalid) ownership seal.
func forgedStageRoot(t *testing.T, fs afero.Fs, parent string, mutate func(*artifactStageManifest)) string {
	t.Helper()
	root, err := afero.TempDir(fs, parent, artifactStageDirPrefix)
	require.NoError(t, err)
	host, _ := os.Hostname()
	manifest := artifactStageManifest{
		Version:           artifactStageManifestVer,
		Token:             artifactStageManifestToken(root),
		PID:               os.Getpid(),
		Hostname:          host,
		CreatedAt:         time.Now().UTC(),
		CompletedUnixNano: time.Now().UTC().UnixNano(),
	}
	if mutate != nil {
		mutate(&manifest)
	}
	body, err := json.Marshal(&manifest)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, artifactStageManifestName), body, 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "payload.bin"), []byte("never-javinizer-bytes"), 0o644))
	return root
}

// A forged manifest carrying every convincing field — valid token derived
// from the directory name, the local hostname, a positive PID, a nonzero
// completed stamp — must NOT authorize recursive deletion: without the local
// secret the seal cannot be minted, so the tree is retained.
func TestSweepArtifactStaging_ForgedCompleteManifestRetained(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*artifactStageManifest)
	}{
		{name: "no seal at all", mutate: nil},
		{name: "garbage seal", mutate: func(m *artifactStageManifest) { m.Seal = strings.Repeat("ab", 32) }},
		{name: "truncated seal", mutate: func(m *artifactStageManifest) { m.Seal = "abcd" }},
		{name: "non-hex seal", mutate: func(m *artifactStageManifest) { m.Seal = strings.Repeat("zz", 32) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			setSweepSeams(t, fsutil.ProcessDead, nil)
			root := forgedStageRoot(t, fs, "/lib", tc.mutate)

			assert.False(t, artifactStageManifestReclaimable(fs, root), "the forged manifest fails the seal gate")
			sweepArtifactStaging(fs, "/lib")

			exists, err := afero.DirExists(fs, root)
			require.NoError(t, err)
			assert.True(t, exists, "a staged tree Javinizer cannot authenticate is retained, never swept")
			payload, perr := afero.ReadFile(fs, filepath.Join(root, "payload.bin"))
			require.NoError(t, perr)
			assert.Equal(t, "never-javinizer-bytes", string(payload), "foreign payload untouched")
		})
	}
}

// A completed-tamper variant: seal the manifest LEGITIMATELY, then flip the
// evidence a sweeper would act on. The seal binds every field, so the
// tampered manifest is a forgery and the tree is retained.
func TestSweepArtifactStaging_TamperedSealedManifestRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := forgedStageRoot(t, fs, "/lib", nil)

	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	var manifest artifactStageManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.NoError(t, sealArtifactStageManifest(&manifest), "a genuine owner seals first")
	manifest.CompletedUnixNano = time.Now().UTC().UnixNano() // attacker flips completion post-seal
	tampered, err := json.Marshal(&manifest)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, artifactStageManifestName), tampered, 0o600))

	assert.False(t, artifactStageManifestReclaimable(fs, root), "field tampering invalidates the seal")
	sweepArtifactStaging(fs, "/lib")
	exists, err := afero.DirExists(fs, root)
	require.NoError(t, err)
	assert.True(t, exists, "tampered manifest retained")
}

// The external sidecar proof is gated the same way: a quarantined tree whose
// manifest vanished (partial remove) and whose sidecar is a full-field
// FORGERY is retained — the pre-fix gate (well-formed + completed + same
// host + name-derived token) was forgeable end-to-end.
func TestSweepArtifactStaging_ForgedSidecarProofRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := forgedStageRoot(t, fs, "/lib", nil)
	quarantine := root + artifactStageQuarantineMark + "ff"
	require.NoError(t, fs.Rename(root, quarantine))
	// Manifest consumed mid-remove; only the (forged) sidecar remains.
	require.NoError(t, fs.Remove(filepath.Join(quarantine, artifactStageManifestName)))

	assert.False(t, artifactStageReclaimable(fs, quarantine), "the forged sidecar fails the seal gate")
	sweepArtifactStaging(fs, "/lib")
	exists, err := afero.DirExists(fs, quarantine)
	require.NoError(t, err)
	assert.True(t, exists, "forged sidecar never authorizes removal")
}

// Control for the forge tests: the SAME evidence fields, sealed through the
// production writer with the ring key, sweep exactly as before. Pre-fix this
// shape also swept (it is indistinguishable from the forgery pre-fix) — the
// seal is what separates them now.
func TestSweepArtifactStaging_SealedCompletedRootStillSwept(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) {
		m.CompletedUnixNano = time.Now().UTC().UnixNano()
	})

	sweepArtifactStaging(fs, "/lib")

	exists, err := afero.DirExists(fs, root)
	require.NoError(t, err)
	assert.False(t, exists, "a legitimately sealed completed root is still reclaimed")
}

// A root sealed by a key OUTSIDE this machine's ring reads as foreign
// residue: sealed but not verifiable locally. Retained.
func TestSweepArtifactStaging_WrongKeySealRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) {
		m.CompletedUnixNano = time.Now().UTC().UnixNano()
	})
	foreignSweepKey(t) // the verifier no longer holds the sealing key

	sweepArtifactStaging(fs, "/lib")

	exists, err := afero.DirExists(fs, root)
	require.NoError(t, err)
	assert.True(t, exists, "a seal minted by another key ring is never ours to destroy")
}

// Fail-closed availability: when the secret cannot be loaded, manifests are
// still written (debug evidence) but UNSEALED, and sweeps retain them.
func TestSweepArtifactStaging_UnavailableKeyNeverBlocksApply(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/stage", 0o755))
	old := artifactSweepSecretKey
	artifactSweepSecretKey = func() ([]byte, error) { return nil, errors.New("key store denied") }
	t.Cleanup(func() { artifactSweepSecretKey = old })

	root := "/stage/" + artifactStageDirPrefix + "unsealed"
	require.NoError(t, fs.MkdirAll(root, 0o755))
	assert.NotPanics(t, func() { writeArtifactStageManifest(fs, root) }, "sealing failure never blocks the apply")

	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err, "the manifest is still written as debug evidence")
	var manifest artifactStageManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	assert.Empty(t, manifest.Seal, "no seal without the key")

	// The same unavailable-key verifier then retains the tree.
	assert.False(t, artifactStageManifestReclaimable(fs, root))
}

// The default loader creates the key file with owner-only permissions under
// the redirected cache dir, reuses it across calls, shares it with a
// pre-created sibling (the O_EXCL collision leg), and refuses corrupt files.
func TestArtifactSweepSecretLoader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0600 permission assertions are POSIX-specific")
	}
	dir := t.TempDir()
	old := artifactSweepSecretPath
	artifactSweepSecretPath = func() (string, error) { return filepath.Join(dir, "Javinizer", artifactStageSecretName), nil }
	t.Cleanup(func() { artifactSweepSecretPath = old })

	first, err := defaultArtifactSweepSecretKey()
	require.NoError(t, err)
	require.Len(t, first, artifactStageSecretBytes)
	info, err := os.Stat(filepath.Join(dir, "Javinizer", artifactStageSecretName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the key file is owner-only")

	second, err := defaultArtifactSweepSecretKey()
	require.NoError(t, err)
	assert.Equal(t, first, second, "the key is durable across processes")

	// Corrupt key: never derive authority from a degraded secret.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Javinizer", artifactStageSecretName), []byte("short"), 0o600))
	_, err = defaultArtifactSweepSecretKey()
	require.Error(t, err, "a corrupt key file is a hard failure, never a seal")
}

func TestArtifactSweepSecretLoader_PathFailure(t *testing.T) {
	old := artifactSweepSecretPath
	artifactSweepSecretPath = func() (string, error) { return "", errors.New("no cache dir") }
	t.Cleanup(func() { artifactSweepSecretPath = old })
	_, err := defaultArtifactSweepSecretKey()
	require.Error(t, err)
}
