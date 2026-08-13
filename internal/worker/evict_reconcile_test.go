package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// evictWitnessJobRowsFor returns a job whose single completed row's movie
// carries `sourceURL` as its poster's effective source — arbitration reads
// commit-landed by comparing this against the witness's NewSourceURL.
func evictWitnessJobRowsFor(t *testing.T, id, sourceURL string) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/ev.mp4": {
			ResultID:      "res-ev",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: id, Poster: models.PosterState{PosterURL: sourceURL}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/ev.mp4", MovieID: id},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	return &models.Job{Results: string(payload)}
}

// The live-op sweep of a committed eviction witness is warn-only BUT the
// file survives for startup reconcile — never swept-around the wedged remove.
func TestEvictStalePosterPairWitnessSweepWedgeKeepsWitness(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-ES2"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	wp := filepath.Join(dir, ".evict-ES2-1.json")
	require.NoError(t, afero.WriteFile(base, wp, []byte("{}"), 0o644))
	fs := removeExactFailFS{Fs: base, name: ".evict-ES2-1.json"}
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-x", "ES2-1", "")
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "J-ES2"})
	m := &LockedMovieOps{pe: pe, movieID: "ES2-1"}
	m.evictStalePosterPair("ES2-1", wp)
	_, wErr := base.Stat(wp)
	assert.NoError(t, wErr, "witness retained — wedged sweep fired but no orphan")
}

// A wedged witness sweep at reconcile keeps the witness for the next startup.
// The pending-evict probe: matching fenced ids (content + name-fold), never
// by unrelated content, and read errors fail closed (like every other lane).
func TestPendingEvictWitnessCore(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-PE"
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	// miss: empty dir
	hit, err := pendingEvictWitnessCore(fs, dir, "PE-1")
	require.NoError(t, err)
	assert.False(t, hit)

	// content match with fold
	payload, _ := json.Marshal(evictWitness{OldID: "pe-1", NewSourceURL: "https://n/y.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-pe-1.json"), payload, 0o644))
	hit, err = pendingEvictWitnessCore(fs, dir, "PE-1")
	require.NoError(t, err)
	assert.True(t, hit, "content match fences")

	// non-matching → no fence
	hit2, err := pendingEvictWitnessCore(fs, dir, "OTH-9")
	require.NoError(t, err)
	assert.False(t, hit2)

	// legacy contentless witness still fences by name
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-LG-9.json"), []byte("{}"), 0o644))
	hit3, err := pendingEvictWitnessCore(fs, dir, "LG-9")
	require.NoError(t, err)
	assert.True(t, hit3)

	// read-wedge on a DB containing one witness fails closed
	fs2 := witnessOpenFailFS{Fs: fs, suffix: ".evict-pe-1.json"}
	_, err2 := pendingEvictWitnessCore(fs2, dir, "PE-1")
	require.Error(t, err2)
}

// codex cloud P2 (@snFs): a pending evict witness fences the whole admission path.
func TestPosterWitnessConflictCoreFencesPendingEviction(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-WP"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	payload, _ := json.Marshal(evictWitness{OldID: "WP-1", NewSourceURL: "https://n/w.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-WP-1.json"), payload, 0o644))
	err := posterWitnessConflictCore(fs, "/tmp", "J-WP", "WP-1")
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
}

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

// COMMITTED scenario: durable row carries the witness's NewSourceURL — evict
// the old pair and sweep the witness.
func TestReconcileEvictWitnessCompletes(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EV"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-1.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-1", NewSourceURL: "https://new-site/x.jpg"})
	wp := filepath.Join(dir, ".evict-EV-1.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(evictWitnessJobRowsFor(t, "EV-1", "https://new-site/x.jpg"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 1, n)
	_, fErr := fs.Stat(filepath.Join(dir, "EV-1-full.jpg"))
	assert.Error(t, fErr, "old full leg removed")
	_, cErr := fs.Stat(filepath.Join(dir, "EV-1.jpg"))
	assert.Error(t, cErr, "old crop removed")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}

// codex cloud P1: witness present but the durable row still carries the OLD
// source — sweep the witness record away WITHOUT touching bytes.
func TestReconcileEvictWitnessUncommittedSweepOnly(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVU2"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-9-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-9.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-9", NewSourceURL: "https://new-site/never-committed.jpg"})
	wp := filepath.Join(dir, ".evict-EV-9.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(evictWitnessJobRowsFor(t, "EV-9", "https://old-site/still.jpg"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 1, n, "witness swept (it never committed)")
	_, fErr := fs.Stat(filepath.Join(dir, "EV-9-full.jpg"))
	assert.NoError(t, fErr, "old full leg INTACT")
	_, cErr := fs.Stat(filepath.Join(dir, "EV-9.jpg"))
	assert.NoError(t, cErr)
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}

// A wedged leg removal keeps the witness for retry at next startup.
func TestReconcileEvictWitnessWedgeRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVW"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "EV-2.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-2", NewSourceURL: "https://new/y.jpg"})
	wp := filepath.Join(dir, ".evict-EV-2.json")
	require.NoError(t, afero.WriteFile(base, wp, payload, 0o644))
	wedged := selectiveFailRemoveFS{Fs: base, failSuffix: "EV-2.jpg"}
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(evictWitnessJobRowsFor(t, "EV-2", "https://new/y.jpg"), nil)
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 0, n)
	_, wErr := base.Stat(wp)
	assert.NoError(t, wErr, "witness retained for retry")
	got, rerr := afero.ReadFile(base, filepath.Join(dir, "EV-2.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "old-crop", string(got), "wedged leg untouched")
}

// An undecodable results column refuses arbitration — the witness (and the
// poster legs) stay in place until a decodable row can prove the outcome.
func TestReconcileEvictWitnessUndecodableResultsRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVD"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-3.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-3", NewSourceURL: "https://new/z.jpg"})
	wp := filepath.Join(dir, ".evict-EV-3.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: "{not-json"}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr, "witness retained — the outcome is unprovable")
	_, lErr := fs.Stat(filepath.Join(dir, "EV-3.jpg"))
	assert.NoError(t, lErr, "no leg touched without arbitration")
}

// Decoded results can carry non-nil entries whose Movie is nil (e.g. a
// failed row persisted without a movie): the arbitration scan skips them
// instead of dereferencing nil or treating them as commit evidence.
func TestReconcileEvictWitnessNilMovieEntriesSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVN"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EV-4.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-4", NewSourceURL: "https://new/n.jpg"})
	wp := filepath.Join(dir, ".evict-EV-4.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	res := map[string]*resultstore.MovieResult{
		// Nil-Movie row: must be skipped by the scan, never a crash or a match.
		"/f/no-movie.mp4": {
			ResultID:      "res-nm",
			Status:        models.JobStatusCompleted,
			FileMatchInfo: models.FileMatchInfo{Path: "/f/no-movie.mp4", MovieID: "EV-M"},
		},
		// Real row carrying the OLD source: proves the commit, so the nil-movie
		// row's skip is exercised deterministically regardless of map order.
		"/f/ev.mp4": {
			ResultID:      "res-ev",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "EV-4", Poster: models.PosterState{PosterURL: "https://old-site/still.jpg"}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/ev.mp4", MovieID: "EV-4"},
		},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(data)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 1, n, "nothing committed — witness swept after skipping the nil-movie row")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
	_, lErr := fs.Stat(filepath.Join(dir, "EV-4.jpg"))
	assert.NoError(t, lErr, "uncommitted legs untouched")
}

// A wedged witness sweep AFTER committed-leg removal keeps the witness so the
// eviction record retries at the next startup.
func TestReconcileEvictWitnessSweepErrorRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVS"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "EV-5.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EV-5", NewSourceURL: "https://new/s.jpg"})
	wp := filepath.Join(dir, ".evict-EV-5.json")
	require.NoError(t, afero.WriteFile(base, wp, payload, 0o644))
	wedged := selectiveFailRemoveFS{Fs: base, failSuffix: ".evict-EV-5.json"}
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(evictWitnessJobRowsFor(t, "EV-5", "https://new/s.jpg"), nil)
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 0, n, "sweep wedge keeps the witness for retry")
	_, wErr := base.Stat(wp)
	assert.NoError(t, wErr, "witness retained")
	_, lErr := base.Stat(filepath.Join(dir, "EV-5.jpg"))
	assert.Error(t, lErr, "committed leg was removed before the sweep wedge")
}

// An unsafe witness ID never steers any removal outside the job poster dir.
func TestReconcileEvictWitnessUnsafeIDKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVU"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/outside.txt", []byte("untouchable"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "../evil", NewSourceURL: "https://x/z.jpg"})
	wp := filepath.Join(dir, ".evict-evil.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: mocks.NewMockJobRepositoryInterface(t)}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr, "unsafe witness left in place (not trusted, not acted on)")
	got, oErr := afero.ReadFile(fs, "/tmp/posters/outside.txt")
	require.NoError(t, oErr)
	assert.Equal(t, "untouchable", string(got))
}

// Corrupt payload: nothing to commit-check against — the witness is retained.
func TestReconcileEvictWitnessCorruptKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-EVC"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	wp := filepath.Join(dir, ".evict-EV-3.json")
	require.NoError(t, afero.WriteFile(fs, wp, []byte("{not-json"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp))
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr)
}

// Without an arbiter (nil repo), an eviction witness simply stays — bytes and
// record alike survive undisturbed for the next startup.
func TestReconcileEvictWitnessNilRepoKeepsEverything(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-ENR"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ENR-1.jpg"), []byte("x"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "ENR-1", NewSourceURL: "https://n/x.jpg"})
	wp := filepath.Join(dir, ".evict-ENR-1.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp))
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr)
	_, lErr := fs.Stat(filepath.Join(dir, "ENR-1.jpg"))
	assert.NoError(t, lErr)
}

// A repo lookup failure is undecidable too — same keep-everything posture.
func TestReconcileEvictWitnessRepoErrorKeepsEverything(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-ERE"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	payload, _ := json.Marshal(evictWitness{OldID: "ERE-1", NewSourceURL: "https://n/x.jpg"})
	wp := filepath.Join(dir, ".evict-ERE-1.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, assert.AnError)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	assert.Equal(t, 0, cl.reconcileEvictWitness(context.Background(), dir, "JOB-W1", wp))
	_, wErr := fs.Stat(wp)
	assert.NoError(t, wErr)
}

// MkdirAll failure on the witness dir surfaces as an error, never hidden.
func TestWriteEvictWitnessMkdirFailureSurfaces(t *testing.T) {
	fs := mkdirWedgeFS{Fs: afero.NewMemMapFs()}
	_, err := writeEvictWitness(fs, "/tmp/posters/J-MW", "MW-1", "https://n/x.jpg", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evict witness dir")
}

// The eviction witness WRITE wedged// The eviction witness WRITE wedged → deferred (fail-closed; nothing removed).
func TestEvictStalePosterPairWitnessWriteWedgedDefers(t *testing.T) {
	mem := afero.NewMemMapFs()
	jobDir := "/tmp/posters/J-EW"
	require.NoError(t, mem.MkdirAll(jobDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(jobDir, "EV-8-full.jpg"), []byte("of"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(jobDir, "EV-8.jpg"), []byte("oc"), 0o644))
	fs := createWedgeFS{Fs: mem, contains: ".evict-"}
	_, err := writeEvictWitness(fs, jobDir, "EV-8", "https://new/q.jpg", "")
	require.Error(t, err)
	_, f1 := mem.Stat(filepath.Join(jobDir, "EV-8-full.jpg"))
	assert.NoError(t, f1, "legs untouched while the record can't persist")
}

// rename wedge: the atomic witness writer surfaces the failure and sweeps .tmp.
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

// renameTrackFS records rename destinations in order — the fold-grouping
// unwind-order oracle.
type renameTrackFS struct {
	afero.Fs
	order []string
}

func (f *renameTrackFS) Rename(old, new string) error {
	f.order = append(f.order, filepath.Base(new))
	return f.Fs.Rename(old, new)
}

// codex cloud P2: spell-variant backups of ONE logical canon collapse into a
// single fold-keyed stack; the newest op restores FIRST and the oldest LAST —
// never split stacks by spelling.
func TestReconcileParkedFoldedGroupUnwindsNewestFirst(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-FG"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "fp-1.jpg"), []byte("opB-gen"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "FP-1.jpg.rsbak.1000.1"), []byte("orig"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "fp-1.jpg.rsbak.2000.1"), []byte("opA-gen"), 0o644))
	writeMeta := func(id, nonce string) {
		meta, _ := json.Marshal(inFlightMeta{PosterID: id, PrevRevision: 4})
		require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".inflight-"+id+"."+nonce), meta, 0o644))
	}
	writeMeta("FP-1", "1000.1")
	writeMeta("fp-1", "2000.1")
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "FP-1", 4), nil)

	tracker := &renameTrackFS{Fs: mem}
	cl := &TempDirCleaner{fs: tracker, tempDir: "/tmp", jobRepo: repo}
	cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	require.Len(t, tracker.order, 2, "both backups restore")
	assert.Equal(t, "fp-1.jpg", tracker.order[0], "newest op (2000.1) restores first")
	assert.Equal(t, "FP-1.jpg", tracker.order[1], "oldest op (1000.1) restores last")
	gotOlder, err := afero.ReadFile(mem, filepath.Join(dir, "FP-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "orig", string(gotOlder))
	gotNewer, err := afero.ReadFile(mem, filepath.Join(dir, "fp-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "opA-gen", string(gotNewer))
}

// The router lane: an .evict- witness is routed to reconciler by ReconcileRekeyWitnesses.
func TestRouterRoutesEvictWitnesses(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-RT"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EVT-1-full.jpg"), []byte("of"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "EVT-1.jpg"), []byte("oc"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "EVT-1", NewSourceURL: "https://final/z.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-EVT-1.json"), payload, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "J-RT").Return(evictWitnessJobRowsFor(t, "EVT-1", "https://final/z.jpg"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	_, e1 := fs.Stat(filepath.Join(dir, "EVT-1.jpg"))
	assert.Error(t, e1, "cropped leg evicted via routed reconcile")
	_, e2 := fs.Stat(filepath.Join(dir, ".evict-EVT-1.json"))
	assert.Error(t, e2, "witness swept")
}

// Reading an eviction witness mid-watch fails closed (probe's own error arm).
func TestPendingEvictWitnessCoreReadWedgeFailsClosed(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-PE2"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-pe-2.json"), []byte("{}"), 0o644))
	wedged := witnessOpenFailFS{Fs: fs, suffix: ".evict-pe-2.json"}
	_, err := pendingEvictWitnessCore(wedged, dir, "PE-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eviction witness scan")
}

// Same wedge through the fence entry point: poster's eviction-read failure
// surfaces as the conflict-check error, never silently continues.
func TestPosterWitnessConflictCoreEvictionReadWedgeFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-WP2"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".evict-wp-2.json"), []byte("{}"), 0o644))
	wedged := witnessOpenFailFS{Fs: fs, suffix: ".evict-wp-2.json"}
	err := posterWitnessConflictCore(wedged, "/tmp", "J-WP2", "WP-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poster eviction witness check")
}

// Dir-level read failure also fails closed.
func TestPendingEvictWitnessCoreDirWedgeFailsClosed(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-PE3"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	fs := witnessOpenFailFS{Fs: mem, suffix: dir}
	_, err := pendingEvictWitnessCore(fs, dir, "PE-3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eviction witness scan")
}

// codex P2 round 8 (PR211): the witness's arbitration is scoped to its OWN
// row — a same-ID sibling that already migrated to the new source must NOT
// mark an interrupted override committed (which would evict the pair while
// the witness's row still references it).
func TestReconcileEvictWitness_ScopedToNamedRow(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-MW9"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "MW-9-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "MW-9.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "MW-9", NewSourceURL: "https://new-site/x.jpg", FilePath: "/f/own-row.mp4"})
	wp := filepath.Join(dir, ".evict-MW-9.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	res := map[string]*resultstore.MovieResult{
		// sibling row: already migrated to the new source — under legacy
		// any-row arbitration this would falsely mark the witness committed.
		"/f/sibling.mp4": {
			ResultID:      "res-sib",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "MW-9", Poster: models.PosterState{PosterURL: "https://new-site/x.jpg"}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/sibling.mp1", MovieID: "MW-9"},
		},
		// the witness's own row: still on the OLD source (commit never ran).
		"/f/own-row.mp4": {
			ResultID:      "res-own",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "MW-9", Poster: models.PosterState{PosterURL: "https://old-site/s.jpg"}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/own-row.mp4", MovieID: "MW-9"},
		},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-SC").Return(&models.Job{Results: string(data)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-SC", wp)
	assert.Equal(t, 1, n, "uncommitted witness swept")
	_, fErr := fs.Stat(filepath.Join(dir, "MW-9-full.jpg"))
	assert.NoError(t, fErr, "full leg NOT evicted — sibling migration is not the commit of this row")
	_, cErr := fs.Stat(filepath.Join(dir, "MW-9.jpg"))
	assert.NoError(t, cErr, "crop leg NOT evicted")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}

// Contrast: when the witness's OWN row carries the new source, eviction
// completes normally.
func TestReconcileEvictWitness_ScopedCompletesWhenOwnRowCommitted(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-MWC"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "MC-2-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "MC-2.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "MC-2", NewSourceURL: "https://new-site/x.jpg", FilePath: "/f/own-row.mp4"})
	wp := filepath.Join(dir, ".evict-MC-2.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	res := map[string]*resultstore.MovieResult{
		"/f/own-row.mp4": {
			ResultID:      "res-own",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "MC-2", Poster: models.PosterState{PosterURL: "https://new-site/x.jpg"}},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/own-row.mp4", MovieID: "MC-2"},
		},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-SC").Return(&models.Job{Results: string(data)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-SC", wp)
	assert.Equal(t, 1, n)
	_, fErr := fs.Stat(filepath.Join(dir, "MC-2-full.jpg"))
	assert.Error(t, fErr, "own-row commit ⇒ eviction completed")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}

// codex P2 round 9 (PR211): the persisted row carrying an EMPTY canonical ID
// shares its identity through the matcher alias (FileMatchInfo.MovieID).
// "The alias is the persistent identity" — the witness arbitration must
// accept it so a committed override's eviction completes at startup.
func TestReconcileEvictWitness_LegacyEmptyIDAliasArbitrates(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-LEG"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "LEG-9-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "LEG-9.jpg"), []byte("old-crop"), 0o644))
	payload, _ := json.Marshal(evictWitness{OldID: "LEG-9", NewSourceURL: "https://new-site/l.jpg", FilePath: "/f/leg.mp4"})
	wp := filepath.Join(dir, ".evict-LEG-9.json")
	require.NoError(t, afero.WriteFile(fs, wp, payload, 0o644))

	ownRow := map[string]*resultstore.MovieResult{
		"/f/leg.mp4": {
			ResultID:      "res-leg",
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "", Poster: models.PosterState{PosterURL: "https://new-site/l.jpg"}}, // canonical ID empty
			FileMatchInfo: models.FileMatchInfo{Path: "/f/leg.mp4", MovieID: "LEG-9"},
		},
	}
	data, err := json.Marshal(ownRow)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-LEG").Return(&models.Job{Results: string(data)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n := cl.reconcileEvictWitness(context.Background(), dir, "JOB-LEG", wp)
	assert.Equal(t, 1, n)
	_, fErr := fs.Stat(filepath.Join(dir, "LEG-9-full.jpg"))
	assert.Error(t, fErr, "committed override on alias-identified rows still evicts")
	_, wErr := fs.Stat(wp)
	assert.Error(t, wErr, "witness swept")
}
