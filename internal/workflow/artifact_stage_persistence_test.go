package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestDefaultArtifactSweepSecretPathApplicationHome(t *testing.T) {
	cache := t.TempDir()
	home := t.TempDir()
	oldCache := artifactSweepUserCacheDir
	artifactSweepUserCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { artifactSweepUserCacheDir = oldCache })
	for _, tc := range []struct {
		name string
		home string
		want string
	}{
		{"configured state", home, filepath.Join(home, "cache", "Javinizer", artifactStageSecretName)},
		{"trimmed state", " " + home + " ", filepath.Join(home, "cache", "Javinizer", artifactStageSecretName)},
		{"native default", "", filepath.Join(cache, "Javinizer", artifactStageSecretName)},
		{"blank state", "   ", filepath.Join(cache, "Javinizer", artifactStageSecretName)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JAVINIZER_HOME", tc.home)
			got, err := defaultArtifactSweepSecretPath()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestArtifactSweepSecretSurvivesContainerRecreation(t *testing.T) {
	home := t.TempDir()
	media := t.TempDir()
	firstCache := t.TempDir()
	secondCache := t.TempDir()
	thirdCache := t.TempDir()
	for _, tc := range []struct {
		phase string
		cache string
	}{
		{"write", firstCache},
		{"foreign", secondCache},
		{"sweep", thirdCache},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestArtifactSweepSecretRecreationHelper$")
		cmd.Env = append(os.Environ(),
			"JAVINIZER_HOME="+home,
			"HOME="+tc.cache,
			"USERPROFILE="+tc.cache,
			"LOCALAPPDATA="+filepath.Join(tc.cache, "cache"),
			"XDG_CACHE_HOME="+filepath.Join(tc.cache, ".chromium"),
			"JAVINIZER_TEST_STAGE_ROOT="+media,
			"JAVINIZER_TEST_STAGE_PHASE="+tc.phase,
		)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s process: %s", tc.phase, output)
		require.NoError(t, os.RemoveAll(tc.cache))
	}
	keyPath := filepath.Join(home, "cache", "Javinizer", artifactStageSecretName)
	key, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.Len(t, key, artifactStageSecretBytes)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		info, err = os.Stat(filepath.Dir(keyPath))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
}

func TestArtifactSweepSecretRecreationHelper(t *testing.T) {
	phase := os.Getenv("JAVINIZER_TEST_STAGE_PHASE")
	if phase == "" {
		return
	}
	artifactSweepSecretPath = defaultArtifactSweepSecretPath
	artifactSweepSecretKey = defaultArtifactSweepSecretKey
	parent := os.Getenv("JAVINIZER_TEST_STAGE_ROOT")
	require.NotEmpty(t, parent)
	root := filepath.Join(parent, artifactStageDirPrefix+"recreated")
	fs := afero.NewOsFs()
	if phase == "write" {
		artifactSweepStartTime = func(int) *time.Time {
			started := time.Unix(1700000000, 0)
			return &started
		}
		require.NoError(t, fs.MkdirAll(root, 0o700))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "video.mp4"), []byte("payload"), 0o600))
		writeArtifactStageManifest(fs, root)
		return
	}
	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	var manifest artifactStageManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.True(t, artifactStageSealed(&manifest), "secret must survive replacement of the ephemeral container cache")
	artifactSweepLiveness = func(int) fsutil.ProcessLiveness { return fsutil.ProcessAlive }
	artifactSweepStartTime = func(int) *time.Time {
		started := time.Unix(0, manifest.ProcessStartUnixNano).Add(time.Second)
		return &started
	}
	if phase == "foreign" {
		artifactSweepHostname = func() (string, error) { return manifest.Hostname + "-foreign", nil }
	}
	sweepArtifactStaging(fs, parent)
	exists, err := afero.Exists(fs, root)
	require.NoError(t, err)
	if phase == "foreign" {
		require.True(t, exists, "persisting the key must not weaken foreign-host refusal")
	} else {
		require.Equal(t, "sweep", phase)
		require.False(t, exists, "the same instance can reclaim its authenticated dead-owner root")
	}
}

func TestArtifactSweepSecretApplicationHomeFailureDoesNotFallback(t *testing.T) {
	for _, failure := range []string{"blocked directory", "corrupt key"} {
		t.Run(failure, func(t *testing.T) {
			home := t.TempDir()
			cache := t.TempDir()
			oldPath, oldCache := artifactSweepSecretPath, artifactSweepUserCacheDir
			artifactSweepSecretPath = defaultArtifactSweepSecretPath
			artifactSweepUserCacheDir = func() (string, error) { return cache, nil }
			t.Cleanup(func() { artifactSweepSecretPath, artifactSweepUserCacheDir = oldPath, oldCache })
			fallback := filepath.Join(cache, "Javinizer", artifactStageSecretName)
			require.NoError(t, os.MkdirAll(filepath.Dir(fallback), 0o700))
			require.NoError(t, os.WriteFile(fallback, bytes.Repeat([]byte("f"), artifactStageSecretBytes), 0o600))
			if failure == "blocked directory" {
				home = filepath.Join(home, "blocked")
				require.NoError(t, os.WriteFile(home, []byte("not a directory"), 0o600))
			} else {
				keyPath := filepath.Join(home, "cache", "Javinizer", artifactStageSecretName)
				require.NoError(t, os.MkdirAll(filepath.Dir(keyPath), 0o700))
				require.NoError(t, os.WriteFile(keyPath, []byte("corrupt"), 0o600))
			}
			t.Setenv("JAVINIZER_HOME", home)
			_, err := defaultArtifactSweepSecretKey()
			require.Error(t, err, "do not silently split the signing key between persistent and ephemeral state")
		})
	}
}
