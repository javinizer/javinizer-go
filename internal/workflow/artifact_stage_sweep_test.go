package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	setSweepSeams(t, fsutil.ProcessAlive, nil)
	root := seedStagingRoot(t, fs, "/lib", nil)
	quarantine := root + artifactStageQuarantineMark + "ab12"
	require.NoError(t, fs.Rename(root, quarantine))

	sweepArtifactStaging(fs, "/lib")

	exists, _ := afero.DirExists(fs, quarantine)
	assert.False(t, exists, "claimed quarantine roots retry removal regardless of liveness")
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
	assert.True(t, exists, "residue retained for the next sweep")
}
