package batch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateManualInputIDCollisionsCoverage(t *testing.T) {
	t.Run("malformed override is ignored by the collision pass", func(t *testing.T) {
		err := validateManualInputIDCollisions(map[string]string{"/videos/OTHER-001.mp4": ""}, nil, []string{"/videos/OTHER-001.mp4"})
		require.NoError(t, err)
	})

	t.Run("filename-only duplicate siblings are allowed", func(t *testing.T) {
		files := []string{"/a/IPX-535.mp4", "/b/IPX-535.mp4", "/c/OTHER-001.mp4"}
		err := validateManualInputIDCollisions(map[string]string{"/c/OTHER-001.mp4": ""}, nil, files)
		require.NoError(t, err)
	})

	t.Run("multipart siblings with one override are allowed", func(t *testing.T) {
		files := []string{"/a/IPX-535.mp4", "/b/IPX-535.mp4"}
		err := validateManualInputIDCollisions(map[string]string{"/a/IPX-535.mp4": "IPX-535"}, nil, files)
		require.NoError(t, err)
	})
}
