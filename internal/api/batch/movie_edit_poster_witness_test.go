package batch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The batch-side pending-eviction probe mirrors the worker one: content,
// name-fold, fail-closed reads.
func TestPendingEvictFromDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JB-EV"
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	hit, err := pendingEvictFromDir(fs, dir, "EV-B1")
	require.NoError(t, err)
	assert.False(t, hit, "no witnesses → miss")

	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-ev-b1.json"), []byte(`{"old_id":"ev-b1"}`), 0o644))
	hit, err = pendingEvictFromDir(fs, dir, "EV-B1")
	require.NoError(t, err)
	assert.True(t, hit, "content match fences regardless of spelling")

	hit2, err := pendingEvictFromDir(fs, dir, "UNRL-9")
	require.NoError(t, err)
	assert.False(t, hit2)

	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-LG-Q.json"), []byte("{}"), 0o644))
	_, err3 := pendingEvictFromDir(fs, dir, "LG-Q")
	require.NoError(t, err3)

	hit4, err := pendingEvictFromDir(fs, "/tmp/posters/NOPE", "EV-B1")
	require.NoError(t, err)
	assert.False(t, hit4)
}

// A pending eviction witness fences the crop writer (behind errCropWitnessPending).
func TestWriteCropWitnessGuarded_EvictionFences(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JG-EV"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-ev-c1.json"), []byte(`{"old_id":"ev-c1"}`), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JG-EV", cropWitness{PosterID: "EV-C1", ResultID: "res-1", StageID: "ev-c1.crop-1"})
	require.ErrorIs(t, err, errCropWitnessPending)
	assert.Contains(t, err.Error(), "eviction witness")
}

// Same for the from-URL download guard.
func TestWritePromoteWitnessGuarded_EvictionFences(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JG-PV"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-ev-d1.json"), []byte(`{"old_id":"ev-d1"}`), 0o644))
	_, err := writePromoteWitnessGuarded(fs, "/tmp", "JG-PV", "EV-D1", "https://x", "res-1", 0, nil)
	require.ErrorIs(t, err, errPromoteWitnessPending)
	assert.Contains(t, err.Error(), "(eviction witness)")
}

// The eviction probe fail-closes on an unreadable witness-pair directory.
func TestPendingEvictFromDirUnreadableDirFailsClosed(t *testing.T) {
	fs := &brokenFS{Fs: afero.NewMemMapFs(), failOpen: func(n string) bool { return filepath.ToSlash(n) == "/tmp/posters/J-DW" }}
	require.NoError(t, fs.MkdirAll("/tmp/posters/J-DW", 0o755))
	_, err := pendingEvictFromDir(fs, "/tmp/posters/J-DW", "X-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eviction witness scan")
}

// Crop guard surfaces the probe's read fault as a fail-closed crop-scan error.
func TestWriteCropWitnessGuarded_EvictionProbeWedgeFailsClosed(t *testing.T) {
	fs := &brokenFS{Fs: afero.NewMemMapFs(), failOpen: func(n string) bool { return strings.Contains(n, ".evict-") }}
	require.NoError(t, fs.MkdirAll("/tmp/posters/JG-EC", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JG-EC/.evict-ec-1.json", []byte("{\"old_id\":\"ec-1\"}"), 0o644))
	_, err := writeCropWitnessGuarded(fs, "/tmp", "JG-EC", cropWitness{PosterID: "PI-1", ResultID: "r1", StageID: "PI-1.crop-ec"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}

// Promote guard surfaces it as the eviction-witness check error.
func TestWritePromoteWitnessGuarded_EvictionProbeWedgeFailsClosed(t *testing.T) {
	fs := &brokenFS{Fs: afero.NewMemMapFs(), failOpen: func(n string) bool { return strings.Contains(n, ".evict-") }}
	require.NoError(t, fs.MkdirAll("/tmp/posters/JG-EP", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JG-EP/.evict-ep-1.json", []byte("{\"old_id\":\"ep-1\"}"), 0o644))
	_, err := writePromoteWitnessGuarded(fs, "/tmp", "JG-EP", "PI-1", "https://x", "res-1", 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eviction witness check")
}
