package downloader

// POSTER-WRITE-HARDENING P3 test plumbing: overwrite flows require an armed
// revert ledger (opID + recorder) or destructive replaces are refused.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// armedTestLedger captures replacement records for assertion.
type armedTestLedger struct {
	mu      sync.Mutex
	records []replacementRecord
}

type replacementRecord struct {
	replacedPath string
	backupPath   string
}

func (l *armedTestLedger) RecordReplacement(_ context.Context, _, replacedPath, backupPath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, replacementRecord{replacedPath, backupPath})
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

func (l *failingTestLedger) RecordReplacement(context.Context, string, string, string) error {
	return l.err
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
