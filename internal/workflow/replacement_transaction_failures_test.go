package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type publicationBranchFS struct {
	afero.Fs
	statPath, mkdirPath, removePath, lstatPath, enableLstatAfterRename, plantOnMissingPath string
	activateStatFaultPath, activateStatFaultTarget                                         string
}

func (f *publicationBranchFS) Stat(name string) (os.FileInfo, error) {
	if f.activateStatFaultPath != "" && filepath.Clean(name) == filepath.Clean(f.activateStatFaultPath) {
		f.activateStatFaultPath = ""
		_ = f.Fs.Remove(name)
		f.statPath = f.activateStatFaultTarget
		return nil, os.ErrNotExist
	}
	if f.plantOnMissingPath != "" && filepath.Clean(name) == filepath.Clean(f.plantOnMissingPath) {
		f.plantOnMissingPath = ""
		if err := afero.WriteFile(f.Fs, name, []byte("late foreign"), 0o644); err != nil {
			return nil, err
		}
		return nil, os.ErrNotExist
	}
	if filepath.Clean(name) == filepath.Clean(f.statPath) {
		return nil, errors.New("publication stat fault")
	}
	return f.Fs.Stat(name)
}
func (f *publicationBranchFS) MkdirAll(name string, perm os.FileMode) error {
	if filepath.Clean(name) == filepath.Clean(f.mkdirPath) {
		return errors.New("publication mkdir fault")
	}
	return f.Fs.MkdirAll(name, perm)
}
func (f *publicationBranchFS) Remove(name string) error {
	if filepath.Clean(name) == filepath.Clean(f.removePath) {
		return errors.New("publication remove fault")
	}
	return f.Fs.Remove(name)
}
func (f *publicationBranchFS) Rename(oldname, newname string) error {
	if err := f.Fs.Rename(oldname, newname); err != nil {
		return err
	}
	if filepath.Clean(newname) == filepath.Clean(f.enableLstatAfterRename) {
		f.lstatPath = newname
	}
	return nil
}
func (f *publicationBranchFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.lstatPath) {
		if _, err := f.Fs.Stat(name); err == nil {
			return nil, false, errors.New("publication lstat fault")
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

type postPublishFence struct {
	movie *models.Movie
	after func()
	err   error
}

func (f postPublishFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f postPublishFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	if err := fn(f.movie); err != nil {
		return err
	}
	if f.after != nil {
		f.after()
	}
	return f.err
}

type completionFaultLog struct {
	RevertLog
	complete func() error
}

func (l *completionFaultLog) Complete(context.Context, OperationID, *ApplyResult) error {
	if l.complete != nil {
		return l.complete()
	}
	return nil
}

func stagedPublicationTarget(t *testing.T, real *organizer.Organizer, stage *artifactStage, cmd ApplyCmd) *organizer.OrganizePlan {
	t.Helper()
	match := cmd.Match
	match.Path = stage.stagedSource
	match.Name = filepath.Base(match.Path)
	plan, err := real.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: cmd.Movie, DestDir: stage.finalRoot, ForceUpdate: cmd.Organize.ForceUpdate, MoveFiles: cmd.Organize.MoveFiles, LinkMode: cmd.Organize.LinkMode, OperationMode: cmd.OperationMode})
	require.NoError(t, err)
	return plan
}

func TestFenceFailureRollbackPreservesForeignFinalSubstitution(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "fence-rollback")
	dest := filepath.Join(root, "library")
	movie := models.Movie{ContentID: "fence-rollback", RenderGeneration: 3}
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Download = false
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.PublicationFence = postPublishFence{movie: &movie}
	stage, _, err := orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	plan := stagedPublicationTarget(t, real, stage, cmd)
	cmd.PublicationFence = postPublishFence{movie: &movie, err: database.ErrApplyPublicationStale, after: func() {
		require.NoError(t, base.Remove(plan.TargetPath))
		require.NoError(t, afero.WriteFile(base, plan.TargetPath, []byte("foreign"), 0o644))
	}}
	stage.fencer = artifactFencer(cmd)
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
	err = stage.publish(t.Context(), orch, state, nil)
	require.ErrorContains(t, err, "rollback staged publication after fence failure")
	got, readErr := afero.ReadFile(base, plan.TargetPath)
	require.NoError(t, readErr)
	require.Equal(t, "foreign", string(got))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

func TestPublishUnderFenceRejectsMissingFilesystem(t *testing.T) {
	stage := &artifactStage{original: ApplyCmd{Organize: OrganizeOptions{Skip: true}}}
	err := stage.publishUnderFence(t.Context(), &applyOrchImpl{}, "op", &applyPipelineState{}, nil)
	require.ErrorContains(t, err, "requires a filesystem")
}

func TestVideoPublicationFailsClosedAtEachGuard(t *testing.T) {
	for _, mode := range []string{"nonregular target", "parent create", "occupied target", "guard plan", "guard path", "confirm inspect"} {
		t.Run(mode, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "video-"+mode)
			fs := &publicationBranchFS{Fs: base}
			dest := filepath.Join(root, "library")
			movie := models.Movie{ContentID: "video-" + mode, RenderGeneration: 1}
			real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			fault := &pr260PublicationFaultOrganizer{Organizer: real}
			orch := &applyOrchImpl{fs: fs, organizer: fault}
			cmd := pr260ArtifactFailureCommand(&movie, match, dest)
			cmd.Download = false
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = false
			cmd.PublicationFence = postPublishFence{movie: &movie}
			stage, _, err := orch.prepareArtifact(t.Context(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			plan := stagedPublicationTarget(t, real, stage, cmd)
			switch mode {
			case "nonregular target":
				require.NoError(t, base.MkdirAll(plan.TargetPath, 0o755))
				cmd.Organize.ForceUpdate = true
				stage.original.Organize.ForceUpdate = true
			case "parent create":
				fs.mkdirPath = filepath.Dir(plan.TargetPath)
			case "occupied target":
				require.NoError(t, base.MkdirAll(filepath.Dir(plan.TargetPath), 0o755))
				require.NoError(t, afero.WriteFile(base, plan.TargetPath, []byte("occupied"), 0o644))
			case "guard plan":
				fault.failPlanAt = 2
			case "guard path":
				fault.changeGuardPath = true
			case "confirm inspect":
				fault.afterExecute = func(plan *organizer.OrganizePlan, _ *organizer.OrganizeResult) { fs.lstatPath = plan.TargetPath }
			}
			state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			err = stage.publish(t.Context(), orch, state, nil)
			require.Error(t, err)
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
		})
	}
}

func TestSidecarPublicationFaultsRestorePublishedVideo(t *testing.T) {
	for _, mode := range []string{"staged inspect", "occupied destination", "confirm inspect", "inverse persistence"} {
		t.Run(mode, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "sidecar-"+mode)
			fs := &publicationBranchFS{Fs: base}
			dest := filepath.Join(root, "library")
			movie := models.Movie{ContentID: "sidecar-" + mode, RenderGeneration: 1}
			real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := &applyOrchImpl{fs: fs, organizer: real}
			cmd := pr260ArtifactFailureCommand(&movie, match, dest)
			cmd.Download = false
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.PublicationFence = postPublishFence{movie: &movie}
			stage, _, err := orch.prepareArtifact(t.Context(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			plan := stagedPublicationTarget(t, real, stage, cmd)
			target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(plan.TargetPath), filepath.Base(stage.siblings[0].sourcePath)))
			switch mode {
			case "staged inspect":
				fs.activateStatFaultPath = target
				fs.activateStatFaultTarget = stage.siblings[0].stagedPath
			case "occupied destination":
				require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o755))
				fs.plantOnMissingPath = target
			case "confirm inspect":
				fs.lstatPath = target
			case "inverse persistence":
				orch.revertLog = &completionFaultLog{complete: func() error { return errors.New("inverse unavailable") }}
			}
			state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			err = stage.publish(t.Context(), orch, state, nil)
			require.Error(t, err)
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
		})
	}
}

func TestArtifactTreeInspectionAndLegacyReplacementFailures(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "tree-faults")
	fs := &publicationBranchFS{Fs: base}
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(t.Context(), pr260ArtifactFailureCommand(&models.Movie{ContentID: "tree-faults"}, match, filepath.Join(root, "library")))
	require.NoError(t, err)
	defer stage.cleanup()
	fs.statPath = stage.root
	_, err = stage.treeDestinations("", "", "", "")
	require.ErrorContains(t, err, "inspect staged artifact root")
	fs.statPath = ""
	staged := filepath.Join(stage.root, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	target := filepath.Join(stage.finalRoot, "poster.jpg")
	require.NoError(t, base.MkdirAll(stage.finalRoot, 0o755))
	require.NoError(t, afero.WriteFile(base, target, []byte("old"), 0o644))
	stage.original.OverwriteExistingMedia = true
	fs.removePath = target
	_, err = stage.installTree("", "", nil, "", "")
	require.ErrorContains(t, err, "replace artifact destination")
}

func TestArtifactTreeConfirmFailureRetainsRecoverableStage(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "tree-confirm")
	fs := &publicationBranchFS{Fs: base}
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(t.Context(), pr260ArtifactFailureCommand(&models.Movie{ContentID: "tree-confirm"}, match, filepath.Join(root, "library")))
	require.NoError(t, err)
	defer stage.cleanup()
	staged := filepath.Join(stage.root, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	target := filepath.Join(stage.finalRoot, "poster.jpg")
	batch, err := downloader.NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	stage.publishBatch, stage.publishCtx = batch, t.Context()
	fs.enableLstatAfterRename = target
	_, err = stage.installTree("", "", nil, "", "")
	require.ErrorContains(t, err, "inspect staged publication result")
	require.NoError(t, batch.Rollback(t.Context()))
}

type escapingDirectoryFS struct {
	afero.Fs
	root string
}

type escapingDirectoryFile struct {
	afero.File
	sent    bool
	outside os.FileInfo
}

type escapingFileInfo struct{ os.FileInfo }

func (escapingFileInfo) Name() string { return "../outside.nfo" }

func (f *escapingDirectoryFile) Readdir(int) ([]os.FileInfo, error) {
	if f.sent {
		return nil, io.EOF
	}
	f.sent = true
	return []os.FileInfo{escapingFileInfo{FileInfo: f.outside}}, nil
}

func (f *escapingDirectoryFile) Readdirnames(int) ([]string, error) {
	if f.sent {
		return nil, io.EOF
	}
	f.sent = true
	return []string{"../outside.nfo"}, nil
}

func (f *escapingDirectoryFS) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(name) == filepath.Clean(f.root) {
		outside, statErr := f.Fs.Stat(filepath.Join(filepath.Dir(f.root), "outside.nfo"))
		if statErr != nil {
			_ = file.Close()
			return nil, statErr
		}
		return &escapingDirectoryFile{File: file, outside: outside}, nil
	}
	return file, nil
}

func TestInPlaceOrganizerResultCannotEscapeOwnedStage(t *testing.T) {
	for _, field := range []string{"new path", "folder path"} {
		t.Run(field, func(t *testing.T) {
			base, _, _, subtitle, multipart, _, match := pr260FencedFiles(t, "inplace-result-"+field)
			require.NoError(t, base.Remove(subtitle))
			require.NoError(t, base.Remove(multipart))
			movie := models.Movie{ContentID: "inplace-result-" + field, RenderGeneration: 1}
			real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			fault := &pr260PublicationFaultOrganizer{Organizer: real}
			orch := &applyOrchImpl{fs: base, organizer: fault}
			cmd := pr260ArtifactFailureCommand(&movie, match, filepath.Dir(match.Path))
			cmd.Download = false
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
			cmd.PublicationFence = postPublishFence{movie: &movie}
			stage, _, err := orch.prepareArtifact(t.Context(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			outside := filepath.Join(t.TempDir(), "outside")
			fault.afterExecute = func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
				if field == "new path" {
					result.NewPath = outside + ".mp4"
				} else {
					result.FolderPath = outside
				}
			}
			state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			err = stage.publish(t.Context(), orch, state, nil)
			require.ErrorContains(t, err, "escapes staging area")
		})
	}
}

func TestMoveCleanupRejectsUntrackedOrganizerResult(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "untracked-result")
	movie := models.Movie{ContentID: "untracked-result", RenderGeneration: 1}
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		result.NewPath = filepath.Join(root, "missing-parent", "untracked.mp4")
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&movie, match, filepath.Join(root, "library"))
	cmd.Download = false
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.PublicationFence = postPublishFence{movie: &movie}
	stage, _, err := orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
	err = stage.publish(t.Context(), orch, state, nil)
	require.ErrorContains(t, err, "no regular installed output")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

func TestSidecarCleanupRejectsNonregularFinalSubstitution(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "sidecar-origin")
	movie := models.Movie{ContentID: "sidecar-origin", RenderGeneration: 1}
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real}
	cmd := pr260ArtifactFailureCommand(&movie, match, filepath.Join(root, "library"))
	cmd.Download = false
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.PublicationFence = postPublishFence{movie: &movie}
	stage, _, err := orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	plan := stagedPublicationTarget(t, real, stage, cmd)
	target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(plan.TargetPath), filepath.Base(stage.siblings[0].sourcePath)))
	require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, afero.WriteFile(base, target, []byte("preexisting"), 0o644))
	orch.revertLog = &completionFaultLog{complete: func() error {
		require.NoError(t, base.Remove(target))
		return base.Mkdir(target, 0o755)
	}}
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
	err = stage.publish(t.Context(), orch, state, nil)
	require.ErrorContains(t, err, "no regular installed output")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

func TestTreeDestinationWalkRejectsEscapingEntry(t *testing.T) {
	base := afero.NewMemMapFs()
	root := "/stage/root"
	require.NoError(t, base.MkdirAll(root, 0o755))
	require.NoError(t, afero.WriteFile(base, "/stage/outside.nfo", []byte("outside"), 0o644))
	fs := &escapingDirectoryFS{Fs: base, root: root}
	stage := &artifactStage{fs: fs, root: root, finalRoot: "/other/base/final", inPlace: true}
	_, err := stage.treeDestinations("", "", "", "")
	require.ErrorContains(t, err, "escapes staging area")
}

var _ database.ApplyArtifactPublicationFencer = postPublishFence{}
