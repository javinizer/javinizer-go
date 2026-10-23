package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type collisionPlanOrganizer struct {
	*organizer.Organizer
	plan *organizer.OrganizePlan
	err  error
}

func (o *collisionPlanOrganizer) PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	return o.plan, o.err
}

func TestVideoCollisionPreflightRefusalGates(t *testing.T) {
	for _, fault := range []string{"invalid-policy", "no-executor", "plan-error", "invalid-decision", "reviewed-skip", "container-fail", "container-skip", "inspect-error", "multipart-read-error", "multipart-skip"} {
		t.Run(fault, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-preflight-fault")
			cmd, plan := issue277Command(t, env, true)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0700))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign video"), 0600))
			planner := &collisionPlanOrganizer{Organizer: env.org, plan: plan}
			env.orch.organizer = planner
			switch fault {
			case "invalid-policy":
				cmd.Organize.VideoCollisionPolicy = "invalid"
			case "no-executor":
				env.orch.organizer = noPlanOrganizer{}
				cmd.Organize.VideoCollisionPolicy = videocollision.Skip
			case "plan-error":
				planner.err = os.ErrPermission
			case "invalid-decision":
				cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: {Source: env.source, Destination: plan.TargetPath, Policy: "invalid"}}
			case "reviewed-skip":
				decision := issue277Decision(t, env, cmd, plan)
				decision.Policy = videocollision.Skip
				cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: decision}
			case "container-fail", "container-skip":
				plan.Conflicts = []organizer.PlanConflict{{Path: filepath.Dir(plan.TargetPath), Kind: organizer.ConflictDirectory}}
				if fault == "container-skip" {
					cmd.Organize.VideoCollisionPolicy = videocollision.Skip
				}
			case "inspect-error":
				env.orch.fs = &collisionFaultFS{Fs: env.fs, lstat: func(path string) (os.FileInfo, bool, error) {
					if path == plan.TargetPath {
						return nil, true, os.ErrPermission
					}
					return env.fs.(afero.Lstater).LstatIfPossible(path)
				}}
			case "multipart-read-error":
				require.NoError(t, os.Remove(plan.TargetPath))
				env.orch.fs = &collisionFaultFS{Fs: env.fs, open: func(path string) (afero.File, error) {
					if path == filepath.Dir(env.source) {
						return nil, os.ErrPermission
					}
					return env.fs.Open(path)
				}}
			case "multipart-skip":
				require.NoError(t, os.Remove(plan.TargetPath))
				target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(env.source), filepath.Base(plan.TargetPath), filepath.Base(env.multipart)))
				require.NoError(t, os.WriteFile(target, []byte("unowned sibling"), 0600))
				cmd.Organize.VideoCollisionPolicy = videocollision.Skip
			}
			skipped, err := env.orch.preflightVideoCollision(t.Context(), &cmd)
			if fault == "reviewed-skip" || fault == "container-skip" || fault == "multipart-skip" {
				require.NoError(t, err)
				require.True(t, skipped)
			} else {
				require.Error(t, err)
				require.False(t, skipped)
				if fault == "plan-error" || fault == "inspect-error" || fault == "multipart-read-error" {
					require.ErrorIs(t, err, os.ErrPermission)
				}
			}
			body, readErr := os.ReadFile(env.source)
			require.NoError(t, readErr)
			require.Equal(t, "video", string(body))
			if fault != "multipart-read-error" && fault != "multipart-skip" {
				body, readErr = os.ReadFile(plan.TargetPath)
				require.NoError(t, readErr)
				require.Equal(t, "foreign video", string(body))
			}
		})
	}
}

func TestVideoContainerCollisionKindsAndSizes(t *testing.T) {
	for _, tc := range []struct {
		kind organizer.ConflictKind
		want string
	}{
		{organizer.ConflictDirectory, "directory"}, {organizer.ConflictSymlink, "symlink"}, {organizer.ConflictKind(99), "unknown"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			fs, source, target := collisionFixture(t)
			container := filepath.Join(filepath.Dir(target), "container")
			require.NoError(t, afero.WriteFile(fs, container, []byte("blocked container"), 0600))
			plan := &organizer.OrganizePlan{Conflicts: []organizer.PlanConflict{
				{Path: target, Kind: organizer.ConflictDirectory}, {Path: "ignored-file", Kind: organizer.ConflictFile}, {Path: "ignored-duplicate", Kind: organizer.ConflictDuplicate}, {Path: container, Kind: tc.kind},
			}}
			collision := videoContainerCollision(fs, plan, source, target)
			require.NotNil(t, collision)
			require.Equal(t, tc.want, collision.Kind)
			require.Equal(t, source, collision.SelectedSource)
			require.Equal(t, int64(len("source video")), collision.SourceSize)
			require.Equal(t, int64(len("blocked container")), collision.DestinationSize)
			assertCollisionFilesUnchanged(t, fs, source, target)
		})
	}
}

func TestMultipartVideoInspectionRefusesFaults(t *testing.T) {
	for _, fault := range []string{"readdir", "lstat", "nonregular", "collision-read"} {
		t.Run(fault, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			sibling := filepath.Join(filepath.Dir(source), "ABC-123-cd2.mp4")
			destination := filepath.Join(filepath.Dir(target), "target-cd2.mp4")
			require.NoError(t, afero.WriteFile(base, sibling, []byte("part two"), 0600))
			require.NoError(t, afero.WriteFile(base, destination, []byte("foreign part"), 0600))
			fs := &collisionFaultFS{Fs: base}
			fs.open = func(path string) (afero.File, error) {
				if fault == "readdir" && path == filepath.Dir(source) || fault == "collision-read" && path == sibling {
					return nil, os.ErrPermission
				}
				return base.Open(path)
			}
			fs.lstat = func(path string) (os.FileInfo, bool, error) {
				if path == sibling && fault == "lstat" {
					return nil, true, os.ErrPermission
				}
				info, noFollow, err := base.(afero.Lstater).LstatIfPossible(path)
				if path == sibling && fault == "nonregular" && info != nil {
					info = symlinkModeInfo{info}
				}
				return info, noFollow, err
			}
			collisions, err := inspectMultipartVideoCollisions(t.Context(), fs, source, target, "scope", nil)
			if fault == "nonregular" {
				require.NoError(t, err)
				require.Empty(t, collisions)
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			assertCollisionFilesUnchanged(t, base, source, target)
			for path, want := range map[string]string{sibling: "part two", destination: "foreign part"} {
				body, err := afero.ReadFile(base, path)
				require.NoError(t, err)
				require.Equal(t, want, string(body))
			}
		})
	}
}

func TestPreviewVideoCollisionFaults(t *testing.T) {
	for _, fault := range []string{"plan", "container", "inspect", "multipart"} {
		t.Run(fault, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			fs := &collisionFaultFS{Fs: base}
			strategy := &mockStrategyPlan{plan: &organizer.OrganizePlan{SourcePath: source, TargetPath: target, TargetDir: filepath.Dir(target), BaseFileName: "target"}}
			switch fault {
			case "plan":
				strategy.err = os.ErrPermission
			case "container":
				strategy.plan.Conflicts = []organizer.PlanConflict{{Path: filepath.Dir(target), Kind: organizer.ConflictDirectory}}
			case "inspect":
				fs.open = func(string) (afero.File, error) { return nil, os.ErrPermission }
			case "multipart":
				fs.open = func(path string) (afero.File, error) {
					if path == filepath.Dir(source) {
						return nil, os.ErrPermission
					}
					return base.Open(path)
				}
			}
			preview := newPreviewOrchestrator(fs, nil, PreviewConfig{ResolveStrategy: func(operationmode.OperationMode) organizer.OperationStrategy { return strategy }}, nfo.NFONameConfig{}, nil, nil, nil)
			result, err := preview.Execute(t.Context(), PreviewCmd{Movie: &models.Movie{ID: "ABC-123"}, FileResults: []models.FileMatchInfo{{Path: source, Name: filepath.Base(source), Extension: ".mp4"}}, Destination: filepath.Dir(target), OperationMode: operationmode.OperationModeOrganize, SkipNFO: true, SkipDownload: true})
			if fault == "container" {
				require.NoError(t, err)
				require.Len(t, result.VideoCollisions, 1)
				require.Equal(t, "directory", result.VideoCollisions[0].Kind)
				require.Equal(t, filepath.Dir(target), result.VideoCollisions[0].Destination)
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
				require.Nil(t, result)
			}
			assertCollisionFilesUnchanged(t, base, source, target)
		})
	}
}
