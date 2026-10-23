package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type issue277OpaqueFs struct{ afero.Fs }

func TestIssue277UnprovenNoFollowCapabilityRefusesReplacement(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-opaque")
	cmd, plan := issue277Command(t, env, true)
	require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign"), 0644))
	cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
	env.orch.fs = &issue277OpaqueFs{Fs: env.fs}
	collision, err := inspectVideoCollision(t.Context(), env.orch.fs, env.source, plan.TargetPath, "opaque-review")
	require.NoError(t, err)
	require.NotNil(t, collision)
	require.Equal(t, "unknown", collision.Kind)
	require.Empty(t, collision.ReviewToken)
	_, err = env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.FileExists(t, env.source)
	body, err := os.ReadFile(plan.TargetPath)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(body))
}

type issue277CancelReadFs struct {
	afero.Fs
	cancel context.CancelFunc
}

func (fs *issue277CancelReadFs) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return fs.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (fs *issue277CancelReadFs) Open(path string) (afero.File, error) {
	file, err := fs.Fs.Open(path)
	if err != nil {
		return nil, err
	}
	return &issue277CancelReadFile{File: file, cancel: fs.cancel}, nil
}

type issue277CancelReadFile struct {
	afero.File
	cancel context.CancelFunc
}

func (file *issue277CancelReadFile) Read(data []byte) (int, error) {
	n, err := file.File.Read(data)
	file.cancel()
	return n, err
}

func TestIssue277CancellationDuringSnapshotRemainsCancellation(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-cancel-read")
	cmd, plan := issue277Command(t, env, true)
	require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	require.NoError(t, os.WriteFile(plan.TargetPath, []byte("foreign"), 0644))
	cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	env.orch.fs = &issue277CancelReadFs{Fs: env.fs, cancel: cancel}
	_, err := env.orch.Execute(ctx, cmd)
	require.ErrorIs(t, err, context.Canceled)
	require.FileExists(t, env.source)
	body, err := os.ReadFile(plan.TargetPath)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(body))
}

func TestIssue277SourceRewriteAtConsumption(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{true: "move", false: "copy"}[move], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "issue277-consume")
			cmd, plan := issue277Command(t, env, move)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
			require.NoError(t, os.WriteFile(plan.TargetPath, []byte("reviewed video"), 0644))
			cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
			env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) {
				info, err := os.Stat(env.source)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(env.source, []byte("other"), 0644))
				require.NoError(t, os.Chtimes(env.source, info.ModTime(), info.ModTime()))
			}}
			_, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			var conflict *videocollision.ConflictError
			require.True(t, errors.As(err, &conflict), "%v", err)
			require.Equal(t, "VIDEO_COLLISION_STALE", conflict.Code)
			body, err := os.ReadFile(plan.TargetPath)
			require.NoError(t, err)
			require.Equal(t, "reviewed video", string(body))
			body, err = os.ReadFile(env.source)
			require.NoError(t, err)
			require.Equal(t, "other", string(body))
		})
	}
}

func TestIssue277ReviewedTargetCannotBecomeResident(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-late-resident")
	cmd, plan := issue277Command(t, env, true)
	require.NoError(t, os.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	require.NoError(t, os.WriteFile(plan.TargetPath, []byte("reviewed video"), 0644))
	cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: issue277Decision(t, env, cmd, plan)}
	env.orch.artifactPrepared = func() {
		require.NoError(t, os.Rename(plan.TargetPath, plan.TargetPath+".reviewed"))
		require.NoError(t, os.Link(env.source, plan.TargetPath))
	}
	_, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	var conflict *videocollision.ConflictError
	require.True(t, errors.As(err, &conflict))
	require.Equal(t, "VIDEO_COLLISION_STALE", conflict.Code)
	require.FileExists(t, env.source)
	body, err := os.ReadFile(plan.TargetPath + ".reviewed")
	require.NoError(t, err)
	require.Equal(t, "reviewed video", string(body))
}
