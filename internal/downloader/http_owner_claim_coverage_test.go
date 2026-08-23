package downloader

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimeDownloadOwnersGuardsAndPreservesExistingClaim(t *testing.T) {
	var nilDedup *sync.Map
	PrimeDownloadOwners(nilDedup, map[string]string{"movie": "owner"})

	dedup := &sync.Map{}
	existing := &downloadOwnerClaim{logicalKey: "taken", ownerKey: "old", done: make(chan struct{})}
	dedup.Store(ownerClaimKey("taken"), existing)
	PrimeDownloadOwners(dedup, map[string]string{
		"":        "owner",
		"blank":   " ",
		"taken":   "new",
		" fresh ": " owner ",
	})

	value, ok := dedup.Load(ownerClaimKey("taken"))
	require.True(t, ok)
	assert.Same(t, existing, value)
	value, ok = dedup.Load(ownerClaimKey("fresh"))
	require.True(t, ok)
	claim, ok := value.(*downloadOwnerClaim)
	require.True(t, ok)
	assert.Equal(t, "fresh", claim.logicalKey)
	assert.Equal(t, "owner", claim.ownerKey)
}

func TestDownloadOwnerClaimDestinationAndOutcome(t *testing.T) {
	claim := &downloadOwnerClaim{done: make(chan struct{})}
	assert.True(t, claim.bindDestination("/dest"))
	assert.True(t, claim.bindDestination("/dest"))
	assert.False(t, claim.bindDestination("/other"))
	claim.complete(true)
	dest, success := claim.outcome()
	assert.Equal(t, "/dest", dest)
	assert.True(t, success)
}

func TestAcquireDownloadReservationOwnerClaimBranches(t *testing.T) {
	t.Run("completed owner with same destination skips", func(t *testing.T) {
		dedup := &sync.Map{}
		claim := &downloadOwnerClaim{logicalKey: "movie", ownerKey: "first", destPath: "/dest", success: true, done: make(chan struct{})}
		dedup.Store(ownerClaimKey("movie"), claim)
		close(claim.done)

		reservation, skipped, err := acquireDownloadReservation(context.Background(), dedup, "/dest", "movie", "second")
		require.NoError(t, err)
		assert.True(t, skipped)
		assert.Nil(t, reservation)
	})

	t.Run("waiting on another owner honors cancellation", func(t *testing.T) {
		dedup := &sync.Map{}
		claim := &downloadOwnerClaim{logicalKey: "movie", ownerKey: "first", done: make(chan struct{})}
		dedup.Store(ownerClaimKey("movie"), claim)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		reservation, skipped, err := acquireDownloadReservation(ctx, dedup, "/dest", "movie", "second")
		assert.ErrorIs(t, err, context.Canceled)
		assert.False(t, skipped)
		assert.Nil(t, reservation)
		claim.complete(false)
	})

	t.Run("already-bound claim yields a fresh destination reservation", func(t *testing.T) {
		dedup := &sync.Map{}
		claim := &downloadOwnerClaim{logicalKey: "movie", ownerKey: "same", destPath: "/other", done: make(chan struct{})}
		dedup.Store(ownerClaimKey("movie"), claim)

		reservation, skipped, err := acquireDownloadReservation(context.Background(), dedup, "/dest", "movie", "same")
		require.NoError(t, err)
		assert.False(t, skipped)
		require.NotNil(t, reservation)
		finishDownloadReservation(dedup, "/dest", reservation, false)
		dedup.Delete(ownerClaimKey("movie"))
	})

	t.Run("foreign reservation value is treated as skipped", func(t *testing.T) {
		dedup := &sync.Map{}
		dedup.Store("/dest", "foreign")
		reservation, skipped, err := acquireDownloadReservation(context.Background(), dedup, "/dest")
		require.NoError(t, err)
		assert.True(t, skipped)
		assert.Nil(t, reservation)
	})
}

func TestAcquireDownloadReservationRetriesAfterFailedOwnerClaim(t *testing.T) {
	dedup := &sync.Map{}
	claim := &downloadOwnerClaim{logicalKey: "movie", ownerKey: "first", done: make(chan struct{}, 1)}
	claim.done <- struct{}{}
	dedup.Store(ownerClaimKey("movie"), claim)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, skipped, err := acquireDownloadReservation(ctx, dedup, "/dest", "movie", "second")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, skipped)
}

func TestFinishDownloadReservationRemovesCompletedOwnerClaim(t *testing.T) {
	dedup := &sync.Map{}
	claim := &downloadOwnerClaim{logicalKey: "movie", ownerKey: "owner", done: make(chan struct{})}
	reservation := &downloadReservation{done: make(chan struct{}), claim: claim}
	dest := "/dest"
	dedup.Store(dest, reservation)
	dedup.Store(ownerClaimKey("movie"), claim)

	finishDownloadReservation(dedup, dest, reservation, true)

	select {
	case <-reservation.done:
	default:
		t.Fatal("reservation completion was not published")
	}
	select {
	case <-claim.done:
	default:
		t.Fatal("owner completion was not published")
	}
	_, ok := dedup.Load(ownerClaimKey("movie"))
	assert.False(t, ok)
}
