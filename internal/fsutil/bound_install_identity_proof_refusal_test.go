package fsutil

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6qLkJ9): the publish-identity-backed source proof
// fails closed on every doubt arm it owns — missing identity, missing object,
// and a strong identity the live filesystem cannot re-derive.

func TestVerifiedSourceProofFromBoundInstallIdentityRefusesNilIdentity(t *testing.T) {
	proof := VerifiedSourceProofFromBoundInstallIdentity(afero.NewMemMapFs(), nil)
	require.NotNil(t, proof)
	err := proof("/anywhere/file.mkv", nil)
	require.ErrorContains(t, err, "requires the identity its publish produced")
}

func TestVerifiedSourceProofFromBoundInstallIdentityRefusesMissingObject(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/installed.mkv", []byte("payload"), 0o644))
	info, err := fs.Stat("/installed.mkv")
	require.NoError(t, err)
	installed := NewWeakBoundInstallIdentity(info)
	require.NotNil(t, installed)

	proof := VerifiedSourceProofFromBoundInstallIdentity(fs, installed)
	require.ErrorContains(t, proof("/installed.mkv", nil), "found no regular-file object")
}

func TestVerifiedSourceProofFromBoundInstallIdentityFailsWhenStrongIdentityNotRederivable(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/installed.mkv", []byte("payload"), 0o644))
	info, err := fs.Stat("/installed.mkv")
	require.NoError(t, err)
	installed := newBoundInstallIdentity(info, 4242, 987654, true)
	require.NotNil(t, installed)

	proof := VerifiedSourceProofFromBoundInstallIdentity(fs, installed)
	cur, err := fs.Stat("/installed.mkv")
	require.NoError(t, err)
	err = proof("/installed.mkv", cur)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPublishCompleted))
	assert.ErrorContains(t, err, "cannot re-derive the strong identity")
}
