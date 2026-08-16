package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// codex P3 R6-5: a cleanup-exclusive grab must never cut in front of a QUEUED
// phase start — newly-admitted-or-queued intent blocks it.
func TestTryAdmitExclusive_RespectsQueuedPhase(t *testing.T) {
	b := newAdmissionBarrier()
	ctx := context.Background()

	// Held shared lease; queued phase parks behind it.
	releaseShared, err := b.AdmitShared()
	require.NoError(t, err)
	phaseDone := make(chan *phaseEntry, 1)
	go func() {
		entry, err := b.BeginPhase(ctx)
		if err == nil {
			phaseDone <- entry
		}
	}()
	// Wait until the queued phase registers pendingPhase.
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		pending := b.pendingPhase
		b.mu.Unlock()
		if pending > 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "phase never queued")
		time.Sleep(5 * time.Millisecond)
	}

	_, ok := b.TryAdmitExclusive()
	require.False(t, ok, "queued phase must block a cleanup-exclusive grab")

	releaseShared()
	var entry *phaseEntry
	select {
	case entry = <-phaseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("queued phase did not start after the shared lease released")
	}
	releasePhase := entry.Downgrade()
	releasePhase()

	// Nothing held or queued now — the grab succeeds.
	releaseEx, ok := b.TryAdmitExclusive()
	require.True(t, ok)
	releaseEx()
}
