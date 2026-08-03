package worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActressSyncManagerSignalDropsDuplicateWake(t *testing.T) {
	manager := &ActressSyncManager{wake: make(chan struct{}, 1)}
	manager.signal()
	manager.signal()
	require.Len(t, manager.wake, 1)
}
