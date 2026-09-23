package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type replacementBatchRecorder struct {
	recordErr, confirmErr, releaseErr error
	recorded, confirmed, released     int
}

func (r *replacementBatchRecorder) RecordReplacement(context.Context, string, string, string, ...models.ReplacementBackupFacts) error {
	r.recorded++
	return r.recordErr
}
func (r *replacementBatchRecorder) ConfirmReplacement(context.Context, string, string, string) error {
	r.confirmed++
	return r.confirmErr
}
func (r *replacementBatchRecorder) ReleaseReplacement(context.Context, string, string, string) error {
	r.released++
	return r.releaseErr
}
func (r *replacementBatchRecorder) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return nil
}

func TestReplacementBatchValidationAndCreatedRollback(t *testing.T) {
	_, err := NewReplacementBatch(nil, "op", &replacementBatchRecorder{})
	require.Error(t, err)
	fs := afero.NewMemMapFs()
	b, err := NewReplacementBatch(fs, "op", &replacementBatchRecorder{})
	require.NoError(t, err)
	require.NoError(t, b.Preflight([]string{"/missing"}))
	require.Error(t, b.Preflight([]string{"/same", "/same"}))
	require.NoError(t, fs.MkdirAll("/dir", 0o755))
	require.Error(t, b.Preflight([]string{"/dir"}))
	require.NoError(t, afero.WriteFile(fs, "/regular", []byte("x"), 0o644))
	require.NoError(t, b.Preflight([]string{"/regular"}))

	replaced, err := b.BeforePublish(context.Background(), "/created", false)
	require.NoError(t, err)
	require.False(t, replaced)
	require.False(t, b.IsReplacement("/created"))
	require.Error(t, b.ConfirmPublish(context.Background(), "/unknown"))
	require.Error(t, b.ConfirmPublish(context.Background(), "/created"))
	require.NoError(t, afero.WriteFile(fs, "/created", []byte("new"), 0o644))
	require.NoError(t, b.ConfirmPublish(context.Background(), "/created"))
	require.NoError(t, b.Rollback(context.Background()))
	_, err = fs.Stat("/created")
	require.True(t, os.IsNotExist(err))
}

func TestReplacementBatchReplacementLifecycleAndFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		recorder ReplacementRecorder
		op       string
	}{
		{"overwrite-disabled", &replacementBatchRecorder{}, "op"},
		{"missing-recorder", nil, "op"},
		{"missing-operation", &replacementBatchRecorder{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
			b, err := NewReplacementBatch(fs, tc.op, tc.recorder)
			require.NoError(t, err)
			_, err = b.BeforePublish(ctx, "/dest", tc.name != "overwrite-disabled")
			require.Error(t, err)
			got, readErr := afero.ReadFile(fs, "/dest")
			require.NoError(t, readErr)
			require.Equal(t, "old", string(got))
		})
	}

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
	rec := &replacementBatchRecorder{}
	b, err := NewReplacementBatch(fs, "op", rec)
	require.NoError(t, err)
	replaced, err := b.BeforePublish(ctx, "/dest", true)
	require.NoError(t, err)
	require.True(t, replaced)
	require.True(t, b.IsReplacement(filepath.Clean("/dest")))
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("new"), 0o644))
	require.NoError(t, b.ConfirmPublish(ctx, "/dest"))
	require.Equal(t, 1, rec.confirmed)
	require.NoError(t, b.Rollback(ctx))
	require.Equal(t, 1, rec.released)
	got, err := afero.ReadFile(fs, "/dest")
	require.NoError(t, err)
	require.Equal(t, "old", string(got))

	fs2 := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs2, "/dest", []byte("old"), 0o644))
	bad := &replacementBatchRecorder{recordErr: errors.New("db down")}
	b2, err := NewReplacementBatch(fs2, "op", bad)
	require.NoError(t, err)
	_, err = b2.BeforePublish(ctx, "/dest", true)
	require.ErrorContains(t, err, "journal staged replacement")
	got, err = afero.ReadFile(fs2, "/dest")
	require.NoError(t, err)
	require.Equal(t, "old", string(got))
}

func TestReplacementBatchConfirmAndReleaseErrorsRemainActionable(t *testing.T) {
	ctx := context.Background()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
	rec := &replacementBatchRecorder{confirmErr: errors.New("confirm failed")}
	b, err := NewReplacementBatch(fs, "op", rec)
	require.NoError(t, err)
	_, err = b.BeforePublish(ctx, "/dest", true)
	require.NoError(t, err)
	require.NoError(t, fs.Mkdir("/dest", 0o755))
	require.Error(t, b.ConfirmPublish(ctx, "/dest"))
	require.NoError(t, fs.Remove("/dest"))
	require.NoError(t, afero.WriteFile(fs, "/dest", []byte("new"), 0o644))
	require.ErrorContains(t, b.ConfirmPublish(ctx, "/dest"), "confirm staged replacement")
	rec.confirmErr = nil
	rec.releaseErr = errors.New("release failed")
	require.ErrorContains(t, b.Rollback(ctx), "release rolled-back replacement")
	require.GreaterOrEqual(t, rec.recorded, 1)
}

func TestReplacementBatchDirectRecoveryStates(t *testing.T) {
	ctx := context.Background()
	t.Run("rollback unjournaled and stale installed identity", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/created", []byte("first"), 0o644))
		oldInfo, err := fs.Stat("/created")
		require.NoError(t, err)
		require.NoError(t, fs.Remove("/created"))
		require.NoError(t, afero.WriteFile(fs, "/created", []byte("foreign"), 0o644))
		b := &ReplacementBatch{fs: fs, legs: []*replacementBatchLeg{{destination: "/created", installed: true, installedID: oldInfo}}}
		require.ErrorContains(t, b.Rollback(ctx), "reverse staged publication")
	})
	t.Run("missing replacement backup", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		rec := &replacementBatchRecorder{}
		b := &ReplacementBatch{fs: fs, opID: "op", recorder: rec, legs: []*replacementBatchLeg{{destination: "/dest", backup: "/missing-backup", replaced: true}}}
		require.ErrorContains(t, b.Rollback(ctx), "restore staged replacement")
	})
}

func TestReplacementBatchRollbackOriginMovesInstalledOutputWithoutClobber(t *testing.T) {
	ctx := context.Background()
	t.Run("restores source and prior destination", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		rec := &replacementBatchRecorder{}
		require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/source", []byte("new"), 0o644))
		b, err := NewReplacementBatch(fs, "op", rec)
		require.NoError(t, err)
		_, err = b.BeforePublish(ctx, "/dest", true)
		require.NoError(t, err)
		require.NoError(t, fs.Rename("/source", "/dest"))
		require.NoError(t, b.ConfirmPublish(ctx, "/dest"))
		require.NoError(t, b.SetRollbackOrigin("/dest", "/source"))
		require.NoError(t, b.Rollback(ctx))
		require.Equal(t, "new", string(mustReadReplacementBatch(t, fs, "/source")))
		require.Equal(t, "old", string(mustReadReplacementBatch(t, fs, "/dest")))
	})

	t.Run("foreign source claimant preserves every recovery object", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		rec := &replacementBatchRecorder{}
		require.NoError(t, afero.WriteFile(fs, "/dest", []byte("old"), 0o644))
		b, err := NewReplacementBatch(fs, "op", rec)
		require.NoError(t, err)
		_, err = b.BeforePublish(ctx, "/dest", true)
		require.NoError(t, err)
		require.NoError(t, afero.WriteFile(fs, "/dest", []byte("new"), 0o644))
		require.NoError(t, b.ConfirmPublish(ctx, "/dest"))
		require.NoError(t, b.SetRollbackOrigin("/dest", "/source"))
		require.NoError(t, afero.WriteFile(fs, "/source", []byte("foreign"), 0o644))
		require.Error(t, b.Rollback(ctx))
		require.Equal(t, "foreign", string(mustReadReplacementBatch(t, fs, "/source")))
		require.Equal(t, "new", string(mustReadReplacementBatch(t, fs, "/dest")))
		leg := b.find("/dest")
		require.NotNil(t, leg)
		require.Equal(t, "old", string(mustReadReplacementBatch(t, fs, leg.backup)))
	})
}

func mustReadReplacementBatch(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
