package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/stretchr/testify/require"
)

func TestValidateManualInputIDCollisionsCoverage(t *testing.T) {
	t.Run("malformed override is ignored by the collision pass", func(t *testing.T) {
		err := validateManualInputIDCollisions(map[string]string{"/videos/OTHER-001.mp4": ""}, nil, []string{"/videos/OTHER-001.mp4"}, nil)
		require.NoError(t, err)
	})

	t.Run("filename-only duplicate siblings are allowed", func(t *testing.T) {
		fileMatcher, err := matcher.NewMatcher(&matcher.Config{})
		require.NoError(t, err)
		files := []string{"/a/IPX-535.mp4", "/b/IPX-535.mp4", "/c/OTHER-001.mp4"}
		err = validateManualInputIDCollisions(map[string]string{"/c/OTHER-001.mp4": ""}, nil, files, fileMatcher)
		require.NoError(t, err)
	})

	t.Run("multipart siblings with one override are allowed", func(t *testing.T) {
		fileMatcher, err := matcher.NewMatcher(&matcher.Config{})
		require.NoError(t, err)
		files := []string{"/a/IPX-535.mp4", "/b/IPX-535.mp4"}
		err = validateManualInputIDCollisions(map[string]string{"/a/IPX-535.mp4": "IPX-535"}, nil, files, fileMatcher)
		require.NoError(t, err)
	})

	t.Run("configured matcher participates in collision detection", func(t *testing.T) {
		fileMatcher, err := matcher.NewMatcher(&matcher.Config{
			RegexEnabled: true,
			RegexPattern: `([A-Z]+-\d+-X)`,
		})
		require.NoError(t, err)
		files := []string{"/a/ABC-123-X.mp4", "/b/unrelated.mp4"}
		err = validateManualInputIDCollisions(
			map[string]string{"/b/unrelated.mp4": "ABC-123-X"},
			nil,
			files,
			fileMatcher,
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "collides")
	})
}
