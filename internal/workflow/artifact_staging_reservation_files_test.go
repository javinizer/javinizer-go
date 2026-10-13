package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

type stagedReservationReleaseFaultFS struct {
	afero.Fs
	failReads map[string]int
}

func (f *stagedReservationReleaseFaultFS) Open(path string) (afero.File, error) {
	path = filepath.Clean(path)
	if f.failReads[path] > 0 {
		f.failReads[path]--
		return nil, os.ErrPermission
	}
	return f.Fs.Open(path)
}
func (f *stagedReservationReleaseFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func TestFencedPublicationWithBusyMarkerReleaseFailure(t *testing.T) {
	for _, lateFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "publish", true: "compensate"}[lateFailure], func(t *testing.T) {
			ctx := context.Background()
			base, _, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "busy-reservations")
			root := filepath.Dir(filepath.Dir(source))
			dest := filepath.Join(root, "library")
			fs := &stagedReservationReleaseFaultFS{Fs: base, failReads: map[string]int{}}
			real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := &applyOrchImpl{fs: fs, organizer: real}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "busy-reservations"}, match, dest)
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.Download = false
			if lateFailure {
				cmd.PublicationFence = staleFinalizeFencer{}
			}
			stage, _, err := orch.prepareArtifact(ctx, cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			plan, err := real.PlanOrganize(ctx, organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
			require.NoError(t, err)
			require.NoError(t, base.MkdirAll(plan.TargetDir, 0o700))
			fanart, nfo := filepath.Join(plan.TargetDir, "fanart.jpg"), filepath.Join(plan.TargetDir, "movie.nfo")
			require.NoError(t, afero.WriteFile(base, fanart, []byte("artwork"), 0o600))
			require.NoError(t, afero.WriteFile(base, nfo, []byte("metadata"), 0o600))
			fs.failReads[fsutil.ReplacementBusyPath(fanart)] = 1
			release, err := fsutil.AcquireReplacementBusy(fs, fanart)
			require.NoError(t, err)
			release()
			finalDir := filepath.Join(dest, "movie")
			if lateFailure {
				for _, name := range []string{"movie.mp4", "movie.nfo", "fanart.jpg"} {
					fs.failReads[fsutil.ReplacementBusyPath(filepath.Join(finalDir, name))] = 1
				}
			}
			state := &applyPipelineState{operationID: "busy-operation", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}, downloadPaths: []string{fanart}, nfoPath: nfo}
			publishErr := stage.publish(ctx, orch, state, nil)
			if lateFailure {
				require.ErrorIs(t, publishErr, database.ErrApplyPublicationStale)
				require.NotErrorIs(t, publishErr, fsutil.ErrReplacementBusy)
				pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
				files, err := afero.ReadDir(base, finalDir)
				require.NoError(t, err)
				require.Empty(t, files)
			} else {
				require.NoError(t, publishErr)
				for name, payload := range map[string]string{"movie.mp4": "video", "movie.nfo": "metadata", "fanart.jpg": "artwork"} {
					actual, err := afero.ReadFile(base, filepath.Join(finalDir, name))
					require.NoError(t, err)
					require.Equal(t, payload, string(actual))
					_, err = base.Stat(fsutil.ReplacementBusyPath(filepath.Join(finalDir, name)))
					require.True(t, os.IsNotExist(err))
				}
			}
		})
	}
}

func TestStagedArtifactWalksExcludeBusyReservations(t *testing.T) {
	fs := afero.NewMemMapFs()
	root, final := filepath.FromSlash("/stage"), filepath.FromSlash("/library")
	require.NoError(t, fs.MkdirAll(root, 0o700))
	names := []string{"fanart.jpg.dlbusy", "fanart.jpg.dlbusy.takeover-123-abcd", "fanart.jpg.dlbusy.quarantine-123-abcd", "fanart.jpg.dlbusy.takeover-123-abcd.vac.001"}
	for _, name := range names {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, name), []byte("reservation"), 0o600))
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(root, "nested.dlbusy"), 0o700))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "nested.dlbusy", "private"), []byte("control"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "fanart.jpg"), []byte("artwork"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "notes.dlbusy-reference.txt"), []byte("ordinary artifact"), 0o600))
	stage := &artifactStage{fs: fs, root: root, finalRoot: final}
	destinations, err := stage.treeDestinations("", "", "", "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{filepath.Join(final, "fanart.jpg"), filepath.Join(final, "notes.dlbusy-reference.txt")}, destinations)
	_, err = stage.installTree("", "", nil, "", "", nil)
	require.NoError(t, err)
	artifacts, err := afero.ReadDir(fs, final)
	require.NoError(t, err)
	require.Len(t, artifacts, 2)
	for _, name := range names {
		_, err := fs.Stat(filepath.Join(root, name))
		require.NoError(t, err)
	}
	_, err = fs.Stat(filepath.Join(root, "nested.dlbusy", "private"))
	require.NoError(t, err)
}
