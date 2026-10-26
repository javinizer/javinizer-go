package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// These wrappers observe real planning and filesystem inspection. Cancellation
// happens synchronously at a boundary, without sleeps or replacing the planner.
type previewCancellationFS struct {
	afero.Fs
	ctx             context.Context
	postCancelCalls []string
	beforeLstat     func(string) error
	beforeOpen      func(string) error
	afterReadDir    func()
}

func (f *previewCancellationFS) observe(op, path string) {
	if f.ctx.Err() != nil {
		f.postCancelCalls = append(f.postCancelCalls, op+":"+path)
	}
}

func (f *previewCancellationFS) Stat(path string) (os.FileInfo, error) {
	f.observe("stat", path)
	return f.Fs.Stat(path)
}

func (f *previewCancellationFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	f.observe("lstat", path)
	if f.beforeLstat != nil {
		if err := f.beforeLstat(path); err != nil {
			return nil, true, err
		}
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func (f *previewCancellationFS) Open(path string) (afero.File, error) {
	f.observe("open", path)
	if f.beforeOpen != nil {
		if err := f.beforeOpen(path); err != nil {
			return nil, err
		}
	}
	file, err := f.Fs.Open(path)
	if err != nil {
		return nil, err
	}
	return &previewCancellationFile{File: file, afterReadDir: f.afterReadDir}, nil
}

type previewCancellationFile struct {
	afero.File
	afterReadDir func()
}

func (f *previewCancellationFile) Readdir(count int) ([]os.FileInfo, error) {
	entries, err := f.File.Readdir(count)
	if f.afterReadDir != nil {
		f.afterReadDir()
	}
	return entries, err
}

type previewCancellationStrategy struct {
	organizer.OperationStrategy
	ctx             context.Context
	cancel          context.CancelFunc
	cancelAt        int
	calls           int
	postCancelCalls int
	executeCalls    int
	inPlan          bool
}

func (s *previewCancellationStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, dest string, force bool) (*organizer.OrganizePlan, error) {
	s.calls++
	if s.ctx.Err() != nil {
		s.postCancelCalls++
	}
	s.inPlan = true
	plan, err := s.OperationStrategy.Plan(match, movie, dest, force)
	s.inPlan = false
	if s.calls == s.cancelAt {
		s.cancel()
	}
	return plan, err
}

func (s *previewCancellationStrategy) Execute(*organizer.OrganizePlan) (*organizer.OrganizeResult, error) {
	s.executeCalls++
	return nil, fmt.Errorf("preview must never execute a plan")
}

// Cancel between the completed planning-phase guard and the collision-loop
// guard. A context wrapper makes this otherwise timing-dependent boundary
// deterministic; Err and Done still come from a real cancelable context.
type previewCollisionLoopCancellation struct {
	context.Context
	cancel       context.CancelFunc
	observations int
}

func (c *previewCollisionLoopCancellation) Done() <-chan struct{} {
	c.observations++
	if c.observations == 2 {
		c.cancel()
	}
	return c.Context.Done()
}

func TestPreviewCancellationStopsCollisionPlanning(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{
		operationmode.OperationModeOrganize,
		operationmode.OperationModeInPlace,
		operationmode.OperationModeInPlaceNoRenameFolder,
	} {
		t.Run(string(mode), func(t *testing.T) {
			for _, phase := range []string{"active", "nil-context", "before-preview", "resolver", "first-plan", "collision-plan", "before-collision-loop", "next-collision", "last-missing-source", "source-stat", "primary-inspection", "primary-inspection-error", "multipart-directory", "multipart-read-error", "multipart-read", "multipart-sibling"} {
				t.Run(phase, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					base := afero.NewOsFs()
					root := t.TempDir()
					fs := &previewCancellationFS{Fs: base, ctx: ctx}
					files := make([]models.FileMatchInfo, 3)
					originals := make(map[string]string)
					for i := range files {
						dir := filepath.Join(root, fmt.Sprintf("input-%d", i))
						require.NoError(t, base.MkdirAll(dir, 0700))
						for _, name := range []string{"RAW-001.mp4", "RAW-001-cd2.mp4"} {
							path := filepath.Join(dir, name)
							originals[path] = "synthetic video " + name
							require.NoError(t, afero.WriteFile(base, path, []byte(originals[path]), 0600))
						}
						files[i] = models.FileMatchInfo{Path: filepath.Join(dir, "RAW-001.mp4"), Name: "RAW-001.mp4", Extension: ".mp4", MovieID: "TEST-001"}
					}
					strategy := &previewCancellationStrategy{
						OperationStrategy: organizer.ResolveStrategy(fs, &organizer.Config{OperationMode: mode, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, nil),
						ctx:               ctx, cancel: cancel,
					}
					wantCalls := 0
					switch phase {
					case "before-preview":
						cancel()
					case "first-plan":
						strategy.cancelAt, wantCalls = 1, 1
					case "collision-plan":
						strategy.cancelAt, wantCalls = 4, 4
					case "before-collision-loop":
						wantCalls = 3
					case "next-collision", "last-missing-source", "source-stat":
						wantCalls = 3
						fileIndex := 0
						if phase == "last-missing-source" {
							wantCalls, fileIndex = 5, 2
						}
						fs.beforeLstat = func(path string) error {
							if !strategy.inPlan && strategy.calls == wantCalls && path == files[fileIndex].Path {
								cancel()
								if phase != "source-stat" {
									return os.ErrNotExist
								}
							}
							return nil
						}
					case "primary-inspection", "primary-inspection-error":
						wantCalls = 4
						target := filepath.Join(root, "output", "TEST-001", "TEST-001.mp4")
						if mode != operationmode.OperationModeOrganize {
							target = filepath.Join(filepath.Dir(files[0].Path), "TEST-001.mp4")
						}
						fs.beforeLstat = func(path string) error {
							if !strategy.inPlan && strategy.calls == 4 && path == target {
								cancel()
								if phase == "primary-inspection-error" {
									return os.ErrPermission
								}
								return os.ErrNotExist
							}
							return nil
						}
					case "multipart-directory", "multipart-read-error":
						wantCalls = 4
						fs.beforeOpen = func(path string) error {
							if !strategy.inPlan && strategy.calls == 4 && path == filepath.Dir(files[0].Path) {
								cancel()
								if phase == "multipart-read-error" {
									return os.ErrPermission
								}
								return os.ErrNotExist
							}
							return nil
						}
					case "multipart-read":
						wantCalls = 4
						fs.afterReadDir = cancel
					case "multipart-sibling":
						wantCalls = 4
						target := filepath.Join(root, "output", "TEST-001", "TEST-001-cd2.mp4")
						if mode != operationmode.OperationModeOrganize {
							target = filepath.Join(filepath.Dir(files[0].Path), "TEST-001-cd2.mp4")
						}
						inspections := 0
						fs.beforeLstat = func(path string) error {
							if !strategy.inPlan && strategy.calls == 4 && path == target {
								inspections++
								if inspections == 2 {
									cancel()
									return os.ErrNotExist
								}
							}
							return nil
						}
					case "active", "nil-context":
						wantCalls = 6
					}
					resolverCalls := 0
					wf := &Workflow{preview: newPreviewOrchestrator(fs, nil, PreviewConfig{ResolveStrategy: func(operationmode.OperationMode) organizer.OperationStrategy {
						resolverCalls++
						if phase == "resolver" {
							cancel()
						}
						return strategy
					}}, nfo.NFONameConfig{}, nil, nil, nil)}
					var previewCtx context.Context = ctx
					if phase == "before-collision-loop" {
						previewCtx = &previewCollisionLoopCancellation{Context: ctx, cancel: cancel}
					}
					if phase == "nil-context" {
						previewCtx = nil
					}
					destination := filepath.Join(root, "output")
					result, err := wf.Preview(previewCtx, PreviewCmd{Movie: &models.Movie{ID: "TEST-001"}, FileResults: files, Destination: destination, OperationMode: mode, SkipNFO: true, SkipDownload: true})
					switch phase {
					case "before-preview":
						require.ErrorIs(t, err, context.Canceled)
						require.Nil(t, result)
						require.Zero(t, resolverCalls)
					case "active", "nil-context":
						require.NoError(t, err)
						require.Len(t, result.VideoFiles, 3)
						require.Len(t, result.VideoCollisions, 3, "active control must actually inspect multipart siblings")
						for _, collision := range result.VideoCollisions {
							require.Equal(t, "new", collision.Kind)
						}
					default:
						require.ErrorIs(t, ctx.Err(), context.Canceled)
						require.NoError(t, err, "preserve the mid-planning cancellation contract")
						require.Equal(t, &PreviewResult{OperationMode: mode}, result, "canceled plans must not leak paths or collisions")
					}
					require.Equal(t, wantCalls, strategy.calls)
					require.Zero(t, strategy.postCancelCalls)
					require.Zero(t, strategy.executeCalls)
					require.Empty(t, fs.postCancelCalls, "no new filesystem inspection after the cancellation boundary")
					for path, want := range originals {
						body, readErr := afero.ReadFile(base, path)
						require.NoError(t, readErr)
						require.Equal(t, want, string(body))
					}
					_, err = base.Stat(destination)
					require.ErrorIs(t, err, os.ErrNotExist)
				})
			}
		})
	}
}

func TestMultipartVideoInspectionCancellationBoundaries(t *testing.T) {
	for _, phase := range []string{"before-inspection", "directory-read", "between-siblings", "last-sibling"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := afero.NewOsFs()
			root := t.TempDir()
			source := filepath.Join(root, "RAW-001.mp4")
			target := filepath.Join(root, "output", "TEST-001.mp4")
			for _, name := range []string{"RAW-001.mp4", "RAW-001-cd2.mp4", "RAW-001-cd3.mp4"} {
				require.NoError(t, afero.WriteFile(base, filepath.Join(root, name), []byte("synthetic video"), 0600))
			}
			fs := &previewCancellationFS{Fs: base, ctx: ctx}
			switch phase {
			case "before-inspection":
				cancel()
			case "directory-read":
				fs.afterReadDir = cancel
			case "between-siblings", "last-sibling":
				name := "TEST-001-cd2.mp4"
				if phase == "last-sibling" {
					// Keep the last inspected sibling last in ReadDir's sorted order.
					require.NoError(t, base.Remove(source))
					name = "TEST-001-cd3.mp4"
				}
				destination := filepath.Join(filepath.Dir(target), name)
				inspections := 0
				fs.beforeLstat = func(path string) error {
					if path == destination {
						inspections++
						if inspections == 2 {
							cancel()
							return os.ErrNotExist
						}
					}
					return nil
				}
			}
			collisions, err := inspectMultipartVideoCollisions(ctx, fs, source, target, "cancel-boundary", nil)
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, collisions, "do not return partial collision decisions after cancellation")
			require.Empty(t, fs.postCancelCalls)
		})
	}
}

func TestPreviewCancellationDuringFinalContainerInspection(t *testing.T) {
	for _, cancelInspection := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelInspection), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := afero.NewOsFs()
			root := t.TempDir()
			source := filepath.Join(root, "RAW-001.mp4")
			destination := filepath.Join(root, "output")
			container := filepath.Join(destination, "TEST-001")
			require.NoError(t, base.MkdirAll(destination, 0700))
			require.NoError(t, afero.WriteFile(base, source, []byte("synthetic source"), 0600))
			require.NoError(t, afero.WriteFile(base, container, []byte("synthetic container blocker"), 0600))
			fs := &previewCancellationFS{Fs: base, ctx: ctx}
			mode := operationmode.OperationModeOrganize
			strategy := &previewCancellationStrategy{OperationStrategy: organizer.ResolveStrategy(fs, &organizer.Config{OperationMode: mode, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, nil), ctx: ctx, cancel: cancel}
			fs.beforeLstat = func(path string) error {
				if cancelInspection && strategy.calls == 2 && !strategy.inPlan && path == container {
					cancel()
				}
				return nil
			}
			wf := &Workflow{preview: newPreviewOrchestrator(fs, nil, PreviewConfig{ResolveStrategy: func(operationmode.OperationMode) organizer.OperationStrategy { return strategy }}, nfo.NFONameConfig{}, nil, nil, nil)}
			result, err := wf.Preview(ctx, PreviewCmd{Movie: &models.Movie{ID: "TEST-001"}, FileResults: []models.FileMatchInfo{{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: "TEST-001"}}, Destination: destination, OperationMode: mode, SkipNFO: true, SkipDownload: true})
			require.NoError(t, err)
			if cancelInspection {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.Equal(t, &PreviewResult{OperationMode: mode}, result)
			} else {
				require.Len(t, result.VideoCollisions, 1)
				require.Equal(t, "directory", result.VideoCollisions[0].Kind)
				require.Equal(t, container, result.VideoCollisions[0].Destination)
			}
			require.Equal(t, 2, strategy.calls)
			require.Zero(t, strategy.executeCalls)
			require.Empty(t, fs.postCancelCalls)
			for path, want := range map[string]string{source: "synthetic source", container: "synthetic container blocker"} {
				body, readErr := afero.ReadFile(base, path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(body))
			}
		})
	}
}
