package downloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// codex P1 (PR #276): the bound rollback arms refuse at every doubt point —
// never recording, arming, or re-anchoring identity a publish did not prove.

func TestSetRollbackOriginBoundRequiresBoundIdentity(t *testing.T) {
	batch, err := NewReplacementBatch(afero.NewMemMapFs(), "op", nil)
	require.NoError(t, err)
	defer func() { _ = batch }()
	require.ErrorContains(t, batch.SetRollbackOriginBound("/dest.mkv", "/origin.mkv", nil),
		"requires a bound installed identity")
}

func TestSetRollbackOriginBoundRefusesWhenInstallUnobservable(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	other := filepath.Join(dir, "elsewhere.mkv")
	otherOut := filepath.Join(dir, "installed.mkv")
	require.NoError(t, afero.WriteFile(fs, other, []byte("ours"), 0o644))
	identity, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, other, otherOut, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NotNil(t, identity)

	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	missing := filepath.Join(dir, "never-published.mkv")
	require.ErrorContains(t, batch.SetRollbackOriginBound(missing, filepath.Join(dir, "origin.mkv"), identity),
		"no longer names its bound installed output")
}

func TestSetRollbackOriginBoundBoundLegRefusesSuccessor(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.mkv")
	origin := filepath.Join(dir, "origin.mkv")
	require.NoError(t, afero.WriteFile(fs, dst, []byte("orig "), 0o644))

	batch, err := NewReplacementBatch(fs, "op", nil)
	require.NoError(t, err)
	require.NoError(t, batch.SetRollbackOrigin(dst, origin))

	foreignSrc := filepath.Join(dir, "foreign-src.mkv")
	foreignDst := filepath.Join(dir, "foreign-dst.mkv")
	require.NoError(t, afero.WriteFile(fs, foreignSrc, []byte("foreign"), 0o644))
	foreignID, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, foreignSrc, foreignDst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NotNil(t, foreignID)

	require.ErrorContains(t, batch.SetRollbackOriginBound(dst, origin, foreignID),
		"no longer names its bound installed output")

	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("orig "), got, "refusal retains the occupant byte-intact")
}

func TestObservedInstallIdentityNilInfo(t *testing.T) {
	assert.Nil(t, observedInstallIdentity(afero.NewMemMapFs(), "/anywhere.mkv", nil))
}
