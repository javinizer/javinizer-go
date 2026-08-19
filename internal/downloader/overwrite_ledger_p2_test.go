package downloader

// POSTER-WRITE-HARDENING P3 test plumbing: overwrite flows require an armed
// revert ledger (opID + recorder) or destructive replaces are refused.

import (
	"context"
	"errors"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// armedTestLedger captures replacement records for assertion.
type armedTestLedger struct {
	mu       sync.Mutex
	records  []replacementRecord
	released []replacementRecord
	pendings []pendingRecord // entries marked restore-pending (kind-carrying, wave-19+21)
}

type replacementRecord struct {
	replacedPath string
	backupPath   string
}

// pendingRecord is a restore-pending mark WITH its wave-21 routing kind.
type pendingRecord struct {
	replacedPath string
	backupPath   string
	kind         string
}

func (l *armedTestLedger) RecordReplacement(_ context.Context, _, replacedPath, backupPath string, _ ...models.ReplacementBackupFacts) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, replacementRecord{replacedPath, backupPath})
	return nil
}

func (l *armedTestLedger) ConfirmReplacement(_ context.Context, _, _, _ string) error {
	return nil
}

// MarkReplacementRestorePendingKind records the restore-pending mark WITH
// its routing kind WITHOUT releasing the entry: it stays journaled (the
// durable row equivalent keeps it too), so tests assert both the
// untriggered release and the delivered mark+kind.
func (l *armedTestLedger) MarkReplacementRestorePendingKind(_ context.Context, _, replacedPath, backupPath, kind string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pendings = append(l.pendings, pendingRecord{replacedPath: replacedPath, backupPath: backupPath, kind: kind})
	return nil
}

func (l *armedTestLedger) getPendings() []pendingRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]pendingRecord, len(l.pendings))
	copy(out, l.pendings)
	return out
}

func (l *armedTestLedger) ReleaseReplacement(_ context.Context, _, replacedPath, backupPath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = append(l.released, replacementRecord{replacedPath: replacedPath, backupPath: backupPath})
	for i, r := range l.records {
		if r.replacedPath == replacedPath && r.backupPath == backupPath {
			l.records = append(l.records[:i], l.records[i+1:]...)
			break
		}
	}
	return nil
}

// armOverwrite arms cmd with an armed ledger so overwrite-mode flows run the
// P3 replace discipline (tests that predate the ledger gate call this).
func armOverwrite(t *testing.T, cmd DownloadCmd) (DownloadCmd, *armedTestLedger) {
	t.Helper()
	rec := &armedTestLedger{}
	cmd.OperationID = "test-op-" + t.Name()
	cmd.Recorder = rec
	return cmd, rec
}

func (l *armedTestLedger) get() []replacementRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]replacementRecord, len(l.records))
	copy(out, l.records)
	return out
}

// rejectStagedRenameFS rejects every rename FROM a staged temp name — it
// models a disk that tolerates the backup aside but refuses the final byte
// swap (the restore path must engage).
type rejectStagedRenameFS struct {
	afero.Fs
}

func (f rejectStagedRenameFS) Rename(oldname, newname string) error {
	if strings.HasSuffix(oldname, ".tmp") {
		return errors.New("staged install rejected")
	}
	return f.Fs.Rename(oldname, newname)
}

// --- P3 named contract tests (discipline landing) ---

// Destructive overwrite without an armed ledger (no opID, no recorder, or
// op-name-only) is ALWAYS refused: destination bytes preserved, the result is
// a skip, and the download's staged temp is cleaned.
func TestDownloader_OverwriteWithoutOperationRecord_IsSkippedWithWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	variants := []struct {
		name string
		op   downloadLedger
	}{
		{"unarmed (no fields)", downloadLedger{}},
		{"opID without recorder", downloadLedger{opID: "op-orphan"}},
		{"recorder without opID", downloadLedger{recorder: &armedTestLedger{}}},
		{"blank opID", downloadLedger{opID: "  ", recorder: &armedTestLedger{}}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := "/output/OLD-1-fanart.jpg"
			require.NoError(t, afero.WriteFile(fs, path, []byte("old bytes"), 0644))
			d := NewDownloader(server.Client(), fs, &Config{}, nil)
			result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil, v.op)
			require.NoError(t, err)
			assert.True(t, result.Skipped, "destructive overwrite refused")
			assert.False(t, result.Downloaded)
			got, rerr := afero.ReadFile(fs, path)
			require.NoError(t, rerr)
			assert.Equal(t, "old bytes", string(got), "existing artwork preserved")
			assertNoUniqueTemps(t, fs, "/output")
		})
	}
}

// A ledger-record failure must restore the backup (destination bytes
// unchanged), surface the error, and leave no staged temp.
type failingTestLedger struct{ err error }

func (l *failingTestLedger) RecordReplacement(context.Context, string, string, string, ...models.ReplacementBackupFacts) error {
	return l.err
}

func (l *failingTestLedger) ConfirmReplacement(context.Context, string, string, string) error {
	return nil
}

func (l *failingTestLedger) ReleaseReplacement(context.Context, string, string, string) error {
	return nil
}

func (l *failingTestLedger) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return nil
}

func TestDownloader_OverwriteLedgerRecordFailure_RestoresBackupAndFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	path := "/output/OLD-2-fanart.jpg"
	require.NoError(t, afero.WriteFile(fs, path, []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	sentinel := errors.New("ledger store wedged")
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-42", recorder: &failingTestLedger{err: sentinel}})
	require.ErrorIs(t, err, sentinel)
	assert.False(t, result.Downloaded)
	got, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Equal(t, "old bytes", string(got), "restore kept the pre-existing bytes")
	assertNoUniqueTemps(t, fs, "/output")
	entries, _ := afero.ReadDir(fs, "/output")
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".dlbak"), "no backup residue after restore: %s", e.Name())
	}
}

// Successful armed overwrite: old bytes are journaled at the
// opID-namespaced backup path and the destination carries the new bytes.
func TestDownloader_OverwriteJournalsBackupWithOldBytesRecoverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	path := "/output/OLD-3-fanart.jpg"
	require.NoError(t, afero.WriteFile(fs, path, []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	rec := &armedTestLedger{}
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-journal", recorder: rec})
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.True(t, result.Replaced)
	require.Len(t, rec.get(), 1)
	record := rec.get()[0]
	assert.Equal(t, path, record.replacedPath)
	backup, berr := afero.ReadFile(fs, record.backupPath)
	require.NoError(t, berr, "backup bytes remain at the journaled path (revert reverses)")
	assert.Equal(t, "old bytes", string(backup))
	got, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Equal(t, "new bytes", string(got))
}

// [race] Two armed concurrent overwrites on one dest: existence is
// re-classified inside the dest lock, so BOTH journal their predecessors and
// the final bytes are exactly one complete payload (never torn).
func TestDownloader_ExistedDetectionInsideDestLock(t *testing.T) {
	srvPayload := map[string][]byte{"a": []byte("payload-a-aaaaa"), "b": []byte("payload-b-bbbbb")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		key := strings.TrimPrefix(r.URL.Path, "/")
		_, _ = w.Write(srvPayload[key])
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	path := "/output/OLD-4-fanart.jpg"
	require.NoError(t, afero.WriteFile(fs, path, []byte("seed old"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	rec := &armedTestLedger{}

	var wg sync.WaitGroup
	results := make([]*DownloadResult, 2)
	errs := make([]error, 2)
	for i, key := range []string{"a", "b"} {
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			results[idx], errs[idx] = d.download(context.Background(), server.URL+"/"+k, path, MediaTypeCover, true, nil,
				downloadLedger{opID: "op-race-" + k, recorder: rec})
		}(i, key)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	assert.Len(t, rec.get(), 2, "every replacement journaled")
	for _, r := range results {
		assert.True(t, r.Downloaded)
		assert.True(t, r.Replaced, "inside-lock classification marks each writer's predecessor")
	}
	got, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, [][]byte{srvPayload["a"], srvPayload["b"]}, got, "final bytes are one complete payload")
}

func TestDownloader_SameOpDoubleOverwrite_UniqueBackups(t *testing.T) {
	// codex P3 round 1 (F1): ONE operation overwriting the same destination
	// twice must journal TWO backups at two distinct paths — with opID-only
	// naming the second rename clobbers the first backup and revert can never
	// recover the original bytes.
	newStaticMediaServer := func(body []byte) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(body)
		}))
	}
	serverA := newStaticMediaServer([]byte("gen-A"))
	defer serverA.Close()
	serverB := newStaticMediaServer([]byte("gen-B"))
	defer serverB.Close()

	fs := afero.NewMemMapFs()
	dest := "/out/DBL/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/DBL", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("original"), 0o644))

	d := NewDownloader(serverA.Client(), fs, &Config{}, nil)
	ledger := &armedTestLedger{}
	lg := downloadLedger{opID: "op-double", recorder: ledger}

	_, err := d.download(context.Background(), serverA.URL+"/poster.jpg", dest, MediaTypeCover, true, nil, lg)
	require.NoError(t, err)
	_, err = d.download(context.Background(), serverB.URL+"/poster.jpg", dest, MediaTypeCover, true, nil, lg)
	require.NoError(t, err)

	recs := ledger.get()
	require.Len(t, recs, 2)
	require.NotEqual(t, recs[0].backupPath, recs[1].backupPath,
		"stacked same-op overwrites must journal distinct backups")

	// Both backups retain THEIR generations; the second is the first install's
	// bytes, the first is the original.
	b1, err := afero.ReadFile(fs, recs[0].backupPath)
	require.NoError(t, err)
	require.Equal(t, []byte("original"), b1)
	b2, err := afero.ReadFile(fs, recs[1].backupPath)
	require.NoError(t, err)
	require.Equal(t, []byte("gen-A"), b2)
	final, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, []byte("gen-B"), final)
}

// codex P3 R5-2: a transient confirm failure must roll the install back and
// retract the journal — success is never reported against an armed entry.
func TestDownload_ConfirmFailureRollsBackAndRetracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new-bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	dest := "/output/CNF-1-poster.jpg"
	old := []byte("old-bytes")
	require.NoError(t, afero.WriteFile(fs, dest, old, 0644))

	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &confirmFailingLedger{armedTestLedger: &armedTestLedger{}, err: errors.New("db wedged")}
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-confirm-fail", recorder: rec})

	require.Error(t, err, "unconfirmed installs must not report success")
	assert.False(t, result.Downloaded)
	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	assert.Equal(t, old, got, "rollback restored pre-existing bytes")
	assert.Empty(t, rec.get(), "stale entry retracted via ReleaseReplacement")
	// R9-1: rollback copies the backup before cleanup so the destination is
	// restored without consuming the source. Once ReleaseReplacement succeeds,
	// ownership is retracted only after the backup has been removed.
	entries, _ := afero.ReadDir(fs, "/output")
	backups := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dlbak.") {
			backups++
		}
	}
	assert.Equal(t, 0, backups, "a successfully released rollback must not leak an unjournaled backup")
}

type confirmFailingLedger struct {
	*armedTestLedger
	err error
}

func (l *confirmFailingLedger) ConfirmReplacement(context.Context, string, string, string) error {
	return l.err
}

// codex P3 R9-1: confirm AND release failing together (one outage) must
// leave entry + backup + destination mutually consistent — backup intact
// on disk, entry armed and pointing at it, destination rolled back.
func TestDownload_ConfirmAndReleaseBothFail_BackupSurvivesRollback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new-bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	dest := "/output/CRF-poster.jpg"
	old := []byte("old-bytes")
	require.NoError(t, afero.WriteFile(fs, dest, old, 0644))

	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &confirmAndReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, err: errors.New("outage")}
	_, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-crf", recorder: rec})

	require.Error(t, err)
	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	assert.Equal(t, old, got, "destination rolled back to pre-existing bytes")
	recs := rec.get()
	require.Len(t, recs, 1, "armed entry persists when retract also fails")
	data, rerr := afero.ReadFile(fs, recs[0].backupPath)
	require.NoError(t, rerr, "the journaled backup file must still exist")
	assert.Equal(t, old, data)
}

// stagedWedgeFS fails OpenFile on downloader restore-staging paths.
type stagedWedgeFS struct{ afero.Fs }

func (f stagedWedgeFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".dlrstr.") {
		return nil, errors.New("staged wedge")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type confirmAndReleaseFailingLedger struct {
	*armedTestLedger
	err error
}

func (l *confirmAndReleaseFailingLedger) ConfirmReplacement(context.Context, string, string, string) error {
	return l.err
}
func (l *confirmAndReleaseFailingLedger) ReleaseReplacement(context.Context, string, string, string) error {
	return l.err
}

// copyBackupToDest never masks unreadable sources.
func TestCopyBackupToDest_ErrorsSurface(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.Error(t, copyBackupToDest(fs, "/missing.backup", "/dest"))

	require.NoError(t, afero.WriteFile(fs, "/b.bin", []byte("x"), 0o644))
	wedged := stagedWedgeFS{Fs: afero.NewMemMapFs()}
	require.NoError(t, afero.WriteFile(wedged, "/b.bin", []byte("x"), 0o644))
	require.Error(t, copyBackupToDest(wedged, "/b.bin", "/d"), "staged-open failure surfaces")
	// The staged artifact is cleaned up on error.
	entries, _ := afero.ReadDir(wedged, "/")
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlrstr.", "staged residue after copy failure")
	}
}

func TestBackupPathFormat(t *testing.T) {
	p1 := overwriteBackupPath("/a/poster.jpg", "op-1")
	p2 := overwriteBackupPath("/a/poster.jpg", "op-1")
	require.NotEqual(t, p1, p2, "same-op repeats never collide")
	require.Contains(t, p1, ".dlbak.")
	require.NotContains(t, p1, "op-1", "opIDs never leak as path segments")
}

// The set-aside failure leg: destination can't move aside → replace refused.
func TestDownload_OverwriteAsideFailureRefusesDestroy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()

	base := afero.NewMemMapFs()
	dest := "/output/ASF-poster.jpg"
	old := []byte("old")
	require.NoError(t, afero.WriteFile(base, dest, old, 0644))
	fs := rejectSpecificRenameFS{Fs: base, blockSrc: dest}
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &armedTestLedger{}
	_, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-asf", recorder: rec})
	require.Error(t, err)
	require.Contains(t, err.Error(), "set aside")
	got, rerr := afero.ReadFile(base, dest)
	require.NoError(t, rerr)
	assert.Equal(t, old, got, "pre-existing bytes intact after the refused swap")
	assert.Empty(t, rec.get(), "nothing journaled when the set-aside failed")
}

type rejectSpecificRenameFS struct {
	afero.Fs
	blockSrc string
}

func (f rejectSpecificRenameFS) Rename(src, dst string) error {
	if strings.Contains(src, f.blockSrc) || strings.Contains(dst, f.blockSrc) {
		return errors.New("rename wedged")
	}
	return f.Fs.Rename(src, dst)
}

// codex P3 R17-2 side: a destination that IS a directory must be refused —
// never rename a tree aside for a file write; nothing journals.
func TestDownload_OverwriteDirectoryDestination_RefusesTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	dest := "/output/DEST-DIR"
	require.NoError(t, fs.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest+"/keep.txt", []byte("keep"), 0o644))

	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &armedTestLedger{}
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-dir", recorder: rec})
	require.NoError(t, err)
	require.False(t, result.Downloaded)
	assert.True(t, result.Skipped, "directory destination skips instead of overwriting")
	assert.Empty(t, rec.get())
	exists, _ := afero.Exists(fs, dest+"/keep.txt")
	require.True(t, exists, "directory's contents never moved")
}

// Symlinked destinations stay links — never get journaled/overwritten.
func TestDownload_OverwriteSymlinkDestination_Refuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	fs := afero.NewOsFs()
	tmp := t.TempDir()
	realPath := filepath.Join(tmp, "real.jpg")
	linkPath := filepath.Join(tmp, "link.jpg")
	require.NoError(t, os.WriteFile(realPath, []byte("real"), 0o644))
	require.NoError(t, os.Symlink(realPath, linkPath))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()

	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &armedTestLedger{}
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", linkPath, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-sym", recorder: rec})
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.False(t, result.Downloaded)
	assert.Empty(t, rec.get())

	target, _ := os.Readlink(linkPath)
	require.NotEmpty(t, target, "link object preserved")
	body, _ := os.ReadFile(realPath)
	require.Equal(t, []byte("real"), body, "target bytes untouched")
}

// WithDestLocks scopes an isolated destination registry for tests that would
// otherwise contend with the shared one.
func TestDownload_WithDestLocks_IsolatedRegistry(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := NewDownloader(nil, fs, &Config{}, nil)
	iso := d.WithDestLocks(fsutil.NewKeyedLockRegistry())
	require.NotNil(t, iso)
	// Renaming on the isolated copy must not see writers through the shared one.
	hel1 := shareOp{fs: fs, path: "/x/a"}
	hel1.work(d)
	hel1.work(iso)
}

type shareOp struct {
	fs   afero.Fs
	path string
}

func (s shareOp) work(d *Downloader) {
	r := d.destLocks.Acquire(s.path)
	r()
}

// Armed-ledger happy path through the public Download seam covers the media
// legs the unit-level download() callers don't traverse.
func TestDownload_ArmedSuccessLegs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("poster-bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	destDir := "/dst/OAR"
	require.NoError(t, fs.MkdirAll(destDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, destDir+"/poster.jpg", []byte("old"), 0o644))

	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: false, DownloadPoster: true}, nil)
	rec := &armedTestLedger{}
	outcome, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "OAR-001", Poster: models.PosterState{CoverURL: server.URL + "/c.jpg", PosterURL: server.URL + "/p.jpg"}},
		DestDir:                destDir,
		OverwriteExistingMedia: true,
		OperationID:            "op-oar",
		Recorder:               rec,
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	found := false
	for _, r2 := range outcome.Results {
		if r2.Replaced {
			found = true
		}
	}
	if !found {
		t.Logf("results: %+v", outcome.Results)
	}
}

// Stat non-NotExist leg (e.g. permission): swap refused, no journal write.
func TestOverwrite_StatErrorLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/output/SF-poster.jpg"
	require.NoError(t, base.MkdirAll("/output", 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("x"), 0o644))

	fst := &statErrFS{Fs: base, victim: dest}
	d := NewDownloader(nil, fst, &Config{DownloadCover: true}, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("poster"))
	}))
	defer server.Close()

	rec := &armedTestLedger{}
	_, err := d.download(context.Background(), server.URL+"/p.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-stat", recorder: rec})
	require.ErrorContains(t, err, "failed to stat destination")
	assert.Empty(t, rec.get())
	content, _ := afero.ReadFile(base, dest)
	assert.Equal(t, []byte("x"), content, "untouched")
}

type statErrFS struct {
	afero.Fs
	victim string
}

func (f *statErrFS) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(name)
}

// Confirm-fail + copy rollback staging failure: compound message surfaces.
func TestDownload_ConfirmRollbackBothLegs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("gen-new"))
	}))
	defer server.Close()

	base := afero.NewMemMapFs()
	dest := "/output/CRL-poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))

	fst := &statErrFailPlusRename{Fs: base, stagedDst: dest + ".dlrstr."}
	d := NewDownloader(server.Client(), fst, &Config{DownloadCover: true}, nil)
	rec := &confirmAndReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, err: errors.New("db")}

	_, err := d.download(context.Background(), server.URL+"/c.jpg", dest, MediaTypeCover, true, nil,
		downloadLedger{opID: "op-crl", recorder: rec})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed")
	require.Contains(t, err.Error(), "rollback restore failed")
}

type statErrFailPlusRename struct {
	afero.Fs
	stagedDst string
}

func (f *statErrFailPlusRename) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.HasPrefix(name, f.stagedDst) {
		return nil, errors.New("wedge")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// The cancel-respect legs with redirected async work: the entire rollback chain terminates the leg.
func TestCopyBackupToDest_CancelRollBackLeftover(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/b", []byte("x"), 0o644))
	err := copyBackupToDest(fs, "/b", "/d")
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, "/d")
	require.NoError(t, rerr)
	require.Equal(t, []byte("x"), got)
}
