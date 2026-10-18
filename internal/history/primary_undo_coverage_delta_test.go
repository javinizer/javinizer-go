package history

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrimaryUndoLateHandleStatFailuresRetainData(t *testing.T) {
	for _, phase := range []string{"hash-identity", "hash-witness", "copy-identity", "copy-witness"} {
		t.Run(phase, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			state := "sealed"
			if strings.HasPrefix(phase, "copy") {
				state = "planned"
			}
			j := f.pause(state)
			open := restoreOpenReplacementSource
			t.Cleanup(func() { restoreOpenReplacementSource = open })
			target, failAt := j.Stage, 3
			switch phase {
			case "hash-witness":
				failAt = 4
			case "copy-identity":
				target = j.Source
				failAt = 4
			case "copy-witness":
				target = j.Source
				failAt = 5
			}
			fired := false
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				file, err := open(fs, path)
				if err != nil {
					return nil, err
				}
				if path != target {
					return file, nil
				}
				return &primaryUndoFaultFile{File: file, fail: func(op string, n int) error {
					if op == "stat" && n == failAt {
						fired = true
						return errPrimaryUndoFault
					}
					return nil
				}}, nil
			}
			var err error
			if state == "sealed" {
				err = primaryUndoVerify(context.Background(), f.fs, j.Stage, j.StageIdentity, j.SHA256)
			} else {
				err = copyPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j)
			}
			require.ErrorIs(t, err, errPrimaryUndoFault)
			require.True(t, fired)
			require.FileExists(t, j.Source)
			require.NoFileExists(t, j.Destination)
			bytes, err := os.ReadFile(j.Source)
			require.NoError(t, err)
			require.Equal(t, "synthetic primary video", string(bytes))
		})
	}
}

func TestPrimaryUndoNamedWitnessRestampRefusesStage(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("sealed")
	info, err := os.Stat(j.Stage)
	require.NoError(t, err)
	replacement := []byte("new media bytes!!!!!!!!")
	require.Equal(t, int64(len(replacement)), info.Size())
	open := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = open })
	opens, mutations := 0, 0
	restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
		file, err := open(fs, path)
		if err != nil {
			return nil, err
		}
		if path != j.Stage {
			return file, nil
		}
		opens++
		if opens != 2 {
			return file, nil
		}
		return &primaryUndoFaultFile{File: file, fail: func(op string, n int) error {
			if op == "stat" && n == 2 {
				time.Sleep(time.Millisecond)
				require.NoError(t, os.WriteFile(j.Stage, replacement, info.Mode().Perm()))
				require.NoError(t, os.Chtimes(j.Stage, info.ModTime(), info.ModTime()))
				mutations++
			}
			return nil
		}}, nil
	}
	require.ErrorContains(t, primaryUndoVerify(context.Background(), f.fs, j.Stage, j.StageIdentity, j.SHA256), "after hashing")
	require.Equal(t, 1, mutations)
	require.FileExists(t, j.Source)
	require.NoFileExists(t, j.Destination)
	got, err := os.ReadFile(j.Stage)
	require.NoError(t, err)
	require.Equal(t, replacement, got)
}

func TestPrimaryUndoResumeGenerationRefusalPreservesJournal(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("copying")
	before := f.op.GeneratedFiles
	random := primaryUndoRandom
	t.Cleanup(func() { primaryUndoRandom = random })
	primaryUndoRandom = strings.NewReader("")
	require.Error(t, resumePrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
	f.reload()
	require.Equal(t, before, f.op.GeneratedFiles)
	require.FileExists(t, j.Source)
	require.FileExists(t, j.Stage)
}

func TestPrimaryUndoSupersededCleanupProofFailuresKeepClaims(t *testing.T) {
	for _, fault := range []string{"open", "close", "token", "terminal-open", "terminal-close", "destination-witness"} {
		t.Run(fault, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("copying")
			stop := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "primary_removed", after: true}
			require.Error(t, resumePrimaryUndo(context.Background(), f.fs, stop, f.op, j))
			require.True(t, stop.fired)
			j = f.journal()
			require.Len(t, j.SupersededStages, 1)
			stage := j.SupersededStages[0]
			open, publish, random := restoreOpenReplacementSource, primaryUndoPublish, primaryUndoRandom
			t.Cleanup(func() { restoreOpenReplacementSource = open; primaryUndoPublish = publish; primaryUndoRandom = random })
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				initial := path == stage.Path
				terminal := strings.HasPrefix(path, stage.Path+".undo-remove.")
				if (fault == "open" && initial) || (fault == "terminal-open" && terminal) {
					return nil, errPrimaryUndoFault
				}
				file, err := open(fs, path)
				if err != nil {
					return nil, err
				}
				if (fault == "close" && initial) || (fault == "terminal-close" && terminal) {
					return &primaryUndoFaultFile{File: file, fail: func(op string, _ int) error {
						if op == "close" {
							return errPrimaryUndoFault
						}
						return nil
					}}, nil
				}
				return file, nil
			}
			if fault == "token" {
				primaryUndoRandom = strings.NewReader("")
			}
			if fault == "destination-witness" {
				info, err := os.Stat(j.Destination)
				require.NoError(t, err)
				primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
					err := publish(fs, src, dst)
					if err == nil {
						time.Sleep(time.Millisecond)
						require.NoError(t, os.WriteFile(j.Destination, []byte("new media bytes!!!!!!!!"), info.Mode().Perm()))
						require.NoError(t, os.Chtimes(j.Destination, info.ModTime(), info.ModTime()))
					}
					return err
				}
			}
			require.Error(t, cleanupSupersededPrimaryUndo(context.Background(), f.fs, f.repo, f.op, j))
			saved := f.journal()
			require.Len(t, saved.SupersededStages, 1)
			require.Equal(t, "primary_removed", saved.State)
			require.True(t, models.PrimaryUndoNeedsRetention(*f.op))
			if strings.HasPrefix(fault, "terminal-") || fault == "destination-witness" {
				require.FileExists(t, saved.SupersededStages[0].Cleanup)
			} else {
				require.FileExists(t, stage.Path)
			}
			expected := "synthetic primary video"
			if fault == "destination-witness" {
				expected = "new media bytes!!!!!!!!"
			}
			got, err := os.ReadFile(j.Destination)
			require.NoError(t, err)
			require.Equal(t, expected, string(got))
		})
	}
}

func TestPrimaryUndoWeakMutationWitnessRefused(t *testing.T) {
	file, err := afero.NewMemMapFs().Create("synthetic-weak-witness")
	require.NoError(t, err)
	defer file.Close()
	_, err = primaryUndoObserveChange(file)
	require.ErrorContains(t, err, "native mutation witness")
}

type primaryUndoCrossDeviceFaultFS struct {
	afero.Fs
	renamed bool
}

func (fs *primaryUndoCrossDeviceFaultFS) Rename(source, dest string) error {
	fs.renamed = true
	return &os.LinkError{Op: "rename", Old: source, New: dest, Err: syscall.EXDEV}
}
func TestPrimaryUndoReverterAdmissionFailurePreservesPrimary(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	fs := &primaryUndoCrossDeviceFaultFS{Fs: f.fs}
	fault := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "planned"}
	cleaned := false
	result, err := revertPrimaryFileFS(context.Background(), fs, fault, f.op, func(string, string) { cleaned = true })
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.True(t, fs.renamed)
	require.True(t, fault.fired)
	require.False(t, cleaned)
	require.FileExists(t, f.op.NewPath)
	require.NoFileExists(t, f.op.OriginalPath)
	require.Nil(t, f.journal())
}
