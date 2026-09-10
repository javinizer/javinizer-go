//go:build windows

package fsutil

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestNoClobberProbeWindowsBypass(t *testing.T) {
	for _, fs := range []afero.Fs{afero.NewOsFs(), afero.NewMemMapFs()} {
		require.NoError(t, ProbeNoClobberPublish(fs, `Z:\missing\directory`))
	}
}
