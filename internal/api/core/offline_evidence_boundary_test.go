package core

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

func TestOfflineEvidenceBoundaryAbsentRuntime(t *testing.T) {
	// These exported methods explicitly support an absent runtime. Opting out
	// must remain inert, while opting in must fail before dependency access.
	var rt *APIRuntime
	require.Nil(t, rt.OfflineScrapeEvidence())
	release, err := rt.AdmitOfflineEvidenceOperation(false)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
	release()
	release, err = rt.AdmitOfflineEvidenceOperation(true)
	require.ErrorIs(t, err, ErrOfflineEvidenceShuttingDown)
	require.Nil(t, release)

	parent := t.Context()
	ctx, release, err := rt.BeginOfflineScrapeEvidence(parent, true)
	require.Same(t, parent, ctx)
	require.ErrorIs(t, err, ErrOfflineEvidenceShuttingDown)
	require.Nil(t, release)
	require.NoError(t, parent.Err())

	off, release, err := rt.BeginOfflineScrapeEvidence(parent, false)
	require.NoError(t, err)
	require.NotNil(t, off)
	require.Nil(t, scrapeevidence.Resolve(off, nil, false))
	release()
	release()
	require.NoError(t, parent.Err())
}

func TestOfflineEvidenceBoundaryNilContext(t *testing.T) {
	rt := NewAPIRuntime(nil)
	defer rt.Shutdown()

	// The public linker deliberately normalizes a missing context; neither
	// this nor the default-off path should admit work or initialize a reader.
	off, release := rt.LinkOfflineEvidenceOperationContext(nil, false)
	require.NotNil(t, off)
	require.NoError(t, off.Err())
	require.Nil(t, off.Done())
	release()
	release()
	require.NoError(t, off.Err())

	linked, release := rt.LinkOfflineEvidenceOperationContext(nil, true)
	require.NotNil(t, linked)
	require.NoError(t, linked.Err())
	require.NotNil(t, linked.Done())
	release()
	release()
	require.ErrorIs(t, linked.Err(), context.Canceled)
	require.NoError(t, rt.ServerCtx().Err(), "releasing a child must not cancel the server")

	// A subsequent link remains usable: cancellation is owned by each call.
	fresh, releaseFresh := rt.LinkOfflineEvidenceOperationContext(t.Context(), true)
	require.NoError(t, fresh.Err())
	releaseFresh()
	require.ErrorIs(t, fresh.Err(), context.Canceled)
	require.NoError(t, t.Context().Err())
}

func TestOfflineEvidenceBoundaryLinkAfterShutdown(t *testing.T) {
	rt := NewAPIRuntime(nil)
	rt.Shutdown()
	require.ErrorIs(t, rt.ServerCtx().Err(), context.Canceled)

	parent, cancelParent := context.WithCancel(t.Context())
	defer cancelParent()
	linked, release := rt.LinkOfflineEvidenceOperationContext(parent, true)
	// Cancellation must be visible on return, not eventually via AfterFunc.
	require.ErrorIs(t, linked.Err(), context.Canceled)
	release()
	release()
	require.NoError(t, parent.Err())

	off, releaseOff := rt.LinkOfflineEvidenceOperationContext(parent, false)
	require.Same(t, parent, off, "opt-out must not inherit server shutdown")
	releaseOff()
	require.NoError(t, off.Err())
	cancelParent()
	require.ErrorIs(t, off.Err(), context.Canceled)
}
