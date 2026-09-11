package organizer

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
)

func TestRemasterSeparatedFilenameDedicatedFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156-HD.mkv", nil, 0644))
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	strategy := newInPlaceStrategy(fs, &Config{}, m, nil)
	dedicated, err := strategy.isDedicatedFolder("/source", "RCT-156H", m)
	require.NoError(t, err)
	assert.True(t, dedicated)
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156.mkv", nil, 0644))
	dedicated, err = strategy.isDedicatedFolder("/source", "RCT-156H", m)
	require.NoError(t, err)
	assert.False(t, dedicated)
}
