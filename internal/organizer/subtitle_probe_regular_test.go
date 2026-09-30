package organizer

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonLstaterFs wraps an afero.Fs without surfacing the afero.Lstater
// interface: the embedded interface promotes only the afero.Fs method set, so
// probeSubtitleSourceRegular must answer through the following Stat fallback —
// the path every Lstater-less filesystem (HTTP, SFTP, filtered views) takes.
type nonLstaterFs struct{ afero.Fs }

// A filesystem with no no-FOLLOW view answers regularity through the
// following Stat: an in-memory filesystem has no symlink model, so every
// resolvable source is regular-or-absent by construction.
func TestProbeSubtitleSourceRegularNonLstaterFs(t *testing.T) {
	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, "/ABC-123.srt", []byte("subtitle"), 0o600))
	fallback := nonLstaterFs{Fs: mem}
	_, isLstater := interface{}(fallback).(afero.Lstater)
	require.False(t, isLstater, "wrapper must hide the Lstater view to exercise the Stat fallback")
	assert.True(t, probeSubtitleSourceRegular(fallback, "/ABC-123.srt"),
		"following Stat on a present file answers regular")
}

// A failed lookup keeps the subtitle entry: the probe stays advisory for
// transient errors, and admission-bound sources are re-proven at consumption,
// so a source that cannot be stated is never dropped from the enumeration.
func TestProbeSubtitleSourceRegularLookupFailureKeepsEntry(t *testing.T) {
	mem := afero.NewMemMapFs()
	// Lstater path: LstatIfPossible errors, then the Stat fallback errors too.
	assert.True(t, probeSubtitleSourceRegular(mem, "/does-not-exist.srt"),
		"lstat+stat lookup failure keeps the entry (advisory probe)")
	// Non-Lstater path: the following Stat itself fails.
	assert.True(t, probeSubtitleSourceRegular(nonLstaterFs{Fs: mem}, "/does-not-exist.srt"),
		"stat lookup failure on a non-Lstater fs keeps the entry")
}
