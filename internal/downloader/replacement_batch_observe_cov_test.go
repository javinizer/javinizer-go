package downloader

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// ObservePublishResultBound hits the `leg == nil` short-return when armed for a
// destination the batch never prepared (covers replacement_batch.go:203-204).
func TestObservePublishResultBound_NoLeg(t *testing.T) {
	b, err := NewReplacementBatch(afero.NewMemMapFs(), "op-1", nil)
	require.NoError(t, err)
	require.NoError(t, b.ObservePublishResultBound("/never-armed.mp4", nil))
}
