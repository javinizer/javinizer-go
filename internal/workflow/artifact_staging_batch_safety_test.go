package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
)

type cleanupRevalidationFS struct {
	afero.Fs
	root          string
	deny          bool
	denyIdentity  bool
	matchMetadata bool
	swapAt        string
	fired         bool
	swapErr       error
	aside         string
}

type unavailableStageNativeInfo struct {
	os.FileInfo
}

func (unavailableStageNativeInfo) Sys() any {
	return struct{}{}
}

func (f *cleanupRevalidationFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.deny && filepath.Clean(path) == filepath.Clean(f.root) {
		return nil, false, errors.New("directory identity unavailable")
	}
	var info os.FileInfo
	var err error
	var lstat bool
	if fs, ok := f.Fs.(afero.Lstater); ok {
		info, lstat, err = fs.LstatIfPossible(path)
	} else {
		info, err = f.Fs.Stat(path)
	}
	if err == nil && f.denyIdentity && filepath.Clean(path) == filepath.Clean(f.root) {
		info = unavailableStageNativeInfo{FileInfo: info}
	}
	return info, lstat, err
}

func (f *cleanupRevalidationFS) plant(path string) error {
	info, err := f.Fs.Stat(path)
	if err != nil {
		return err
	}
	f.fired = true
	f.aside = path + ".retained"
	if err := f.Fs.Rename(path, f.aside); err != nil {
		f.swapErr = err
		return err
	}
	if err := f.Fs.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := afero.WriteFile(f.Fs, filepath.Join(path, "foreign.bin"), []byte("foreign"), 0o600); err != nil {
		return err
	}
	stamp := info.ModTime().Add(time.Second)
	if f.matchMetadata {
		stamp = info.ModTime()
	}
	return f.Fs.Chtimes(path, stamp, stamp)
}

func (f *cleanupRevalidationFS) Open(path string) (afero.File, error) {
	file, err := f.Fs.Open(path)
	if err == nil && (!f.fired || f.swapErr != nil) && f.swapAt == "retry" && strings.Contains(path, artifactStageQuarantineMark) {
		if info, statErr := file.Stat(); statErr == nil && info.IsDir() {
			if err := f.plant(path); err != nil {
				_ = file.Close()
				return nil, err
			}
		}
	}
	return file, err
}

func (f *cleanupRevalidationFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if !f.fired && f.swapAt == "completion stamp" && strings.HasPrefix(path, filepath.Join(f.root, artifactStageManifestName)+".jv-stage.") {
		if err := f.plant(f.root); err != nil {
			return nil, err
		}
	}
	if !f.fired && f.swapAt == "proof write" && strings.HasPrefix(path, artifactStageProofPath(f.root)+".jv-stage.") {
		if err := f.plant(f.root); err != nil {
			return nil, err
		}
	}
	return f.Fs.OpenFile(path, flag, mode)
}

func (f *cleanupRevalidationFS) Rename(old, next string) error {
	if !f.fired && f.swapAt == "quarantine rename" && old == f.root {
		if err := f.plant(f.root); err != nil {
			return err
		}
	}
	if !f.fired && f.swapAt == "proof carry" && old == artifactStageProofPath(f.root) {
		if err := f.plant(strings.TrimSuffix(next, artifactStageProofSuffix)); err != nil {
			return err
		}
	}
	err := f.Fs.Rename(old, next)
	if err == nil && !f.fired && f.swapAt == "proof publication" && next == artifactStageProofPath(f.root) {
		return f.plant(f.root)
	}
	return err
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
	for _, unavailable := range []string{"lookup error", "kernel identity unavailable"} {
		t.Run(unavailable, func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, base.MkdirAll("/library", 0o755))
			root := seedStagingRoot(t, base, "/library", nil)
			fs := &cleanupRevalidationFS{Fs: base, root: root, deny: unavailable == "lookup error", denyIdentity: unavailable == "kernel identity unavailable"}
			stage := &artifactStage{fs: fs, root: root}
			stage.cleanup()
			body, err := afero.ReadFile(base, filepath.Join(root, ".source", "MIRD-281.mp4"))
			require.NoError(t, err)
			assert.Equal(t, "payload", string(body))
			assert.Equal(t, root, stage.root)
			proofExists, err := afero.Exists(base, artifactStageProofPath(root))
			require.NoError(t, err)
			assert.False(t, proofExists)
			_, reclaimable := artifactStageReclaimableIdentity(fs, root)
			assert.False(t, reclaimable)
		})
	}
}

func TestCleanupRetainsForeignRootAcrossEveryProofBoundary(t *testing.T) {
	for _, seam := range []string{"completion stamp", "proof write", "proof publication", "quarantine rename", "proof carry", "retry"} {
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

func TestCleanupRetainsNativeForeignRootWithMatchingTimestamp(t *testing.T) {
	for _, seam := range []string{"completion stamp", "proof write", "proof publication", "quarantine rename", "proof carry", "retry"} {
		t.Run(seam, func(t *testing.T) {
			base := afero.NewOsFs()
			parent := t.TempDir()
			root := seedStagingRoot(t, base, parent, nil)
			info, err := base.Stat(root)
			require.NoError(t, err)
			require.True(t, captureArtifactStageDirIdentity(base, root, info).hasDevIno)
			fs := &cleanupRevalidationFS{Fs: base, root: root, swapAt: seam, matchMetadata: true}
			stage := &artifactStage{fs: fs, root: root}
			stage.cleanup()
			require.True(t, fs.fired)
			if runtime.GOOS == "windows" && seam == "retry" && fs.swapErr != nil {
				require.ErrorIs(t, fs.swapErr, syscall.Errno(32))
				owned, readErr := afero.ReadFile(base, filepath.Join(stage.root, ".source", "MIRD-281.mp4"))
				require.NoError(t, readErr)
				assert.Equal(t, "payload", string(owned))
				return
			}
			foreign, err := afero.ReadFile(base, filepath.Join(stage.root, "foreign.bin"))
			require.NoError(t, err)
			assert.Equal(t, "foreign", string(foreign))
			original, err := afero.ReadFile(base, filepath.Join(fs.aside, ".source", "MIRD-281.mp4"))
			require.NoError(t, err)
			assert.Equal(t, "payload", string(original))
			ownedInfo, err := base.Stat(fs.aside)
			require.NoError(t, err)
			foreignInfo, err := base.Stat(stage.root)
			require.NoError(t, err)
			if seam != "completion stamp" {
				assert.Equal(t, ownedInfo.ModTime(), foreignInfo.ModTime())
			}
			sweepArtifactStaging(base, parent)
			foreign, err = afero.ReadFile(base, filepath.Join(stage.root, "foreign.bin"))
			require.NoError(t, err)
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
	for _, tc := range []struct {
		name     string
		policy   videocollision.Policy
		decision bool
	}{
		{name: "occupied"},
		{name: "occupied typed policy", policy: videocollision.Fail},
		{name: "occupied typed decision", decision: true},
		{name: "busy refusal"},
		{name: "source drift"},
		{name: "unknown admission"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := tc.name
			occupied := strings.HasPrefix(kind, "occupied")
			var fs *deferredSiblingLateFailureFS
			env := buildDeferredArmEnv(t, "multipart-admission", func(base afero.Fs) afero.Fs {
				fs = &deferredSiblingLateFailureFS{Fs: base, kind: kind, suffix: "-cd2.mp4"}
				return fs
			}, nil)
			multipart := strings.TrimSuffix(env.source, filepath.Ext(env.source)) + "-cd2.mp4"
			sourceBefore, err := fs.Fs.Stat(multipart)
			require.NoError(t, err)
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			cmd.Organize.VideoCollisionPolicy = tc.policy
			planner := env.orch.organizer.(artifactPlanExecutor)
			plan, err := planner.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, MoveFiles: true})
			require.NoError(t, err)
			target := filepath.Join(plan.TargetDir, stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(multipart)))
			if tc.decision {
				cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: {Source: env.source, Destination: plan.TargetPath, Policy: videocollision.Fail}}
			}
			var targetBefore os.FileInfo
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
				if occupied || kind == "source drift" {
					env.orch.organizer = armAfterExecOrganizer{deferredOrganizerBase{env.orch.organizer}, func() {
						if occupied {
							require.NoError(t, afero.WriteFile(fs.Fs, target, []byte("foreign"), 0o600))
							var statErr error
							targetBefore, statErr = fs.Fs.Stat(target)
							require.NoError(t, statErr)
						} else {
							require.NoError(t, afero.WriteFile(fs.Fs, multipart, []byte("changed source"), 0o600))
						}
					}}
				}
				_, err = env.orch.Execute(t.Context(), cmd)
				if occupied {
					if tc.policy != "" || tc.decision {
						var conflict *videocollision.ConflictError
						require.ErrorAs(t, err, &conflict)
						assert.Equal(t, "VIDEO_COLLISION_REFRESHED", conflict.Code)
						assert.Equal(t, multipart, conflict.Collision.Source)
						assert.Equal(t, target, conflict.Collision.Destination)
						assert.Equal(t, "multipart", conflict.Collision.Kind)
					} else {
						require.NoError(t, err)
					}
					body, readErr := afero.ReadFile(fs.Fs, target)
					require.NoError(t, readErr)
					assert.Equal(t, "foreign", string(body))
					targetAfter, statErr := fs.Fs.Stat(target)
					require.NoError(t, statErr)
					assert.True(t, os.SameFile(targetBefore, targetAfter))
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
				sourceAfter, statErr := fs.Fs.Stat(multipart)
				require.NoError(t, statErr)
				assert.True(t, os.SameFile(sourceBefore, sourceAfter))
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
