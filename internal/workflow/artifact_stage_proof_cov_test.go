package workflow

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Round-50 coverage: artifact_stage_sweep.go:253-255 — readArtifactStageProof
// must refuse a well-formed manifest that carries no valid seal (the round-49
// security leg: attackers can mint a manifest with every publicly observable
// field, only the local-secret seal separates a genuine owner from a forgery).
func TestReadArtifactStageProof_UnsealedStep(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/stage/.javinizer-apply-unsealed"
	require.NoError(t, fs.MkdirAll(root, 0o755))

	host, herr := artifactSweepHostname()
	require.NoError(t, herr)
	// Carry every public field at plausible values — version, token, hostname,
	// PID, completed stamp — but no seal. The verifier must refuse even here.
	m := &artifactStageManifest{
		Version:           artifactStageManifestVer,
		Token:             artifactStageManifestToken(root),
		Hostname:          host,
		PID:               7,
		CompletedUnixNano: 1,
	}
	data, err := artifactSweepMarshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, artifactStageProofPath(root), data, 0o600))

	require.False(t, readArtifactStageProof(fs, root), "an unsealed full-evidence manifest proves nothing")
}
