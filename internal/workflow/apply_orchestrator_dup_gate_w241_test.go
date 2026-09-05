package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// ---------------------------------------------------------------------------
// Spies for the duplicate-ownership gate (codex P1, PR #241)
// ---------------------------------------------------------------------------

// dupGateCountingMerger counts pre-organize NFO merge invocations.
type dupGateCountingMerger struct {
	applyStubNFO
	calls atomic.Int32
}

func (m *dupGateCountingMerger) MergeWithExistingNFO(movie *models.Movie, opts nfo.MergeWithExistingOptions) nfo.MergeWithExistingResult {
	m.calls.Add(1)
	return m.applyStubNFO.MergeWithExistingNFO(movie, opts)
}

// dupGateCountingDownloader counts media-download invocations.
type dupGateCountingDownloader struct {
	stubDownloader
	calls atomic.Int32
}

func (d *dupGateCountingDownloader) Download(ctx context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	d.calls.Add(1)
	return d.stubDownloader.Download(ctx, cmd)
}

// dupGateCountingNFOGen counts NFO write invocations.
type dupGateCountingNFOGen struct {
	stubNFOGen
	calls atomic.Int32
}

func (g *dupGateCountingNFOGen) ResolveAndGenerate(ctx context.Context, movie *models.Movie, destDir string, nameCfg nfo.NFONameConfig, videoPath string, tags []string) (string, error) {
	g.calls.Add(1)
	return g.stubNFOGen.ResolveAndGenerate(ctx, movie, destDir, nameCfg, videoPath, tags)
}

// dupGateCapturingRevertLog records the ApplyResults handed to the journal so
// the strict no-op surface of a skipped duplicate is assertable without a DB.
type dupGateCapturingRevertLog struct {
	noOpRevertLog
	mu        sync.Mutex
	completed []*ApplyResult
	failed    []*ApplyResult
}

func (l *dupGateCapturingRevertLog) Begin(context.Context, ApplyCmd) (OperationID, error) {
	return "dup-gate-op", nil
}

func (l *dupGateCapturingRevertLog) Complete(_ context.Context, _ OperationID, result *ApplyResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.completed = append(l.completed, result)
	return nil
}

func (l *dupGateCapturingRevertLog) CompleteFailed(_ context.Context, _ OperationID, result *ApplyResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failed = append(l.failed, result)
	return nil
}

func dupGateSkippedResult() *organizer.OrganizeResult {
	return &organizer.OrganizeResult{
		OriginalPath:     "/in/B.mkv",
		NewPath:          "/dest/shared/shared.mkv",
		FolderPath:       "/dest/shared",
		FileName:         "shared.mkv",
		Moved:            false,
		DuplicateSkipped: true,
		Warnings:         []string{"duplicate destination within batch: /dest/shared/shared.mkv already claimed by /in/A.mkv (overwrite authorized)"},
	}
}

// TestApplyOrchImpl_Execute_DuplicateSkipped_ShortCircuitsAllOutputs pins the
// codex P1 fix: once organize reports an authorized intra-batch duplicate
// skip, NO downstream output step runs — the result's FolderPath/NewPath name
// the WINNER's shared destination, so merge/download/NFO would aim at the
// winner's artifacts and journal them onto the loser's revert row. Only the
// destination owner produces ancillary outputs; the loser's completion carries
// a strict journal no-op while the duplicate warning still surfaces.
func TestApplyOrchImpl_Execute_DuplicateSkipped_ShortCircuitsAllOutputs(t *testing.T) {
	merger := &dupGateCountingMerger{}
	dl := &dupGateCountingDownloader{stubDownloader: stubDownloader{
		outcome: &downloader.DownloadOutcome{CreatedPaths: []string{"/dest/shared/poster.jpg"}},
	}}
	gen := &dupGateCountingNFOGen{stubNFOGen: stubNFOGen{resolvedPath: "/dest/shared/shared.nfo"}}
	rl := &dupGateCapturingRevertLog{}
	impl := &applyOrchImpl{
		fs:         afero.NewMemMapFs(),
		organizer:  &stubOrganizer{result: dupGateSkippedResult()},
		downloader: dl,
		nfoGen:     gen,
		nfo:        merger,
		revertLog:  rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "ABC-200", Title: "Loser Movie"},
		Match:       models.FileMatchInfo{MovieID: "ABC-200", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"},
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true, ForceUpdate: true},
		Download:    true,
		GenerateNFO: true,
	})
	require.NoError(t, err, "a skipped duplicate is a successful skip, not a failure")
	require.NotNil(t, result)

	assert.Zero(t, merger.calls.Load(), "pre-organize merge must not run for a skipped duplicate (it reads the winner's NFO)")
	assert.Zero(t, dl.calls.Load(), "download must not run — it would write into the winner's folder")
	assert.Zero(t, gen.calls.Load(), "NFO generation must not run — it would truncate the winner's NFO")

	assert.True(t, result.Steps.Organized, "the organize step reported its skip")
	assert.False(t, result.Steps.Merged)
	assert.False(t, result.Steps.DisplayTitle)
	assert.False(t, result.Steps.Downloaded)
	assert.False(t, result.Steps.NFOGenerated)
	assert.Empty(t, result.NFOPath, "the loser produces no NFO path")
	assert.Empty(t, result.DownloadPaths, "the loser produces no download paths")

	require.NotNil(t, result.OrganizeResult)
	assert.True(t, result.OrganizeResult.DuplicateSkipped)
	require.NotEmpty(t, result.OrganizeResult.Warnings, "duplicate warning surface preserved")
	assert.Contains(t, result.OrganizeResult.Warnings[0], "duplicate destination within batch")

	require.Len(t, rl.completed, 1, "success completion journals once")
	assert.Empty(t, rl.failed, "no failure completion for a successful skip")
	completed := rl.completed[0]
	assert.Empty(t, completed.NFOPath, "journaled result carries no NFO delete for the loser")
	assert.Empty(t, completed.DownloadPaths, "journaled result carries no download deletes for the loser")
	require.NotNil(t, completed.OrganizeResult)
	assert.True(t, completed.OrganizeResult.DuplicateSkipped, "journal no-op gating key survives to Complete")
}

// TestApplyOrchImpl_Execute_UnauthorizedDuplicateConflict_Unchanged pins the
// NORMAL duplicate path (no ForceUpdate): the organizer's conflict error still
// fails the apply at the organize step, runs no downstream outputs, and routes
// the failure through CompleteFailed.
func TestApplyOrchImpl_Execute_UnauthorizedDuplicateConflict_Unchanged(t *testing.T) {
	merger := &dupGateCountingMerger{}
	dl := &dupGateCountingDownloader{}
	gen := &dupGateCountingNFOGen{}
	rl := &dupGateCapturingRevertLog{}
	impl := &applyOrchImpl{
		fs:         afero.NewMemMapFs(),
		organizer:  &stubOrganizer{err: errors.New("conflicts detected: /dest/shared/shared.mkv")},
		downloader: dl,
		nfoGen:     gen,
		nfo:        merger,
		revertLog:  rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "ABC-200", Title: "Loser Movie"},
		Match:       models.FileMatchInfo{MovieID: "ABC-200", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"},
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "organization failed")
	require.NotNil(t, result)
	assert.Equal(t, "organize", result.FailedStep)
	assert.Zero(t, merger.calls.Load())
	assert.Zero(t, dl.calls.Load())
	assert.Zero(t, gen.calls.Load())
	assert.Empty(t, rl.completed)
	assert.Len(t, rl.failed, 1, "organize failure still records the failed completion")
}

// TestApplyOrchImpl_Execute_NonDuplicateFlow_Unchanged pins the gate to
// DuplicateSkipped ONLY: an ordinary moved result threads the winner's own
// folder into download/NFO exactly as before.
func TestApplyOrchImpl_Execute_NonDuplicateFlow_Unchanged(t *testing.T) {
	merger := &dupGateCountingMerger{}
	dl := &dupGateCountingDownloader{stubDownloader: stubDownloader{
		outcome: &downloader.DownloadOutcome{CreatedPaths: []string{"/dest/ABC-100/poster.jpg"}},
	}}
	gen := &dupGateCountingNFOGen{stubNFOGen: stubNFOGen{resolvedPath: "/dest/ABC-100/ABC-100.nfo"}}
	rl := &dupGateCapturingRevertLog{}
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{result: &organizer.OrganizeResult{
			OriginalPath: "/in/A.mkv",
			NewPath:      "/dest/ABC-100/ABC-100.mkv",
			FolderPath:   "/dest/ABC-100",
			Moved:        true,
		}},
		downloader: dl,
		nfoGen:     gen,
		nfo:        merger,
		revertLog:  rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "ABC-100", Title: "Winner Movie"},
		Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"},
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int32(1), merger.calls.Load())
	assert.Equal(t, int32(1), dl.calls.Load())
	assert.Equal(t, int32(1), gen.calls.Load())
	assert.True(t, result.Steps.Organized)
	assert.True(t, result.Steps.Merged)
	assert.True(t, result.Steps.DisplayTitle)
	assert.True(t, result.Steps.Downloaded)
	assert.True(t, result.Steps.NFOGenerated)
	assert.Equal(t, "/dest/ABC-100/ABC-100.nfo", result.NFOPath)
	assert.Equal(t, "/dest/ABC-100/ABC-100.mkv", gen.lastVideoPath, "NFO stream details still thread the MOVED video path")
	require.Len(t, rl.completed, 1)
	assert.Equal(t, "/dest/ABC-100/ABC-100.nfo", rl.completed[0].NFOPath, "the winner still journals its generated artifacts")
	assert.Equal(t, []string{"/dest/ABC-100/poster.jpg"}, rl.completed[0].DownloadPaths)
}

// ---------------------------------------------------------------------------
// E2E: real organizer + real NFO generator + sqlite revert log over a
// write-counting filesystem (codex P1, PR #241)
// ---------------------------------------------------------------------------

// countingWritesFs wraps a base afero.Fs and counts CONTENTFUL writes
// (Write calls delivering bytes) per slash-normalized path — the precise
// observable for "exactly one producer wrote this artifact".
type countingWritesFs struct {
	afero.Fs
	mu     sync.Mutex
	writes map[string]int
}

func newCountingWritesFs(base afero.Fs) *countingWritesFs {
	return &countingWritesFs{Fs: base, writes: map[string]int{}}
}

func (c *countingWritesFs) Create(name string) (afero.File, error) {
	f, err := c.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &countingWritesFile{File: f, owner: c, name: filepath.ToSlash(name)}, nil
}

func (c *countingWritesFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := c.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return &countingWritesFile{File: f, owner: c, name: filepath.ToSlash(name)}, nil
	}
	return f, nil
}

func (c *countingWritesFs) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes[filepath.ToSlash(path)]
}

func (c *countingWritesFs) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes[filepath.ToSlash(name)]++
}

type countingWritesFile struct {
	afero.File
	owner *countingWritesFs
	name  string
}

func (f *countingWritesFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	if n > 0 {
		f.owner.record(f.name)
	}
	return n, err
}

func (f *countingWritesFile) WriteString(s string) (int, error) {
	n, err := f.File.WriteString(s)
	if n > 0 {
		f.owner.record(f.name)
	}
	return n, err
}

// e2ePosterDownloader is a downloader that actually installs poster bytes into
// the destination it is handed — the observable analogue of the real media
// pipeline — while counting invocations.
type e2ePosterDownloader struct {
	fs    afero.Fs
	calls atomic.Int32
}

func (d *e2ePosterDownloader) Download(_ context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	d.calls.Add(1)
	target := filepath.Join(cmd.DestDir, "poster.jpg")
	if err := afero.WriteFile(d.fs, target, []byte("poster-bytes"), 0o644); err != nil {
		return nil, err
	}
	return &downloader.DownloadOutcome{CreatedPaths: []string{target}}, nil
}

// slashNormalizeW241Paths slash-normalizes a collected path slice so
// separator-native entries — the revert ledger's Roots mix DestPath verbatim
// ("/dest") with the join-derived organizer leaf ("\\dest\\shared" on
// Windows) — compare cleanly against POSIX literals.
func slashNormalizeW241Paths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.ToSlash(p)
	}
	return out
}

// TestApply_ForceUpdateDuplicateOnlyOwnerProduces_EndToEnd is the codex P1
// regression end to end: two ForceUpdate claimants prime ONE shared
// destination; the primed winner moves, the loser is demoted to
// DuplicateSkipped. Through the REAL apply pipeline with GenerateNFO +
// Download enabled, ONLY the winner may produce ancillary outputs at the
// shared destination — exactly one contentful write per artifact, with winner
// content surviving — and the loser's revert row must journal nothing, so a
// loser revert deletes nothing of the winner's.
func TestApply_ForceUpdateDuplicateOnlyOwnerProduces_EndToEnd(t *testing.T) {
	ctx := context.Background()

	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "revert.db"), LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewBatchFileOperationRepository(db)

	cfs := newCountingWritesFs(afero.NewMemMapFs())
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-w241", cfs, nil, nil, nil)
	require.NoError(t, cfs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(cfs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(cfs, "/in/B.mkv", []byte("b-bytes"), 0o644))

	org := organizer.NewOrganizer(cfs, &organizer.Config{
		FolderFormat:  "shared",
		FileFormat:    "shared",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	tracker := organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: "/in/A.mkv", TargetPath: w241Target, WillMove: true},
		{SourcePath: "/in/B.mkv", TargetPath: w241Target, WillMove: true},
	})

	gen := nfo.NewGenerator(cfs, nil)
	dl := &e2ePosterDownloader{fs: cfs}
	// One orchestrator-shaped instance per claimant — production workers each
	// execute the same pipeline concurrently over the shared components.
	newApply := func() *applyOrchImpl {
		return &applyOrchImpl{
			fs:         cfs,
			organizer:  org,
			downloader: dl,
			nfoGen:     gen,
			revertLog:  rl,
			applyCfg: ApplyConfig{
				// Force BOTH movies onto ONE NFO target: the winner's and
				// loser's NFO resolve to /dest/shared/shared.nfo.
				NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "shared"},
			},
		}
	}
	cmdFor := func(movieID, src, name, title string) ApplyCmd {
		return ApplyCmd{
			Movie:       &models.Movie{ID: movieID, Title: title},
			Match:       models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			DestPath:    "/dest",
			Organize:    OrganizeOptions{MoveFiles: true, ForceUpdate: true, DuplicateTracker: tracker},
			Download:    true,
			GenerateNFO: true,
		}
	}

	// Run both claimants through the full pipeline: winner first, then the
	// loser against the SETTLED claim. The loser's skip is claim-state-driven,
	// not timing-driven — pre-fix its downstream steps would still write and
	// truncate /dest/shared/shared.nfo after the winner's write (a second
	// contentful write + a generated-file journal entry on the loser's row),
	// which is exactly what the assertions below forbid.
	winnerRes, winnerErr := newApply().Execute(ctx, cmdFor("ABC-100", "/in/A.mkv", "A.mkv", "Winner Movie"))
	loserRes, loserErr := newApply().Execute(ctx, cmdFor("ABC-200", "/in/B.mkv", "B.mkv", "Loser Movie"))

	require.NoError(t, winnerErr)
	require.NoError(t, loserErr, "the authorized duplicate is a successful skip")
	require.NotNil(t, winnerRes)
	require.NotNil(t, loserRes)

	// Winner: full pipeline ran.
	require.NotNil(t, winnerRes.OrganizeResult)
	assert.True(t, winnerRes.OrganizeResult.Moved)
	assert.False(t, winnerRes.OrganizeResult.DuplicateSkipped)
	assert.True(t, winnerRes.Steps.NFOGenerated)
	assert.True(t, winnerRes.Steps.Downloaded)

	// Loser: organize skipped, EVERY downstream output step gated off.
	require.NotNil(t, loserRes.OrganizeResult)
	assert.True(t, loserRes.OrganizeResult.DuplicateSkipped)
	assert.False(t, loserRes.OrganizeResult.Moved)
	require.NotEmpty(t, loserRes.OrganizeResult.Warnings, "duplicate warning surface preserved end to end")
	assert.True(t, loserRes.Steps.Organized)
	assert.False(t, loserRes.Steps.Downloaded, "the loser installs no shared media")
	assert.False(t, loserRes.Steps.NFOGenerated, "the loser writes no shared NFO")
	assert.Empty(t, loserRes.NFOPath)
	assert.Empty(t, loserRes.DownloadPaths)

	// (a) Exactly ONE contentful write per shared artifact — the winner's.
	nfoPath := "/dest/shared/shared.nfo"
	assert.Equal(t, 1, cfs.count(nfoPath), "exactly one producer wrote the shared NFO (no concurrent truncation)")
	assert.Equal(t, 1, cfs.count("/dest/shared/poster.jpg"), "exactly one producer installed the shared media")
	assert.Equal(t, int32(1), dl.calls.Load(), "the downloader ran only for the destination owner")
	nfoBytes, err := afero.ReadFile(cfs, nfoPath)
	require.NoError(t, err)
	assert.Contains(t, string(nfoBytes), "Winner Movie", "the surviving NFO is the winner's")
	assert.NotContains(t, string(nfoBytes), "Loser Movie", "no loser content ever landed at the shared destination")
	videoBytes, err := afero.ReadFile(cfs, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), videoBytes)

	// Journal truth: winner rows the artifacts; loser rows a strict no-op.
	rows, err := repo.FindByBatchJobID(ctx, "job-w241")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var winnerRow, loserRow *models.BatchFileOperation
	for i := range rows {
		switch rows[i].MovieID {
		case "ABC-100":
			winnerRow = &rows[i]
		case "ABC-200":
			loserRow = &rows[i]
		}
	}
	require.NotNil(t, winnerRow)
	require.NotNil(t, loserRow)
	assert.Equal(t, w241Target, filepath.ToSlash(winnerRow.NewPath))
	winnerLedger, err := models.ParseGeneratedFiles(winnerRow.GeneratedFiles)
	require.NoError(t, err)
	assert.Contains(t, winnerLedger.Delete, nfoPath, "the winner journals its own NFO")
	assert.Contains(t, slashNormalizeW241Paths(winnerLedger.Roots), filepath.ToSlash("/dest/shared"))
	assert.Empty(t, loserRow.NewPath, "the loser journals no primary move")
	loserLedger, err := models.ParseGeneratedFiles(loserRow.GeneratedFiles)
	require.NoError(t, err)
	assert.Empty(t, loserLedger.Delete, "the loser journals NO generated-file deletes — reverting it deletes nothing")
	assert.Empty(t, loserLedger.MoveBack)
	assert.NotContains(t, slashNormalizeW241Paths(loserLedger.Roots), filepath.ToSlash("/dest/shared"))

	// codex P2 (PR #241 F2): the loser's strict no-op row FINALIZED as
	// completed-noop instead of lingering applied-with-empty-NewPath.
	assert.Equal(t, models.RevertStatusNoOp, loserRow.RevertStatus)

	// (b) Loser revert after success deletes NOTHING of the winner's — the
	// completed-noop row is excluded from revert selection entirely.
	reverter := history.NewReverter(cfs, repo)
	rb, err := reverter.RevertScrape(ctx, "job-w241", "ABC-200")
	require.NoError(t, err)
	assert.Empty(t, rb.Outcomes, "a strict no-op row is never a revert subject")
	assert.Zero(t, rb.Total)
	for _, kept := range []string{nfoPath, "/dest/shared/poster.jpg", w241Target} {
		_, statErr := cfs.Stat(filepath.FromSlash(kept))
		assert.NoError(t, statErr, "loser revert left the winner's artifact in place: %s", kept)
	}
	keptNFO, err := afero.ReadFile(cfs, filepath.FromSlash(nfoPath))
	require.NoError(t, err)
	assert.Contains(t, string(keptNFO), "Winner Movie")
	loserSrc, err := afero.ReadFile(cfs, "/in/B.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("b-bytes"), loserSrc, "the loser's untouched source survives its own revert")
}

// completeErrWithOpIDLog arms Begin with a real opID (unlike the no-op
// embedding) so the success-path Complete failure branch — warn-through,
// apply still succeeds — is exercised for real.
type completeErrWithOpIDLog struct {
	noOpRevertLog
	err error
}

func (l completeErrWithOpIDLog) Begin(context.Context, ApplyCmd) (OperationID, error) {
	return "dup-gate-op-completeerr", nil
}

func (l completeErrWithOpIDLog) Complete(context.Context, OperationID, *ApplyResult) error {
	return l.err
}

func TestApplyOrchImpl_Execute_CompleteFailureIsWarnedThrough(t *testing.T) {
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		revertLog: completeErrWithOpIDLog{err: errors.New("journal write refused")},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	})
	assert.NoError(t, err, "a Complete failure warns but never fails the apply")
	require.NotNil(t, result)
	assert.Equal(t, OperationID("dup-gate-op-completeerr"), result.OperationID)
}

// TestApply_OrganizeSkipped_GateNeverTrips guards the gate's nil boundary:
// with organize skipped there is no OrganizeResult, so the pipeline runs
// normally (organize-less flows must not be mistaken for duplicate skips).
func TestApply_OrganizeSkipped_GateNeverTrips(t *testing.T) {
	gen := &dupGateCountingNFOGen{stubNFOGen: stubNFOGen{resolvedPath: "/dest/ABC-100.nfo"}}
	impl := &applyOrchImpl{
		fs:     afero.NewMemMapFs(),
		nfoGen: gen,
		nfo:    &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "ABC-100", Title: "Solo"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.Organized, "organize stayed skipped")
	assert.Equal(t, int32(1), gen.calls.Load(), "downstream outputs still run without an organize result")
}
