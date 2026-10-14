package history

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var errPrimaryUndoFault = errors.New("synthetic primary Undo fault")

type primaryUndoFaultFile struct {
	afero.File
	fail      func(string, int) error
	calls     map[string]int
	afterRead func()
}

func (f *primaryUndoFaultFile) fault(op string) error {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[op]++
	if f.fail != nil {
		return f.fail(op, f.calls[op])
	}
	return nil
}
func (f *primaryUndoFaultFile) Stat() (os.FileInfo, error) {
	if e := f.fault("stat"); e != nil {
		return nil, e
	}
	return f.File.Stat()
}
func (f *primaryUndoFaultFile) Read(p []byte) (int, error) {
	if e := f.fault("read"); e != nil {
		return 0, e
	}
	n, e := f.File.Read(p)
	if f.afterRead != nil {
		f.afterRead()
	}
	return n, e
}
func (f *primaryUndoFaultFile) Write(p []byte) (int, error) {
	if e := f.fault("write"); e != nil {
		return 0, e
	}
	return f.File.Write(p)
}
func (f *primaryUndoFaultFile) Truncate(n int64) error {
	if e := f.fault("truncate"); e != nil {
		return e
	}
	return f.File.Truncate(n)
}
func (f *primaryUndoFaultFile) Close() error {
	fault := f.fault("close")
	err := f.File.Close()
	if fault != nil {
		return fault
	}
	return err
}
func (f *primaryUndoFaultFile) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }

type primaryUndoFaultFS struct {
	afero.Fs
	open  func(string, int, afero.File) (afero.File, error)
	lstat func(string) error
}

func (f *primaryUndoFaultFS) OpenFile(p string, flags int, mode os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(p, flags, mode)
	if err != nil {
		return nil, err
	}
	if f.open != nil {
		wrapped, err := f.open(p, flags, file)
		if err != nil {
			file.Close()
		}
		return wrapped, err
	}
	return file, nil
}
func (f *primaryUndoFaultFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	if f.lstat != nil {
		if err := f.lstat(p); err != nil {
			return nil, true, err
		}
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}

func (f *primaryUndoFixture) pause(state string) *models.PrimaryUndo {
	f.t.Helper()
	bad := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: state, after: true}
	require.Error(f.t, beginPrimaryUndo(context.Background(), f.fs, bad, f.op))
	require.True(f.t, bad.fired)
	return f.journal()
}
func installPrimaryUndoMetadataUnwrap(t *testing.T) {
	t.Helper()
	old := primaryUndoPrepareStage
	t.Cleanup(func() { primaryUndoPrepareStage = old })
	primaryUndoPrepareStage = func(_ afero.Fs, file afero.File, info os.FileInfo) error {
		if wrapped, ok := file.(*primaryUndoFaultFile); ok {
			file = wrapped.File
		}
		return fsutil.SyncStagedMetadata(afero.NewOsFs(), file, info)
	}
}

func TestPrimaryUndoIdentityAdmissionRefusals(t *testing.T) {
	fs := afero.NewMemMapFs()
	file, e := fs.Create("weak")
	require.NoError(t, e)
	defer file.Close()
	_, _, e = primaryUndoIdentity(file)
	require.ErrorContains(t, e, "strong")
	wrapped := &primaryUndoFaultFile{File: file, fail: func(string, int) error { return errPrimaryUndoFault }}
	_, _, e = primaryUndoIdentity(wrapped)
	require.ErrorIs(t, e, errPrimaryUndoFault)
	dir, e := afero.NewOsFs().Open(t.TempDir())
	require.NoError(t, e)
	defer dir.Close()
	_, _, e = primaryUndoIdentity(dir)
	require.ErrorContains(t, e, "regular")
	f := newPrimaryUndoFixture(t)
	id := models.PrimaryUndoIdentity{Strong: true}
	_, e = primaryUndoOpenPinned(f.fs, t.TempDir(), id, true)
	require.ErrorContains(t, e, "regular")
	opener := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = opener })
	restoreOpenReplacementSource = func(afero.Fs, string) (afero.File, error) { return nil, errPrimaryUndoFault }
	_, e = primaryUndoOpenPinned(f.fs, f.op.NewPath, id, true)
	require.ErrorIs(t, e, errPrimaryUndoFault)
	require.ErrorIs(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op), errPrimaryUndoFault)
}

func TestPrimaryUndoDigestFaults(t *testing.T) {
	for _, fault := range []string{"read", "post-read-stat", "changed-content", "post-read-name", "final-close"} {
		t.Run(fault, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("sealed")
			open := restoreOpenReplacementSource
			t.Cleanup(func() { restoreOpenReplacementSource = open })
			calls := 0
			restoreOpenReplacementSource = func(fs afero.Fs, p string) (afero.File, error) {
				file, e := open(fs, p)
				if e != nil {
					return nil, e
				}
				calls++
				if fault == "post-read-name" && calls == 2 {
					file.Close()
					return nil, errPrimaryUndoFault
				}
				at := calls
				return &primaryUndoFaultFile{File: file, fail: func(op string, n int) error {
					if fault == "read" && op == "read" || fault == "post-read-stat" && op == "stat" && n == 2 || fault == "final-close" && at == 2 && op == "close" {
						return errPrimaryUndoFault
					}
					return nil
				}}, nil
			}
			digest := j.SHA256
			if fault == "changed-content" {
				digest = "wrong"
			}
			require.Error(t, primaryUndoVerify(f.fs, j.Stage, j.StageIdentity, digest))
			require.FileExists(t, j.Source)
			require.FileExists(t, j.Stage)
		})
	}
}

func TestPrimaryUndoJournalAndShapeRefusals(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	f.op.InPlaceRenamed = true
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	f.op.InPlaceRenamed = false
	random := primaryUndoRandom
	t.Cleanup(func() { primaryUndoRandom = random })
	primaryUndoRandom = bytes.NewReader(nil)
	require.Error(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	primaryUndoRandom = random
	require.NoError(t, f.db.Model(&models.BatchFileOperation{}).Where("id = ?", f.op.ID).Update("generated_files", "{bad").Error)
	next := &models.PrimaryUndo{State: "planned"}
	require.Error(t, primaryUndoWrite(context.Background(), f.repo, f.op, nil, next))
	require.NoError(t, f.db.Model(&models.BatchFileOperation{}).Where("id = ?", f.op.ID).Update("generated_files", "{}").Error)
	require.ErrorContains(t, primaryUndoWrite(context.Background(), f.repo, f.op, next, next), "concurrently")
	j := f.pause("sealed")
	for _, change := range []func(*models.PrimaryUndo){func(j *models.PrimaryUndo) { j.Token = "bad" }, func(j *models.PrimaryUndo) { j.Cleanup = "foreign" }, func(j *models.PrimaryUndo) { j.Cleanup = j.Source + ".undo-remove." + strings.Repeat("x", 32) }} {
		copy := *j
		change(&copy)
		require.Error(t, validatePrimaryUndo(f.op, &copy))
		require.Error(t, resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, &copy))
	}
	copy := *j
	copy.State = "unknown"
	require.ErrorContains(t, resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, &copy), "unknown")
	require.ErrorContains(t, cleanupPrimaryUndo(context.Background(), f.fs, f.repo, f.op, &copy), "")
}

func TestPrimaryUndoCopyBoundaryFaults(t *testing.T) {
	cases := []struct {
		name, state, target, op string
		at                      int
	}{
		{"source-second-stat", "planned", "source", "stat", 2},
		{"new-stage-stat", "planned", "stage", "stat", 1},
		{"stage-write", "planned", "stage", "write", 1},
		{"source-read", "planned", "source", "read", 1},
		{"source-postcopy-stat", "planned", "source", "stat", 3},
		{"stage-postcopy-stat", "planned", "stage", "stat", 2},
		{"stage-close", "planned", "stage", "close", 1},
		{"resume-truncate", "copying", "stage", "truncate", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause(tc.state)
			installPrimaryUndoMetadataUnwrap(t)
			fs := &primaryUndoFaultFS{Fs: f.fs, open: func(p string, _ int, file afero.File) (afero.File, error) {
				target := ""
				if p == j.Source {
					target = "source"
				}
				if p == j.Stage {
					target = "stage"
				}
				if target != tc.target {
					return file, nil
				}
				return &primaryUndoFaultFile{File: file, fail: func(op string, n int) error {
					if op == tc.op && n == tc.at {
						return errPrimaryUndoFault
					}
					return nil
				}}, nil
			}}
			require.Error(t, copyPrimaryUndo(context.Background(), fs, f.repo, f.op, j))
			require.FileExists(t, j.Source)
			require.NoFileExists(t, j.Destination)
		})
	}
}

func TestPrimaryUndoSyncFailuresRetainRecoverability(t *testing.T) {
	for _, at := range []int{1, 2, 3} {
		t.Run(string(rune('0'+at)), func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			old := primaryUndoSyncParent
			t.Cleanup(func() { primaryUndoSyncParent = old })
			calls := 0
			primaryUndoSyncParent = func(path string) error {
				calls++
				if calls == at {
					return errPrimaryUndoFault
				}
				return old(path)
			}
			require.ErrorIs(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op), errPrimaryUndoFault)
			j := f.journal()
			require.NotNil(t, j)
			if at < 3 {
				require.FileExists(t, j.Source)
			} else {
				require.FileExists(t, j.Destination)
			}
		})
	}
}

func TestPrimaryUndoOtherCopyRefusals(t *testing.T) {
	for _, kind := range []string{"changed-source", "metadata", "resume-missing-stage", "initial-weak-source"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			if kind == "initial-weak-source" {
				fs := afero.NewMemMapFs()
				require.NoError(t, fs.MkdirAll("/library", 0755))
				require.NoError(t, afero.WriteFile(fs, "/library/movie", []byte("x"), 0600))
				f.op.NewPath = "/library/movie"
				f.op.OriginalPath = "/incoming/movie"
				require.ErrorContains(t, beginPrimaryUndo(context.Background(), fs, f.repo, f.op), "strong")
				return
			}
			state := "planned"
			if kind == "resume-missing-stage" {
				state = "copying"
			}
			j := f.pause(state)
			if kind == "resume-missing-stage" {
				require.NoError(t, os.Remove(j.Stage))
				require.Error(t, copyPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
				return
			}
			if kind == "metadata" {
				old := primaryUndoPrepareStage
				t.Cleanup(func() { primaryUndoPrepareStage = old })
				primaryUndoPrepareStage = func(afero.Fs, afero.File, os.FileInfo) error { return errPrimaryUndoFault }
				require.ErrorIs(t, copyPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j), errPrimaryUndoFault)
				return
			}
			old := restoreOpenReplacementSource
			t.Cleanup(func() { restoreOpenReplacementSource = old })
			changed := false
			restoreOpenReplacementSource = func(fs afero.Fs, p string) (afero.File, error) {
				file, e := old(fs, p)
				if e != nil {
					return nil, e
				}
				return &primaryUndoFaultFile{File: file, afterRead: func() {
					if !changed {
						changed = true
						require.NoError(t, os.Chtimes(p, junkTime, junkTime))
					}
				}}, nil
			}
			require.ErrorContains(t, copyPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j), "source changed")
		})
	}
}

var junkTime = func() time.Time { return time.Unix(1800000000, 0) }()

func TestPrimaryUndoPublishedRecoveryRefusals(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("sealed")
	require.NoError(t, os.Link(j.Stage, j.Destination))
	require.ErrorContains(t, resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, j), "stage remains")
	require.FileExists(t, j.Source)
	require.FileExists(t, j.Stage)
	require.FileExists(t, j.Destination)
}

func TestPrimaryUndoRemainingResumeRefusals(t *testing.T) {
	t.Run("occupied-copy-destination", func(t *testing.T) {
		f := newPrimaryUndoFixture(t)
		j := f.pause("planned")
		require.NoError(t, os.WriteFile(j.Destination, []byte("foreign"), 0600))
		require.Error(t, resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
	})
	t.Run("sealed-indeterminate-destination", func(t *testing.T) {
		f := newPrimaryUndoFixture(t)
		j := f.pause("sealed")
		fs := &primaryUndoFaultFS{Fs: f.fs, lstat: func(p string) error {
			if p == j.Destination {
				return errPrimaryUndoFault
			}
			return nil
		}}
		require.ErrorIs(t, resumePrimaryUndo(context.Background(), fs, f.repo, f.op, j), errPrimaryUndoFault)
	})
	t.Run("copy-source-unreadable", func(t *testing.T) {
		f := newPrimaryUndoFixture(t)
		j := f.pause("planned")
		require.NoError(t, os.Remove(j.Source))
		require.Error(t, copyPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
	})
}

func TestPrimaryUndoCleanupRecoveryRefusals(t *testing.T) {
	for _, kind := range []string{"source-reappeared-after-cleanup", "sync-retry", "both-endpoints", "invalid-state", "source-reappeared-before-cleanup", "entropy"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("published")
			switch kind {
			case "source-reappeared-after-cleanup", "sync-retry":
				j.State = "unlinking"
				j.CleanupFrom = j.Source + ".undo-remove." + strings.Repeat("a", 32)
				j.Cleanup = j.Source + ".undo-remove." + strings.Repeat("b", 32)
				if kind == "sync-retry" {
					require.NoError(t, os.Remove(j.Source))
					old := primaryUndoSyncParent
					t.Cleanup(func() { primaryUndoSyncParent = old })
					primaryUndoSyncParent = func(string) error { return errPrimaryUndoFault }
				}
			case "both-endpoints":
				j.State = "unlinking"
				j.CleanupFrom = j.Source
				j.Cleanup = j.Source + ".undo-remove." + strings.Repeat("a", 32)
				require.NoError(t, os.WriteFile(j.Cleanup, []byte("foreign"), 0600))
			case "invalid-state":
				j.State = "invalid"
			case "source-reappeared-before-cleanup":
				j.State = "unlinking"
				j.CleanupFrom = j.Source + ".undo-remove." + strings.Repeat("a", 32)
				j.Cleanup = j.Source + ".undo-remove." + strings.Repeat("b", 32)
				require.NoError(t, os.Rename(j.Source, j.Cleanup))
				require.NoError(t, os.WriteFile(j.Source, []byte("foreign"), 0600))
			case "entropy":
				old := primaryUndoRandom
				t.Cleanup(func() { primaryUndoRandom = old })
				primaryUndoRandom = bytes.NewReader(nil)
			}
			require.Error(t, cleanupPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
			require.FileExists(t, j.Destination)
		})
	}
}

func TestPrimaryUndoFinalTakeRefusesForeignMutations(t *testing.T) {
	for _, kind := range []string{"destination-after-journal", "terminal-substitute", "terminal-close", "destination-close", "source-reoccupied"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("published")
			repo := database.BatchFileOperationRepositoryInterface(f.repo)
			publish := primaryUndoPublish
			open := restoreOpenReplacementSource
			t.Cleanup(func() { primaryUndoPublish = publish; restoreOpenReplacementSource = open })
			if kind == "destination-after-journal" {
				repo = &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "unlinking", onlyHook: true, hook: func(*models.PrimaryUndo) {
					require.NoError(t, os.Rename(j.Destination, j.Destination+".owner"))
					require.NoError(t, os.WriteFile(j.Destination, []byte("foreign"), 0600))
				}}
			}
			if kind == "terminal-substitute" || kind == "source-reoccupied" {
				primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
					err := publish(fs, src, dst)
					if err != nil {
						return err
					}
					if kind == "terminal-substitute" {
						require.NoError(t, os.Rename(dst, dst+".owner"))
						require.NoError(t, os.WriteFile(dst, []byte("foreign"), 0600))
					} else {
						require.NoError(t, os.WriteFile(src, []byte("foreign"), 0600))
					}
					return nil
				}
			}
			if kind == "terminal-close" || kind == "destination-close" {
				destOpens := 0
				restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
					file, e := open(fs, path)
					if e != nil {
						return nil, e
					}
					if path == j.Destination {
						destOpens++
					}
					fail := (kind == "terminal-close" && strings.Contains(path, ".undo-remove.")) || (kind == "destination-close" && path == j.Destination && destOpens == 5)
					if !fail {
						return file, nil
					}
					return &primaryUndoFaultFile{File: file, fail: func(op string, _ int) error {
						if op == "close" {
							return errPrimaryUndoFault
						}
						return nil
					}}, nil
				}
			}
			require.Error(t, cleanupPrimaryUndo(context.Background(), f.fs, repo, f.op, j))
			if kind == "destination-after-journal" {
				require.FileExists(t, j.Source)
			} else {
				require.FileExists(t, j.Cleanup)
			}
		})
	}
}

func TestPrimaryUndoPOSIXSyncMissingParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses write-through moves instead of directory fsync")
	}
	require.Error(t, primaryUndoSyncDir(filepath.Join(t.TempDir(), "missing", "file")))
}
