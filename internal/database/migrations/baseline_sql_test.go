package migrations

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBaselineSQLMatchesFilesystemBytes(t *testing.T) {
	providerSQL, err := fs.ReadFile(Filesystem(), "000001_baseline.sql")
	require.NoError(t, err)
	require.NotEmpty(t, providerSQL)
	require.Equal(t, providerSQL, []byte(BaselineSQL()))
}
