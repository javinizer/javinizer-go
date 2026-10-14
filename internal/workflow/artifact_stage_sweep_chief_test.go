package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// codex P1 (PRRT_kwDORn9KaM6qLkJz): a replant inside the authentication window
// leaves the identity the sweep authenticated OUT of sync with the tree about
// to be renamed+deleted — the rename must not reach it, the foreign content
// must stay byte-intact. The swap fires at the sweep's post-auth identity
// verify (the exact seam the binding closes).
func TestSweepArtifactStagingRefusesReplantBeforeRemove(t *testing.T) {
	base := afero.NewOsFs()
	parent := t.TempDir()
	seedStagingRoot(t, base, parent, nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)

	var victim string
	entries, err := afero.ReadDir(base, parent)
	require.NoError(t, err)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), artifactStageDirPrefix) {
			victim = filepath.Join(parent, e.Name())
		}
	}
	require.NotEmpty(t, victim)

	fs := &swapOnRootStatFS{Fs: base, victim: victim, plant: "foreign"}
	sweepArtifactStaging(fs, parent)
	require.True(t, fs.done, "the identity probe ran")

	entries, err = afero.ReadDir(base, parent)
	require.NoError(t, err)
	kept := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		_, readErr := afero.ReadFile(base, filepath.Join(parent, e.Name(), "foreign.bin"))
		if readErr == nil {
			kept = true
		}
	}
	assert.True(t, kept, "the replanted foreign tree must be retained byte-intact, never finalized for removal")
}

// swapOnRootStatFS replants the authenticated dir the FIRST time the sweep's
// identity capture path probes it — the same LstatIfPossible call
// lstatArtifactStageDir uses for the post-auth recheck and the final
// strong-identity gate.
type swapOnRootStatFS struct {
	afero.Fs
	victim string
	plant  string
	done   bool
}

func (f *swapOnRootStatFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if !f.done && filepath.Clean(name) == filepath.Clean(f.victim) {
		f.done = true
		_ = f.Fs.RemoveAll(f.victim)
		_ = f.Fs.MkdirAll(f.victim, 0o755)
		_ = afero.WriteFile(f.Fs, filepath.Join(f.victim, "foreign.bin"), []byte(f.plant), 0o644)
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}
