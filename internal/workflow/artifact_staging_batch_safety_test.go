package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

type cleanupRevalidationFS struct {
	afero.Fs
	root   string
	deny   bool
	swapAt string
	fired  bool
	aside  string
}

func (f *cleanupRevalidationFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.deny && filepath.Clean(path) == filepath.Clean(f.root) {
		return nil, false, errors.New("directory identity unavailable")
	}
	if fs, ok := f.Fs.(afero.Lstater); ok {
		return fs.LstatIfPossible(path)
	}
	info, err := f.Fs.Stat(path)
	return info, false, err
}

func (f *cleanupRevalidationFS) plant(path string) error {
	f.fired = true
	f.aside = path + ".retained"
	if err := f.Fs.Rename(path, f.aside); err != nil {
		return err
	}
	if err := f.Fs.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return afero.WriteFile(f.Fs, filepath.Join(path, "foreign.bin"), []byte("foreign"), 0o600)
}

func (f *cleanupRevalidationFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if !f.fired && f.swapAt == "proof write" && path == artifactStageProofPath(f.root) {
		if err := f.plant(f.root); err != nil {
			return nil, err
		}
	}
	return f.Fs.OpenFile(path, flag, mode)
}

func (f *cleanupRevalidationFS) Rename(old, next string) error {
	if !f.fired && f.swapAt == "proof carry" && old == artifactStageProofPath(f.root) {
		if err := f.plant(strings.TrimSuffix(next, artifactStageProofSuffix)); err != nil {
			return err
		}
	}
	return f.Fs.Rename(old, next)
}

func (f *cleanupRevalidationFS) RemoveAll(path string) error {
	if !f.fired && f.swapAt == "retry" {
		if err := f.plant(path); err != nil {
			return err
		}
		return errors.New("sharing violation after root replacement")
	}
	return f.Fs.RemoveAll(path)
}

func TestCleanupRetainsRootWhenIdentityIsUnavailable(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/library", 0o755))
	root := seedStagingRoot(t, base, "/library", nil)
	fs := &cleanupRevalidationFS{Fs: base, root: root, deny: true}
	stage := &artifactStage{fs: fs, root: root}
	stage.cleanup()
	body, err := afero.ReadFile(base, filepath.Join(root, ".source", "MIRD-281.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(body))
	assert.Equal(t, root, stage.root)
}

func TestCleanupRetainsForeignRootAcrossEveryProofBoundary(t *testing.T) {
	for _, seam := range []string{"proof write", "proof carry", "retry"} {
		t.Run(seam, func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, base.MkdirAll("/library", 0o755))
			root := seedStagingRoot(t, base, "/library", nil)
			fs := &cleanupRevalidationFS{Fs: base, root: root, swapAt: seam}
			stage := &artifactStage{fs: fs, root: root}
			stage.cleanup()
			require.True(t, fs.fired)
			foreign, err := afero.ReadFile(base, filepath.Join(stage.root, "foreign.bin"))
			require.NoError(t, err)
			assert.Equal(t, "foreign", string(foreign))
			original, err := afero.ReadFile(base, filepath.Join(fs.aside, ".source", "MIRD-281.mp4"))
			require.NoError(t, err)
			assert.Equal(t, "payload", string(original))
			sweepArtifactStaging(base, "/library")
			foreign, err = afero.ReadFile(base, filepath.Join(stage.root, "foreign.bin"))
			require.NoError(t, err, "a later sweep must not reuse the refused root's ownership proof")
			assert.Equal(t, "foreign", string(foreign))
		})
	}
}

type deferredSiblingLateFailureFS struct {
	afero.Fs
	kind      string
	suffix    string
	target    string
	inspects  int
	published bool
	fired     bool
}

func (f *deferredSiblingLateFailureFS) Rename(old, next string) error {
	if f.kind == "compensation refusal" && f.fired && filepath.Clean(old) == filepath.Clean(f.target) {
		return errors.New("compensation move refused")
	}
	if strings.HasSuffix(next, f.suffix) && strings.Contains(next, string(filepath.Separator)+"library"+string(filepath.Separator)) {
		f.target = next
		if f.kind == "source cleanup" && !f.published && strings.Contains(old, ".vac.") && !strings.Contains(old, ".nrstg.") {
			return fmt.Errorf("cross-device multipart move: %w", syscall.EXDEV)
		}
		if err := f.Fs.Rename(old, next); err != nil {
			return err
		}
		f.published = true
		return nil
	}
	return f.Fs.Rename(old, next)
}

func (f *deferredSiblingLateFailureFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if f.kind == "busy refusal" && strings.HasSuffix(path, f.suffix+fsutil.ReplacementBusySuffix) {
		f.fired = true
		return nil, errors.New("busy marker cannot be acquired")
	}
	if f.kind == "source cleanup" && f.published && !f.fired && strings.Count(path, ".vac.") >= 2 && strings.Contains(path, f.suffix+".vac.") {
		f.fired = true
		return nil, errors.New("claimed-source cleanup denied")
	}
	return f.Fs.OpenFile(path, flag, mode)
}

func (f *deferredSiblingLateFailureFS) Stat(path string) (os.FileInfo, error) {
	if f.published && filepath.Clean(path) == filepath.Clean(f.target) {
		f.inspects++
		failArm := (f.kind == "rollback arm" || f.kind == "compensation refusal") && f.inspects == 1
		failConfirm := f.kind == "confirmation" && f.inspects == 2
		if !f.fired && (failArm || failConfirm) {
			f.fired = true
			return nil, errors.New("bound observation temporarily unavailable")
		}
	}
	return f.Fs.Stat(path)
}

func TestDeferredMultipartLateFailureRestoresConsumedSources(t *testing.T) {
	for _, kind := range []string{"source cleanup", "rollback arm", "confirmation"} {
		t.Run(kind, func(t *testing.T) {
			var fs *deferredSiblingLateFailureFS
			env := buildDeferredArmEnv(t, "multipart-late-failure", func(base afero.Fs) afero.Fs {
				fs = &deferredSiblingLateFailureFS{Fs: base, kind: kind, suffix: "-cd2.mp4"}
				return fs
			}, nil)
			multipart := strings.TrimSuffix(env.source, filepath.Ext(env.source)) + "-cd2.mp4"
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			_, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			require.True(t, fs.published)
			require.True(t, fs.fired)
			body, readErr := afero.ReadFile(fs.Fs, multipart)
			require.NoError(t, readErr)
			assert.Equal(t, "part two", string(body))
			body, readErr = afero.ReadFile(fs.Fs, env.source)
			require.NoError(t, readErr)
			assert.Equal(t, "video", string(body))
			_, statErr := fs.Fs.Stat(fs.target)
			assert.True(t, os.IsNotExist(statErr), "failed apply must compensate its multipart install")
		})
	}
}

func TestDeferredOrganizerCompletedMovesKeepBoundIdentity(t *testing.T) {
	for _, suffix := range []string{".mp4", ".srt"} {
		t.Run(suffix, func(t *testing.T) {
			var fs *deferredSiblingLateFailureFS
			env := buildDeferredArmEnv(t, "organizer-completed-identity", func(base afero.Fs) afero.Fs {
				fs = &deferredSiblingLateFailureFS{Fs: base, kind: "source cleanup", suffix: suffix}
				return fs
			}, nil)
			if suffix == ".srt" {
				env.orch.revertLog = moveIntentDenyLog{env.orch.revertLog}
			}
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			_, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			require.True(t, fs.published)
			require.True(t, fs.fired)
			body, readErr := afero.ReadFile(fs.Fs, env.source)
			require.NoError(t, readErr)
			assert.Equal(t, "video", string(body))
			if suffix == ".srt" {
				subtitle := strings.TrimSuffix(env.source, filepath.Ext(env.source)) + ".srt"
				body, readErr = afero.ReadFile(fs.Fs, subtitle)
				require.NoError(t, readErr)
				assert.Equal(t, "subtitle", string(body))
			}
			_, statErr := fs.Fs.Stat(fs.target)
			assert.True(t, os.IsNotExist(statErr), "compensation uses the carried identity for a completed move")
		})
	}
}

func TestDeferredMultipartRetainsInstallWhenCompensationAlsoRefuses(t *testing.T) {
	var fs *deferredSiblingLateFailureFS
	env := buildDeferredArmEnv(t, "multipart-compensation-refusal", func(base afero.Fs) afero.Fs {
		fs = &deferredSiblingLateFailureFS{Fs: base, kind: "compensation refusal", suffix: "-cd2.mp4"}
		return fs
	}, nil)
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.ErrorContains(t, err, "restore deferred sibling after rollback arm refusal")
	require.True(t, fs.fired)
	body, readErr := afero.ReadFile(fs.Fs, fs.target)
	require.NoError(t, readErr)
	assert.Equal(t, "part two", string(body), "failed compensation must leave the installed payload recoverable")
	body, readErr = afero.ReadFile(fs.Fs, env.source)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(body))
}

func TestDeferredMultipartAdmissionAndOccupiedDestination(t *testing.T) {
	for _, kind := range []string{"occupied", "busy refusal", "source drift", "unknown admission"} {
		t.Run(kind, func(t *testing.T) {
			var fs *deferredSiblingLateFailureFS
			env := buildDeferredArmEnv(t, "multipart-admission", func(base afero.Fs) afero.Fs {
				fs = &deferredSiblingLateFailureFS{Fs: base, kind: kind, suffix: "-cd2.mp4"}
				return fs
			}, nil)
			multipart := strings.TrimSuffix(env.source, filepath.Ext(env.source)) + "-cd2.mp4"
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			planner := env.orch.organizer.(artifactPlanExecutor)
			plan, err := planner.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, MoveFiles: true})
			require.NoError(t, err)
			target := filepath.Join(plan.TargetDir, stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(multipart)))
			if kind == "unknown admission" {
				stage, stagedCmd, prepareErr := env.orch.prepareArtifact(t.Context(), cmd)
				require.NoError(t, prepareErr)
				defer stage.cleanup()
				for i := range stage.siblings {
					if stage.siblings[i].deferred {
						stage.siblings[i].identity.known = false
					}
				}
				state := &applyPipelineState{movie: &env.movie, artifact: stage}
				require.NoError(t, env.orch.stepOrganize(t.Context(), stagedCmd, state, &stepCompletion{}))
				err = stage.publish(t.Context(), env.orch, state, nil)
				require.ErrorContains(t, err, "admission identity unavailable")
			} else {
				if kind == "occupied" || kind == "source drift" {
					env.orch.organizer = armAfterExecOrganizer{deferredOrganizerBase{env.orch.organizer}, func() {
						if kind == "occupied" {
							require.NoError(t, afero.WriteFile(fs.Fs, target, []byte("foreign"), 0o600))
						} else {
							require.NoError(t, afero.WriteFile(fs.Fs, multipart, []byte("changed source"), 0o600))
						}
					}}
				}
				_, err = env.orch.Execute(t.Context(), cmd)
				if kind == "occupied" {
					require.NoError(t, err)
					body, readErr := afero.ReadFile(fs.Fs, target)
					require.NoError(t, readErr)
					assert.Equal(t, "foreign", string(body))
				} else {
					require.Error(t, err)
				}
			}
			body, readErr := afero.ReadFile(fs.Fs, multipart)
			require.NoError(t, readErr)
			if kind == "source drift" {
				assert.Equal(t, "changed source", string(body))
			} else {
				assert.Equal(t, "part two", string(body))
			}
		})
	}
}

func TestRemoveArtifactTreeWithRetryMissingAndRemovedNames(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	info, err := base.Stat("/stage")
	require.NoError(t, err)
	identity := captureArtifactStageDirIdentity(base, "/stage", info)
	fs := &notExistRemoveAllFS{Fs: base}
	require.NoError(t, removeArtifactTreeWithRetry(fs, "/stage", identity))
	require.NoError(t, base.RemoveAll("/stage"))
	require.NoError(t, removeArtifactTreeWithRetry(base, "/stage", identity))
}
