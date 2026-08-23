package batch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAndSanitizeManualInputs_RejectsSubmittedIDCollision(t *testing.T) {
	files := []string{"/videos/OTHER-001.mp4", "/videos/IPX-535.mp4"}
	inputs := map[string]string{"/videos/OTHER-001.mp4": "IPX-535"}

	_, err := validateAndSanitizeManualInputs(inputs, files, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "collides")
}
