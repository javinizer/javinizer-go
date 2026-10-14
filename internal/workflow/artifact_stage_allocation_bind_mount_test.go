package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagingPlacementFS struct {
	afero.Fs
	tempErrors          map[string]error
	mountRoot           string
	crossDeviceAttempts int
}

func (f *stagingPlacementFS) Mkdir(path string, mode os.FileMode) error {
	if strings.HasPrefix(filepath.Base(path), artifactStageDirPrefix) {
		if err := f.tempErrors[filepath.Dir(path)]; err != nil {
			return err
		}
	}
	return f.Fs.Mkdir(path, mode)
}

func (f *stagingPlacementFS) Rename(source, target string) error {
	inside := func(path string) bool {
		rel, err := filepath.Rel(f.mountRoot, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if f.mountRoot != "" && strings.Contains(source, artifactStageDirPrefix) && !inside(source) && inside(target) {
		f.crossDeviceAttempts++
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: syscall.EXDEV}
	}
	return f.Fs.Rename(source, target)
}

func (f *stagingPlacementFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if fs, ok := f.Fs.(afero.Lstater); ok {
		return fs.LstatIfPossible(path)
	}
	info, err := f.Fs.Stat(path)
	return info, false, err
}

type stagingPlacementPlanner struct {
	*organizer.Organizer
	plan  *organizer.OrganizePlan
	err   error
	calls int
}

func (p *stagingPlacementPlanner) PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	p.calls++
	return p.plan, p.err
}

func TestArtifactStagePlannedFallbackBoundaries(t *testing.T) {
	unexpected := errors.New("staging IO failure")
	planFailure := errors.New("planning failed")
	childFailure := errors.New("child allocation failed")
	for _, tc := range []struct {
		name        string
		initial     error
		planErr     error
		childErr    error
		path        string
		noPlanner   bool
		nilPlan     bool
		makeFile    bool
		wantErr     error
		wantSuccess bool
	}{
		{name: "permission", initial: os.ErrPermission, path: "child", wantSuccess: true},
		{name: "read-only mount", initial: syscall.EROFS, path: "child", wantSuccess: true},
		{name: "unrelated failure", initial: unexpected, path: "child", wantErr: unexpected},
		{name: "missing planner", initial: os.ErrPermission, noPlanner: true, wantErr: os.ErrPermission},
		{name: "planning failure", initial: os.ErrPermission, planErr: planFailure, wantErr: planFailure},
		{name: "nil plan", initial: os.ErrPermission, nilPlan: true, wantErr: os.ErrPermission},
		{name: "empty target", initial: os.ErrPermission, wantErr: os.ErrPermission},
		{name: "same root", initial: os.ErrPermission, path: ".", wantErr: os.ErrPermission},
		{name: "outside root", initial: os.ErrPermission, path: "../outside", wantErr: os.ErrPermission},
		{name: "missing target", initial: os.ErrPermission, path: "missing", wantErr: os.ErrNotExist},
		{name: "file target", initial: os.ErrPermission, path: "child", makeFile: true, wantErr: os.ErrPermission},
		{name: "child allocation failure", initial: os.ErrPermission, path: "child", childErr: childFailure, wantErr: childFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			library := filepath.Join(string(filepath.Separator), "library")
			child := filepath.Join(library, "child")
			require.NoError(t, base.MkdirAll(library, 0o755))
			if tc.makeFile {
				require.NoError(t, afero.WriteFile(base, child, []byte("foreign"), 0o644))
			} else {
				require.NoError(t, base.Mkdir(child, 0o755))
			}
			fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{library: tc.initial, child: tc.childErr}}
			plan := &organizer.OrganizePlan{}
			if tc.path != "" {
				plan.TargetDir = filepath.Join(library, tc.path)
			}
			if tc.nilPlan {
				plan = nil
			}
			planner := &stagingPlacementPlanner{plan: plan, err: tc.planErr}
			orch := &applyOrchImpl{fs: fs}
			if !tc.noPlanner {
				orch.organizer = planner
			}
			root, err := orch.allocateArtifactStageForApply(context.Background(), library, ApplyCmd{OperationMode: operationmode.OperationModeOrganize})
			if tc.wantSuccess {
				require.NoError(t, err)
				require.Equal(t, child, filepath.Dir(root))
			} else {
				require.ErrorIs(t, err, tc.wantErr)
				require.ErrorIs(t, err, tc.initial, "original allocation error must remain visible")
				require.Empty(t, root)
			}
			if tc.noPlanner || tc.initial == unexpected {
				require.Zero(t, planner.calls)
			} else {
				require.Equal(t, 1, planner.calls)
			}
		})
	}
}

// Distinct bind mount points can report identical device IDs but reject rename
// across the boundary. Model that boundary explicitly on native same-device
// directories without requiring mount privileges in CI.
func TestArtifactStageReadonlyBindMountDestination(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, rollback := range []bool{false, true} {
			name := "copy"
			if move {
				name = "move"
			}
			if rollback {
				name += "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				base := afero.NewOsFs()
				root := t.TempDir()
				library := filepath.Join(root, "library")
				movieDir := filepath.Join(library, "movie")
				require.NoError(t, base.MkdirAll(movieDir, 0o755))
				parentInfo, err := base.Stat(root)
				require.NoError(t, err)
				rootInfo, err := base.Stat(library)
				require.NoError(t, err)
				parentDevice, _, parentKnown := fsutil.BoundObjectIdentity(base, root, parentInfo)
				rootDevice, _, rootKnown := fsutil.BoundObjectIdentity(base, library, rootInfo)
				require.True(t, parentKnown && rootKnown)
				require.Equal(t, parentDevice, rootDevice, "the regression relies on equal device IDs")

				fs := &stagingPlacementFS{Fs: base, mountRoot: library, tempErrors: map[string]error{library: os.ErrPermission}}
				source := filepath.Join(root, "ABC-123.mkv")
				require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
				movie := models.Movie{ID: "ABC-123", ContentID: "readonly-bind-mount", RenderGeneration: 1}
				orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
				orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
				fenceErr := errors.New("publication rejected")
				fence := postPublishFence{movie: &movie}
				if rollback {
					fence.err = fenceErr
				}
				cmd := ApplyCmd{Movie: &movie, PublicationFence: fence, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv"}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
				result, err := orch.Execute(context.Background(), cmd)
				if rollback {
					require.ErrorIs(t, err, fenceErr)
					require.FileExists(t, source)
					entries, readErr := afero.ReadDir(base, movieDir)
					require.NoError(t, readErr)
					require.Empty(t, entries)
				} else {
					require.NoError(t, err)
					require.FileExists(t, result.OrganizeResult.NewPath)
					require.FileExists(t, result.NFOPath)
					if move {
						require.NoFileExists(t, source)
					} else {
						require.FileExists(t, source)
					}
				}
				require.Zero(t, fs.crossDeviceAttempts, "publication must never cross the simulated mount boundary")
				pr260AssertStageGone(t, fs, root)
				pr260AssertStageGone(t, fs, library)
				pr260AssertStageGone(t, fs, movieDir)
			})
		}
	}
}

func TestArtifactStageReadonlyCrossDeviceDestination(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	destination := nativeCrossDeviceDestination(t, root)
	library := filepath.Join(root, "library")
	if err := os.Symlink(destination, library); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	movieDir := filepath.Join(library, "movie")
	require.NoError(t, fs.Mkdir(movieDir, 0o755))
	wrapped := &stagingPlacementFS{Fs: fs, tempErrors: map[string]error{library: syscall.EROFS}}
	source := filepath.Join(root, "ABC-123.mkv")
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	movie := models.Movie{ID: "ABC-123", ContentID: "readonly-cross-device", RenderGeneration: 1}
	orch := pr260RealApply(wrapped, &movie, organizer.MediaFormatConfig{}, nil, true)
	orch.organizer = organizer.NewOrganizer(wrapped, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv"}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
	result, err := orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	require.FileExists(t, result.OrganizeResult.NewPath)
	require.FileExists(t, result.NFOPath)
	require.NoFileExists(t, source)
	pr260AssertStageGone(t, wrapped, root)
	pr260AssertStageGone(t, wrapped, library)
	pr260AssertStageGone(t, wrapped, movieDir)
}

func TestArtifactStageReadonlyLongFolder(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	library := filepath.Join(root, "library")
	require.NoError(t, base.Mkdir(library, 0o755))
	fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{library: os.ErrPermission}}
	source := filepath.Join(root, "ABC-123.mkv")
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	movie := models.Movie{ID: "ABC-123", ContentID: "long-folder", RenderGeneration: 1}
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: strings.Repeat("m", 140), FileFormat: "movie", RenameFile: true, MaxPathLength: 240, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}
	plan, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: library, MoveFiles: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	require.LessOrEqual(t, len(plan.TargetPath), 240)
	require.NoError(t, fs.MkdirAll(plan.TargetDir, 0o755))
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	orch.organizer = org
	cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: match, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
	result, err := orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, plan.TargetPath, result.OrganizeResult.NewPath)
	require.FileExists(t, result.OrganizeResult.NewPath)
	require.FileExists(t, result.NFOPath)
	require.Equal(t, plan.TargetDir, filepath.Dir(result.NFOPath))
	pr260AssertStageGone(t, fs, plan.TargetDir)
}

func TestArtifactStagePlansFinalRelativeLayout(t *testing.T) {
	for _, target := range []string{"/library", "/library/movie/nested", "/outside", "relative"} {
		t.Run(target, func(t *testing.T) {
			plan := &organizer.OrganizePlan{TargetDir: target, TargetPath: filepath.Join(target, "movie.mkv")}
			planner := &stagingPlacementPlanner{plan: plan}
			orch := &applyOrchImpl{organizer: planner}
			stage := &artifactStage{root: "/stage", finalRoot: "/library", sourcePath: "/incoming/video.mkv", videoDeferred: true}
			state := &applyPipelineState{artifact: stage, movie: &models.Movie{ID: "ABC-123"}}
			steps := &stepCompletion{}
			err := orch.stepOrganize(context.Background(), ApplyCmd{OperationMode: operationmode.OperationModeOrganize}, state, steps)
			if target == "/outside" || target == "relative" {
				require.ErrorContains(t, err, "planned artifact path escapes destination")
				require.False(t, steps.Organized)
				return
			}
			require.NoError(t, err)
			require.True(t, steps.Organized)
			merged, err := stage.mergeMatch(state, models.FileMatchInfo{})
			require.NoError(t, err)
			require.Equal(t, plan.TargetPath, merged.Path, "NFO merge must use the original final relative layout")
		})
	}
}
