package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// The eviction sweeper's failure arm: wedged repeated removals surface the
// warn path with the file retained.
func TestSweepEvictionWitnessPermanentWedgeWarns(t *testing.T) {
	base := afero.NewMemMapFs()
	wp := "/tmp/posters/J-XW/.evict-XW-1.json"
	require.NoError(t, base.MkdirAll(filepath.Dir(wp), 0o755))
	require.NoError(t, afero.WriteFile(base, wp, []byte("{}"), 0o644))
	fs := removeExactFailFS{Fs: base, name: ".evict-XW-1.json"}
	sweepEvictionWitness(fs, wp)
	_, err := base.Stat(wp)
	assert.NoError(t, err, "witness stays — it's the reconciler's remaining record")
}

// A wedged witness sweep at reconcile keeps the witness for the next startup.// A wedged witness sweep at reconcile keeps the witness for the next startup.
func TestReconcileEvictWitnessSweepWedgeKeepsWitness(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-ESW"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	payload, _ := json.Marshal(evictWitness{OldID: "ES-1"})
	wp := filepath.Join(dir, ".evict-ES-1.json")
	require.NoError(t, afero.WriteFile(mem, wp, payload, 0o644))
	// legs already absent (the evictions landed); only the witness remove wedges.
	fs := selectiveFailRemoveFS{Fs: mem, failSuffix: ".evict-ES-1.json"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	n := cl.reconcileEvictWitness(dir, wp)
	assert.Equal(t, 0, n)
	_, sErr := mem.Stat(wp)
	assert.NoError(t, sErr, "witness kept while its last sweep is wedged")
}

// writeEvictWitness surfaces atomic-write failure to the caller.
func TestWriteEvictWitnessFailsWhileAtomicWriteWedged(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EWW"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	fs := createWedgeFS{Fs: mem, contains: ".evict-"}
	_, err := writeEvictWitness(fs, dir, "EE-1")
	require.Error(t, err)
}

// codex cloud P2 (@evict): a committed PATCH's eviction outlives crashes via a// codex cloud P2 (@evict): a committed PATCH's eviction outlives crashes via a
// durable witness; startup completes its removals and sweeps the witness.
func TestReconcileEvictWitnessCompletes(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EV"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-1.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-1"})
	wp := filepath.Join(dir, ".evict-EV-1.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	n := cl.reconcileEvictWitness(dir, wp)
	assert.Equal(t, 1, n)
	_, fErr := fs.Stat(filepath.Join(dir, "EV-1-full.jpg"))
	assert.Error(t, fErr, "old full leg removed")
	_, cErr := fs.Stat(filepath.Join(dir, "EV-1.jpg"))
	assert.Error(t, cErr, "old crop removed")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}

// A wedged leg removal keeps the witness AND the partial state for the next
// startup — nothing is half-evicted silently.
func TestReconcileEvictWitnessWedgeRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVW"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "EV-2.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-2"})
	wp := filepath.Join(dir, ".evict-EV-2.json")
	require.NoError(t, afero.WriteFile(base, wp, payload, 0o644))
	wedged := selectiveFailRemoveFS{Fs: base, failSuffix: "EV-2.jpg"}
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: nil}
	n := cl.reconcileEvictWitness(dir, wp)
	assert.Equal(t, 0, n)
	_, wErr := base.Stat(wp)
	assert.NoError(t, wErr, "witness retained for retry")
	got, rerr := afero.ReadFile(base, filepath.Join(dir, "EV-2.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "old-crop", string(got), "wedged leg untouched")
}

// An unsafe witness ID never steers any removal outside the job poster dir.
func TestReconcileEvictWitnessUnsafeIDKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVU"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/outside.txt", []byte("untouchable"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "../evil"})
	wp := filepath.Join(dir, ".evict-evil.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	n := cl.reconcileEvictWitness(dir, wp)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr, "unsafe witness left in place (not trusted, not acted on)")
	got, oErr := afero.ReadFile(fs, "/tmp/posters/outside.txt")
	require.NoError(t, oErr)
	assert.Equal(t, "untouchable", string(got))
}

// Corrupt payload defers to nothing — the reconciler can never tell what it
// evicted, so it stays in place.
func TestReconcileEvictWitnessCorruptKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVC"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	wp := filepath.Join(dir, ".evict-EV-3.json")
	require.NoError(t, afero.WriteFile(fs, wp, []byte("{not-json"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileEvictWitness(dir, wp))
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr)
}

// The eviction witness WRITE failing defers the entire eviction (fail-closed;
// canonical legs untouched, nothing half-committed).
func TestEvictStalePosterPairWitnessWriteWedgedDefers(t *testing.T) {
	mem := afero.NewMemMapFs()
	jobDir := "/tmp/posters/J-EW"
	require.NoError(t, mem.MkdirAll(jobDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(jobDir, "EV-8-full.jpg"), []byte("of"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(jobDir, "EV-8.jpg"), []byte("oc"), 0o644))
	// wedged on any create of the eviction witness file:
	fs := createWedgeFS{Fs: mem, contains: ".evict-"}
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "EV-8", "")
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "J-EW"})
	m := &LockedMovieOps{pe: pe, movieID: "EV-8"}

	m.evictStalePosterPair("EV-8", "")

	for _, n := range []string{"EV-8-full.jpg", "EV-8.jpg"} {
		_, sErr := mem.Stat(filepath.Join(jobDir, n))
		assert.NoError(t, sErr, "%s untouched — witness never landed", n)
	}
	entries, _ := afero.ReadDir(mem, jobDir)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".evict-"), "no witness debris")
	}
}

// rename wedge on the atomic witness write surfaces the failure and removes
// debris tmp file.
func TestWriteFileAtomicForEvictRenameWedge(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVX"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{1: true}}
	target := filepath.Join(dir, ".evict-EVQ.json")
	err := writeFileAtomicForEvict(fs, target, []byte("{}"))
	require.Error(t, err)
	_, sErr := base.Stat(target + ".tmp")
	assert.Error(t, sErr, "tmp swept on failed rename")
}

// renameTrackFS records rename destinations in order// renameTrackFS records rename destinations in order — the fold-grouping
// unwind-order oracle.
type renameTrackFS struct {
	afero.Fs
	order []string
}

func (f *renameTrackFS) Rename(old, new string) error {
	f.order = append(f.order, filepath.Base(new))
	return f.Fs.Rename(old, new)
}

// codex cloud P2 (.819): spell-variant backups of one canonical name form ONE
// folded stack — newest-first unwind restores the winning committed bytes,
// never whichever spelling map iteration visits first. Content outcome is the
// assertion: after both crashed rewinds, the canon must hold the ORIGINAL
// pre-A bytes (B's rewind first (contains A-gen), then A's = orig on top).
func TestReconcileParkedFoldedGroupUnwindsNewestFirst(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-FG"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "fp-1.jpg"), []byte("B-gen-uncommitted"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "FP-1.jpg.rsbak.1000.1"), []byte("orig"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "fp-1.jpg.rsbak.2000.1"), []byte("A-gen"), 0o644))
	writeMeta := func(id, nonce string) {
		meta, _ := json.Marshal(inFlightMeta{PosterID: id, PrevRevision: 4})
		require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".inflight-"+id+"."+nonce), meta, 0o644))
	}
	writeMeta("FP-1", "1000.1")
	writeMeta("fp-1", "2000.1")
	// rev never advanced past either op's capture → both rewind.
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "FP-1", 4), nil)

	tracker := &renameTrackFS{Fs: mem}
	cl := &TempDirCleaner{fs: tracker, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 4, healed, "2 backups restore + their sentinels sweep")
	require.Len(t, tracker.order, 2, "both backups restore their own spelled canon")
	// codex cloud P2: mixed-spelling backups of ONE logical canon fold into a
	// single stack; newest op's restore runs FIRST, oldest LAST.
	assert.Equal(t, "fp-1.jpg", tracker.order[0], "newest op (2000.1) restores first")
	assert.Equal(t, "FP-1.jpg", tracker.order[1], "oldest op (1000.1) restores last")
	gotOlder, err := afero.ReadFile(mem, filepath.Join(dir, "FP-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "orig", string(gotOlder), "older op's backup content at its own canon")
	gotNewer, err := afero.ReadFile(mem, filepath.Join(dir, "fp-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "A-gen", string(gotNewer), "newer op's backup content at its own canon")
}

// The router lane: an .evict- witness under a job dir is routed to the
// reconciler by ReconcileRekeyWitnesses — not swallowed by other prefixes.
func TestRouterRoutesEvictWitnesses(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-RT"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EVT-1-full.jpg"), []byte("of"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EVT-1.jpg"), []byte("oc"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EVT-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-EVT-1.json"), payload, 0o644))

	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: mocks.NewMockJobRepositoryInterface(t)}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	_, e1 := fs.Stat(filepath.Join(dir, "EVT-1.jpg"))
	assert.Error(t, e1, "cropped leg evicted via routed reconcile")
	_, e2 := fs.Stat(filepath.Join(dir, ".evict-EVT-1.json"))
	assert.Error(t, e2, "witness swept")
}
