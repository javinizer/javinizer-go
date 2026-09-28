package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

type failRemoveAllFS struct {
	afero.Fs
	failures int
	attempts int
}

func (f *failRemoveAllFS) RemoveAll(p string) error {
	f.attempts++
	if f.attempts <= f.failures {
		return errors.New("sharing violation")
	}
	return f.Fs.RemoveAll(p)
}

func setSweepSeams(t *testing.T, liveness fsutil.ProcessLiveness, start *time.Time) {
	t.Helper()
	host, _ := os.Hostname()
	oldHost, oldLive, oldStart, oldSleep := artifactSweepHostname, artifactSweepLiveness, artifactSweepStartTime, artifactSweepSleep
	artifactSweepHostname = func() (string, error) { return host, nil }
	artifactSweepLiveness = func(int) fsutil.ProcessLiveness { return liveness }
	artifactSweepStartTime = func(int) *time.Time { return start }
	artifactSweepSleep = func(time.Duration) {}
	t.Cleanup(func() {
		artifactSweepHostname, artifactSweepLiveness, artifactSweepStartTime, artifactSweepSleep = oldHost, oldLive, oldStart, oldSleep
	})
}

func seedStagingRoot(t *testing.T, fs afero.Fs, parent string, mutate func(*artifactStageManifest)) string {
	t.Helper()
	root, err := afero.TempDir(fs, parent, artifactStageDirPrefix)
	require.NoError(t, err)
	pid := os.Getpid()
	host, _ := os.Hostname()
	manifest := artifactStageManifest{Version: artifactStageManifestVer, Token: artifactStageManifestToken(root), PID: pid, Hostname: host, CreatedAt: time.Now().UTC()}
	if mutate != nil {
		mutate(&manifest)
	}
	body, err := json.Marshal(&manifest)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, artifactStageManifestName), body, 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".source", "MIRD-281.mp4"), []byte("payload"), 0o644))
	return root
}

func TestSweepArtifactStaging_DeadOwnerReclaimed(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", nil)

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "dead owner's staging root must be reclaimed")
	entries, rerr := afero.ReadDir(fs, "/lib")
	require.NoError(t, rerr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), artifactStageDirPrefix, "no quarantine residue left behind")
	}
}

func TestSweepArtifactStaging_LiveOwnerRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	start := time.Now().Add(-time.Minute)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.ProcessStartUnixNano = start.UnixNano() })
	setSweepSeams(t, fsutil.ProcessAlive, &start)

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.True(t, exists, "live owner with matching start time must be retained")
}

func TestSweepArtifactStaging_PIDReuseReclaimed(t *testing.T) {
	fs := afero.NewMemMapFs()
	recorded := time.Now().Add(-time.Hour)
	probed := time.Now()
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.ProcessStartUnixNano = recorded.UnixNano() })
	setSweepSeams(t, fsutil.ProcessAlive, &probed)

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "a live PID whose start time differs proves reuse; reclaim")
}

func TestSweepArtifactStaging_RetainsUnverifiable(t *testing.T) {
	start := time.Now()
	testCases := []struct {
		name     string
		mutate   func(*artifactStageManifest)
		liveness fsutil.ProcessLiveness
	}{
		{"unprobeable owner", nil, fsutil.ProcessUnknown},
		{"live owner no start time", func(m *artifactStageManifest) { m.ProcessStartUnixNano = 0 }, fsutil.ProcessAlive},
		{"foreign hostname", func(m *artifactStageManifest) { m.Hostname = "other-host" }, fsutil.ProcessDead},
		{"wrong version", func(m *artifactStageManifest) { m.Version = 99 }, fsutil.ProcessDead},
		{"zero pid", func(m *artifactStageManifest) { m.PID = 0 }, fsutil.ProcessDead},
		{"token mismatch", func(m *artifactStageManifest) { m.Token = "different" }, fsutil.ProcessDead},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			root := seedStagingRoot(t, fs, "/lib", tc.mutate)
			setSweepSeams(t, tc.liveness, &start)

			sweepArtifactStaging(fs, "/lib")

			exists, _ := afero.DirExists(fs, root)
			assert.True(t, exists, "unverifiable staging roots are always retained")
		})
	}
}

func TestSweepArtifactStaging_LegacyUnstampedRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	legacy := "/lib/" + artifactStageDirPrefix + "legacy123"
	require.NoError(t, fs.MkdirAll(legacy, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(legacy, "video.mp4"), []byte("x"), 0o644))

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, legacy)
	assert.True(t, exists, "pre-manifest residue is never auto-deleted")
}

func TestSweepArtifactStaging_QuarantineRetried(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", nil)
	quarantine := root + artifactStageQuarantineMark + "ab12"
	require.NoError(t, fs.Rename(root, quarantine))

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, quarantine)
	assert.False(t, exists, "claimed quarantine roots retry removal once ownership re-proves reclaimable")
}

func TestSweepArtifactStaging_QuarantinedWithoutOwnershipRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	// A directory that merely matches prefix+marker but carries no manifest:
	// never ours to delete.
	foreign := "/lib/" + artifactStageDirPrefix + "foreign" + artifactStageQuarantineMark + "99"
	require.NoError(t, fs.MkdirAll(foreign, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(foreign, "payload.bin"), []byte("x"), 0o644))

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, foreign)
	assert.True(t, exists, "quarantine-marked foreign directories are never auto-deleted")
}

func TestArtifactStageReclaimable_FailsClosedOnUnverifiableHost(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := seedStagingRoot(t, fs, "/lib", nil)

	setSweepSeams(t, fsutil.ProcessDead, nil)
	oldHost := artifactSweepHostname
	artifactSweepHostname = func() (string, error) { return "", errors.New("hostname denied") }
	assert.False(t, artifactStageReclaimable(fs, root), "unverifiable local hostname retains the root")
	artifactSweepHostname = oldHost

	root2 := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.Hostname = "" })
	assert.False(t, artifactStageReclaimable(fs, root2), "empty recorded hostname retains the root")
}

func TestSweepArtifactStaging_IgnoresNonStagingEntries(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	require.NoError(t, fs.MkdirAll("/lib/MIRD-281", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/lib/.javinizer-other-dir", []byte("x"), 0o644))

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, "/lib/MIRD-281")
	assert.True(t, exists)
}

func TestArtifactCleanup_RetriesTransientFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	require.NoError(t, afero.WriteFile(base, "/stage/file.bin", []byte("x"), 0o644))
	fsy := &failRemoveAllFS{Fs: base, failures: 2}
	setSweepSeams(t, fsutil.ProcessDead, nil)

	stage := &artifactStage{fs: fsy, root: "/stage"}
	stage.cleanup()

	assert.Equal(t, 3, fsy.attempts, "two transient failures then success")
	exists, _ := afero.DirExists(base, "/stage")
	assert.False(t, exists)
}

func TestArtifactCleanup_RetainsOnPersistentFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	fsy := &failRemoveAllFS{Fs: base, failures: 100}
	setSweepSeams(t, fsutil.ProcessDead, nil)

	stage := &artifactStage{fs: fsy, root: "/stage"}
	stage.cleanup()

	assert.Equal(t, artifactRemoveAttempts, fsy.attempts, "bounded retries only")
	exists, _ := afero.DirExists(base, "/stage")
	assert.False(t, exists, "the root was claimed by the quarantine rename")
	entries, _ := afero.ReadDir(base, "/")
	quarantined := 0
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), artifactStageQuarantineMark) {
			quarantined++
		}
	}
	assert.Equal(t, 1, quarantined, "quarantine-named residue retained for the next sweep")
}

func TestArtifactStageManifestToken(t *testing.T) {
	root := "/lib/" + artifactStageDirPrefix + "abc123"
	assert.Equal(t, "abc123", artifactStageManifestToken(root))
	quarantined := root + artifactStageQuarantineMark + "de45"
	assert.Equal(t, "abc123", artifactStageManifestToken(quarantined), "quarantine suffix is cut from the token")
}

func TestWriteArtifactStageManifest_Roundtrip(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/stage", 0o755))
	start := time.Now().Add(-time.Minute)
	oldHost, oldStart := artifactSweepHostname, artifactSweepStartTime
	artifactSweepHostname = func() (string, error) { return "test-host", nil }
	artifactSweepStartTime = func(int) *time.Time { return &start }
	t.Cleanup(func() { artifactSweepHostname, artifactSweepStartTime = oldHost, oldStart })

	root := "/stage/" + artifactStageDirPrefix + "tok9"
	require.NoError(t, fs.MkdirAll(root, 0o755))
	writeArtifactStageManifest(fs, root)

	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	var manifest artifactStageManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	assert.Equal(t, "tok9", manifest.Token)
	assert.Equal(t, "test-host", manifest.Hostname)
	assert.Equal(t, os.Getpid(), manifest.PID)
	assert.Equal(t, start.UnixNano(), manifest.ProcessStartUnixNano)
}

func TestWriteArtifactStageManifest_MarshalFailureWarnsOnly(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := artifactSweepMarshal
	artifactSweepMarshal = func(any) ([]byte, error) { return nil, errors.New("encode denied") }
	t.Cleanup(func() { artifactSweepMarshal = old })
	require.NoError(t, fs.MkdirAll("/stage", 0o755))
	assert.NotPanics(t, func() { writeArtifactStageManifest(fs, "/stage") })
}

type failWriteFileFS struct {
	afero.Fs
}

func (f *failWriteFileFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&(os.O_WRONLY|os.O_CREATE|os.O_APPEND) != 0 {
		return nil, errors.New("write denied")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestWriteArtifactStageManifest_WriteFailureWarnsOnly(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	assert.NotPanics(t, func() { writeArtifactStageManifest(&failWriteFileFS{Fs: base}, "/stage") })
}

func TestArtifactStageReclaimable_MalformedManifestRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/stage", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/stage/"+artifactStageManifestName, []byte("{not json"), 0o600))
	assert.False(t, artifactStageReclaimable(fs, "/stage"))
}

func TestSweepArtifactStaging_LiveOwnerNilProbeStartRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	recorded := time.Now().Add(-time.Hour)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.ProcessStartUnixNano = recorded.UnixNano() })
	setSweepSeams(t, fsutil.ProcessAlive, nil)

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.True(t, exists, "live owner without probeable start time is retained")
}

func TestSweepArtifactStaging_MissingParentIgnored(t *testing.T) {
	fs := afero.NewMemMapFs()
	assert.NotPanics(t, func() { sweepArtifactStaging(fs, "/does/not/exist") })
	assert.NotPanics(t, func() { sweepArtifactStaging(nil, "/lib") })
	assert.NotPanics(t, func() { sweepArtifactStaging(fs, "  ") })
}

func TestArtifactStageQuarantineName_RandFallback(t *testing.T) {
	old := artifactSweepRand
	artifactSweepRand = func([]byte) (int, error) { return 0, errors.New("entropy denied") }
	t.Cleanup(func() { artifactSweepRand = old })
	assert.Equal(t, "/lib/x"+artifactStageQuarantineMark+"0", artifactStageQuarantineName("/lib/x"))
}

type notExistRemoveAllFS struct {
	afero.Fs
}

func (f *notExistRemoveAllFS) RemoveAll(string) error {
	return os.ErrNotExist
}

func TestRemoveArtifactTreeWithRetry_NotExistIsSuccess(t *testing.T) {
	assert.NoError(t, removeArtifactTreeWithRetry(&notExistRemoveAllFS{Fs: afero.NewMemMapFs()}, "/stage"))
}

func TestSweepArtifactStaging_RetainedWhenDeleteKeepsFailing(t *testing.T) {
	base := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, base, "/lib", nil)
	fsy := &failRemoveAllFS{Fs: base, failures: 100}

	sweepArtifactStaging(fsy, "/lib")

	entries, rerr := afero.ReadDir(base, "/lib")
	require.NoError(t, rerr)
	remaining := []string{}
	for _, e := range entries {
		if e.IsDir() {
			remaining = append(remaining, e.Name())
		}
	}
	require.Len(t, remaining, 1, "unremovable root is retained (quarantined) for a later sweep")
	assert.Contains(t, remaining[0], artifactStageQuarantineMark)
	exists, _ := afero.DirExists(base, root)
	assert.False(t, exists, "ownership claimed via quarantine rename")
}

type failRenameFS struct {
	afero.Fs
}

func (f *failRenameFS) Rename(oldname, newname string) error {
	return errors.New("quarantine claim denied")
}

func TestSweepArtifactStaging_RetainedWhenClaimRenameFails(t *testing.T) {
	base := afero.NewMemMapFs()
	root := seedStagingRoot(t, base, "/lib", nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)

	sweepArtifactStaging(&failRenameFS{Fs: base}, "/lib")

	exists, _ := afero.DirExists(base, root)
	assert.True(t, exists, "a failed claim rename retains the root untouched")
}

func TestSweepArtifactStaging_CompletedRootReclaimedDespiteLiveOwner(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessAlive, nil)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) {
		m.CompletedUnixNano = time.Now().UnixNano()
	})

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "a finished root is residue even while its owner process stays alive")
}

// A quarantined root whose in-tree manifest was consumed mid-remove re-proves
// ownership through the external sidecar; the reclaim removes both.
func TestSweepArtifactStaging_QuarantineProofFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", nil)
	markArtifactStageCompleted(fs, root)
	quarantine := root + artifactStageQuarantineMark + "zz"
	require.NoError(t, fs.Rename(root, quarantine))
	// mirror to the sidecar, then consume the in-tree manifest (partial remove)
	body, err := afero.ReadFile(fs, filepath.Join(quarantine, artifactStageManifestName))
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, artifactStageProofPath(quarantine), body, 0o600))
	require.NoError(t, fs.Remove(filepath.Join(quarantine, artifactStageManifestName)))
	require.False(t, artifactStageManifestReclaimable(fs, quarantine))
	require.True(t, artifactStageReclaimable(fs, quarantine), "sidecar re-proves ownership")

	sweepArtifactStaging(fs, "/lib")
	exists, _ := afero.DirExists(fs, quarantine)
	assert.False(t, exists)
	proofGone, _ := afero.Exists(fs, artifactStageProofPath(quarantine))
	assert.False(t, proofGone, "sidecar removed with the tree")
}

// A quarantined root without manifest AND without proof stays:
// the fallback never invents ownership.
func TestSweepArtifactStaging_QuarantineWithoutProofRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", nil)
	quarantine := root + artifactStageQuarantineMark + "qq"
	require.NoError(t, fs.Rename(root, quarantine))
	require.NoError(t, fs.Remove(filepath.Join(quarantine, artifactStageManifestName)))
	require.False(t, artifactStageReclaimable(fs, quarantine))
	sweepArtifactStaging(fs, "/lib")
	exists, _ := afero.DirExists(fs, quarantine)
	assert.True(t, exists)
}

// Proof sidecar writer has silent no-op legs: missing manifest or unreadable,
// unwritable sidecar; nothing may panic.
func TestWriteArtifactStageProofSilentLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	assert.NotPanics(t, func() { writeArtifactStageProof(fs, "/lib/absent") }, "missing manifest no-op")
	require.NoError(t, fs.MkdirAll("/lib/broken", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/lib/broken/"+artifactStageManifestName, []byte("{no"), 0o600))
	assert.NotPanics(t, func() { writeArtifactStageProof(fs, "/lib/broken") }, "broken manifest no-op")
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.CompletedUnixNano = time.Now().UnixNano() })
	writeArtifactStageProof(fs, root)
	ok, _ := afero.Exists(fs, artifactStageProofPath(root))
	require.True(t, ok, "valid manifest mirrors a proof")

	// unwritable sidecar logs but never fails
	fsy := &failWriteFileFS{Fs: fs}
	assert.NotPanics(t, func() { writeArtifactStageProof(fsy, root) })

	// marshal denial also logs without failing
	old := artifactSweepMarshal
	artifactSweepMarshal = func(any) ([]byte, error) { return nil, errors.New("encode denied") }
	t.Cleanup(func() { artifactSweepMarshal = old })
	assert.NotPanics(t, func() { writeArtifactStageProof(fs, root) })
}

func TestReadArtifactStageProofBranches(t *testing.T) {
	base := afero.NewMemMapFs()
	start := time.Now()
	setSweepSeams(t, fsutil.ProcessDead, &start)
	path := "/lib/" + artifactStageDirPrefix + "tok" + artifactStageQuarantineMark + "zz"
	require.NoError(t, base.MkdirAll(path, 0o755))
	valid := artifactStageManifest{
		Version:           artifactStageManifestVer,
		Token:             artifactStageManifestToken(path),
		PID:               os.Getpid(),
		Hostname:          mustHostname(t),
		CompletedUnixNano: 1,
	}
	write := func(mut func(*artifactStageManifest)) {
		m := valid
		if mut != nil {
			mut(&m)
		}
		body, err := json.Marshal(&m)
		require.NoError(t, err)
		require.NoError(t, afero.WriteFile(base, artifactStageProofPath(path), body, 0o600))
	}

	assert.False(t, readArtifactStageProof(base, path), "absent sidecar")
	require.NoError(t, afero.WriteFile(base, artifactStageProofPath(path), []byte("{broken"), 0o600))
	assert.False(t, readArtifactStageProof(base, path), "broken json")
	write(func(m *artifactStageManifest) { m.Version = 99 })
	assert.False(t, readArtifactStageProof(base, path), "wrong version")
	write(func(m *artifactStageManifest) { m.PID = 0 })
	assert.False(t, readArtifactStageProof(base, path), "no pid")
	write(func(m *artifactStageManifest) { m.Token = "other" })
	assert.False(t, readArtifactStageProof(base, path), "token mismatch")
	write(func(m *artifactStageManifest) { m.CompletedUnixNano = 0 })
	assert.False(t, readArtifactStageProof(base, path), "incomplete lifecycle")
	oldHost := artifactSweepHostname
	artifactSweepHostname = func() (string, error) { return "", errors.New("hostname denied") }
	write(nil)
	assert.False(t, readArtifactStageProof(base, path), "unverifiable host")
	artifactSweepHostname = func() (string, error) { return "other-host", nil }
	assert.False(t, readArtifactStageProof(base, path), "foreign host")
	artifactSweepHostname = oldHost
	write(nil)
	assert.True(t, readArtifactStageProof(base, path), "valid completed proof")
}

func mustHostname(t *testing.T) string {
	t.Helper()
	host, err := os.Hostname()
	require.NoError(t, err)
	return host
}

// The proof from an earlier cleanup failure follows the tree into quarantine,
// and a successful removal deletes both.
func TestSweepArtifactStaging_QuarantineCarriesPriorProof(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) { m.CompletedUnixNano = time.Now().UnixNano() })
	writeArtifactStageProof(fs, root)
	proofAtRoot := artifactStageProofPath(root)
	ok, _ := afero.Exists(fs, proofAtRoot)
	require.True(t, ok, "proof beside pre-quarantine root")

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, root)
	assert.False(t, exists, "root removed")
	existsProof, _ := afero.Exists(fs, proofAtRoot)
	assert.False(t, existsProof, "pre-quarantine proof removed too")
	added := []string{}
	entries, rerr := afero.ReadDir(fs, "/lib")
	require.NoError(t, rerr)
	for _, e := range entries {
		added = append(added, e.Name())
	}
	assert.Empty(t, added, "no residue of any kind after quarantine+remove")
}
