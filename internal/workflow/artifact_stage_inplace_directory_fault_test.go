package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

type directoryFaultFS struct {
	afero.Fs
	mkdirErr, openErr, statErr, closeErr error
	probe                                func(string) error
	beforeRename, afterRename            func(string, string) error
}

func (f *directoryFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.probe != nil {
		if err := f.probe(path); err != nil {
			return nil, true, err
		}
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func (f *directoryFaultFS) MkdirAll(path string, mode os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	return f.Fs.MkdirAll(path, mode)
}

func (f *directoryFaultFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	if strings.Contains(filepath.Base(path), ".javinizer-directory-") && !strings.Contains(path, ".vac.") {
		if f.openErr != nil {
			return nil, f.openErr
		}
		file, err := f.Fs.OpenFile(path, flags, mode)
		if err != nil {
			return nil, err
		}
		return &directoryFaultFile{File: file, statErr: f.statErr, closeErr: f.closeErr}, nil
	}
	return f.Fs.OpenFile(path, flags, mode)
}

func (f *directoryFaultFS) Rename(source, target string) error {
	if f.beforeRename != nil {
		if err := f.beforeRename(source, target); err != nil {
			return err
		}
	}
	if err := f.Fs.Rename(source, target); err != nil {
		return err
	}
	if f.afterRename != nil {
		return f.afterRename(source, target)
	}
	return nil
}

type directoryFaultFile struct {
	afero.File
	statErr, closeErr error
}

func (f *directoryFaultFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.File.Stat()
}
func (f *directoryFaultFile) Close() error { return errors.Join(f.File.Close(), f.closeErr) }

type directoryFaultLog struct {
	noOpRevertLog
	intentCalls, completeCalls, intentFailAt, completeFailAt int
	failure                                                  error
}

func (l *directoryFaultLog) RecordMoveIntent(context.Context, OperationID, string, string) error {
	l.intentCalls++
	if l.intentCalls == l.intentFailAt {
		return l.failure
	}
	return nil
}
func (l *directoryFaultLog) Complete(context.Context, OperationID, *ApplyResult) error {
	l.completeCalls++
	if l.completeCalls == l.completeFailAt {
		return l.failure
	}
	return nil
}

type directoryFaultPlanner struct {
	*organizer.Organizer
	err    error
	target string
}

func (p *directoryFaultPlanner) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	if p.err != nil {
		return nil, p.err
	}
	plan, err := p.Organizer.PlanOrganize(ctx, cmd)
	if err == nil && p.target != "" {
		plan.TargetDir, plan.TargetPath = p.target, filepath.Join(p.target, filepath.Base(plan.TargetPath))
		plan.RenameFolder = true
	}
	return plan, err
}

func directoryMoveFixture(t *testing.T) (*artifactStage, *applyOrchImpl, *applyPipelineState, string, string) {
	t.Helper()
	fs := afero.NewOsFs()
	sourceDir := filepath.Join(t.TempDir(), "incoming")
	require.NoError(t, fs.MkdirAll(sourceDir, 0o755))
	source := filepath.Join(sourceDir, "IPX-123-original.mkv")
	require.NoError(t, afero.WriteFile(fs, source, []byte("owned media"), 0o644))
	movie := models.Movie{ID: "IPX-123", ContentID: "directory-fault", RenderGeneration: 1}
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
	orch := &applyOrchImpl{fs: fs, organizer: org}
	cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}, DestPath: sourceDir, OperationMode: operationmode.OperationModeInPlace, Organize: OrganizeOptions{MoveFiles: true}, PublicationFence: postPublishFence{movie: &movie}}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	t.Cleanup(stage.cleanup)
	target := filepath.Join(filepath.Dir(sourceDir), "renamed")
	stage.directoryMove = &artifactInPlaceDirectoryMove{oldDir: sourceDir, newDir: target, currentDir: sourceDir, identity: stage.sourceDirIdentity}
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}, operationID: "op"}
	return stage, orch, state, sourceDir, target
}

func TestInPlaceDirectoryMoveFaults(t *testing.T) {
	for _, fault := range []string{"cancel", "identity", "occupied", "mkdir", "open", "stat", "close", "intent-1", "complete-1", "take", "intent-2", "complete-2", "checkpoint-restore", "publish", "publish-restore", "post-publish", "take-restore"} {
		t.Run(fault, func(t *testing.T) {
			stage, orch, _, source, target := directoryMoveFixture(t)
			failure := errors.New("directory fault")
			fs := &directoryFaultFS{Fs: stage.fs}
			stage.fs = fs
			log := &directoryFaultLog{failure: failure}
			orch.revertLog = log
			ctx := context.Background()
			id := stage.sourceDirIdentity
			retained := false
			foreign := false
			switch fault {
			case "cancel":
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			case "identity":
				id.known = false
			case "occupied":
				require.NoError(t, fs.Fs.MkdirAll(target, 0o755))
			case "mkdir":
				fs.mkdirErr = failure
			case "open":
				fs.openErr = failure
			case "stat":
				fs.statErr = failure
			case "close":
				fs.closeErr = failure
			case "intent-1":
				log.intentFailAt = 1
			case "complete-1":
				log.completeFailAt = 1
			case "take":
				fs.beforeRename = func(from, to string) error {
					if from == source {
						return failure
					}
					return nil
				}
			case "intent-2":
				log.intentFailAt = 2
			case "complete-2":
				log.completeFailAt = 2
			case "checkpoint-restore":
				log.intentFailAt, retained = 2, true
				fs.beforeRename = func(from, to string) error {
					if to == source {
						return failure
					}
					return nil
				}
			case "publish":
				fs.beforeRename = func(from, to string) error {
					if to == target {
						return failure
					}
					return nil
				}
			case "publish-restore":
				retained = true
				fs.beforeRename = func(from, to string) error {
					if to == target || to == source {
						return failure
					}
					return nil
				}
			case "post-publish":
				foreign = true
				fs.afterRename = func(from, to string) error {
					if to != target {
						return nil
					}
					require.NoError(t, fs.Fs.Rename(target, target+".owned"))
					require.NoError(t, fs.Fs.MkdirAll(target, 0o755))
					return afero.WriteFile(fs.Fs, filepath.Join(target, "foreign.txt"), []byte("foreign"), 0o644)
				}
			case "take-restore":
				retained = true
				failProbe := ""
				fs.afterRename = func(from, to string) error {
					if from == source {
						failProbe = to
					}
					return nil
				}
				fs.probe = func(path string) error {
					if path == failProbe && failProbe != "" {
						failProbe = ""
						return failure
					}
					return nil
				}
				fs.beforeRename = func(from, to string) error {
					if to == source {
						return failure
					}
					return nil
				}
			}
			location, err := stage.moveInPlaceDirectory(ctx, orch, "op", source, target, id)
			require.Error(t, err)
			if retained {
				require.ErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed)
			}
			if fault == "complete-2" {
				require.Equal(t, target, location)
				require.FileExists(t, filepath.Join(target, "IPX-123-original.mkv"))
			} else if foreign {
				require.Equal(t, target, location)
				require.FileExists(t, filepath.Join(target, "foreign.txt"))
				require.FileExists(t, filepath.Join(target+".owned", "IPX-123-original.mkv"))
			} else if retained {
				require.NotEqual(t, source, location)
				require.NoDirExists(t, source)
				require.FileExists(t, filepath.Join(location, "IPX-123-original.mkv"))
			} else {
				require.Equal(t, source, location)
				body, readErr := afero.ReadFile(fs.Fs, filepath.Join(source, "IPX-123-original.mkv"))
				require.NoError(t, readErr)
				require.Equal(t, "owned media", string(body))
			}
		})
	}
}

func TestInPlaceDirectoryRollbackPreservesReappearedOriginal(t *testing.T) {
	for _, phase := range []string{"during-publish", "after-fence"} {
		t.Run(phase, func(t *testing.T) {
			stage, orch, state, source, target := directoryMoveFixture(t)
			state.operationID = ""
			commitErr := errors.New("commit rejected")
			if phase == "after-fence" {
				stage.fencer = postPublishFence{movie: stage.original.Movie, err: commitErr}
			}
			fs := &directoryFaultFS{Fs: stage.fs}
			stage.fs = fs
			require.NoError(t, afero.WriteFile(fs.Fs, filepath.Join(stage.root, "IPX-123.nfo"), []byte("generated metadata"), 0o644))
			fs.afterRename = func(from, to string) error {
				if to != target {
					return nil
				}
				require.NoError(t, fs.Fs.MkdirAll(source, 0o755))
				require.NoError(t, afero.WriteFile(fs.Fs, filepath.Join(source, "foreign.txt"), []byte("foreign original"), 0o644))
				if phase == "during-publish" {
					return afero.WriteFile(fs.Fs, filepath.Join(target, "IPX-123.nfo"), []byte("foreign metadata"), 0o644)
				}
				return nil
			}
			err := stage.publish(context.Background(), orch, state, nil)
			require.ErrorContains(t, err, "rollback in-place directory relocation")
			if phase == "after-fence" {
				require.ErrorIs(t, err, commitErr)
			}
			require.True(t, stage.sharedPoisoned)
			body, readErr := afero.ReadFile(fs.Fs, filepath.Join(source, "foreign.txt"))
			require.NoError(t, readErr)
			require.Equal(t, "foreign original", string(body))
			body, readErr = afero.ReadFile(fs.Fs, filepath.Join(target, "IPX-123-original.mkv"))
			require.NoError(t, readErr)
			require.Equal(t, "owned media", string(body))
			require.True(t, artifactStageDirMatches(fs, target, stage.sourceDirIdentity))
			if phase == "during-publish" {
				body, readErr = afero.ReadFile(fs.Fs, filepath.Join(target, "IPX-123.nfo"))
				require.NoError(t, readErr)
				require.Equal(t, "foreign metadata", string(body))
			}
		})
	}
}

func TestInPlaceDirectoryPlanningRefusals(t *testing.T) {
	for _, fault := range []string{"missing-executor", "plan-error", "child-target", "move-error"} {
		t.Run(fault, func(t *testing.T) {
			stage, orch, state, source, _ := directoryMoveFixture(t)
			failure := errors.New("planning fault")
			p := &directoryFaultPlanner{Organizer: orch.organizer.(*organizer.Organizer)}
			switch fault {
			case "missing-executor":
				orch.organizer = nil
			case "plan-error":
				p.err = failure
				orch.organizer = p
			case "child-target":
				p.target = filepath.Join(source, "child")
				orch.organizer = p
			case "move-error":
				stage.fs = &directoryFaultFS{Fs: stage.fs, openErr: failure}
			}
			require.Error(t, stage.relocateInPlaceDirectory(context.Background(), orch, "", state))
			require.FileExists(t, filepath.Join(source, "IPX-123-original.mkv"))
		})
	}
}

func TestInPlaceDirectoryRejectsSourceSwap(t *testing.T) {
	stage, orch, _, source, target := directoryMoveFixture(t)
	fs := &directoryFaultFS{Fs: stage.fs}
	stage.fs = fs
	fs.beforeRename = func(from, to string) error {
		if from != source {
			return nil
		}
		require.NoError(t, fs.Fs.Rename(source, source+".owned"))
		require.NoError(t, fs.Fs.MkdirAll(source, 0o755))
		return afero.WriteFile(fs.Fs, filepath.Join(source, "foreign.txt"), []byte("foreign"), 0o644)
	}
	_, err := stage.moveInPlaceDirectory(context.Background(), orch, "", source, target, stage.sourceDirIdentity)
	require.ErrorIs(t, err, errArtifactSourceChanged)
	require.FileExists(t, filepath.Join(source, "foreign.txt"))
	require.FileExists(t, filepath.Join(source+".owned", "IPX-123-original.mkv"))
	require.NoDirExists(t, target)
}

func TestInPlaceDirectoryPrepareDirectoryProbeFailure(t *testing.T) {
	stage, orch, _, source, _ := directoryMoveFixture(t)
	failure := errors.New("directory probe denied")
	orch.fs = &directoryFaultFS{Fs: stage.fs, probe: func(path string) error {
		if path == source {
			return failure
		}
		return nil
	}}
	_, _, err := orch.prepareArtifact(context.Background(), stage.original)
	require.ErrorIs(t, err, failure)
	require.FileExists(t, stage.sourcePath)
}
