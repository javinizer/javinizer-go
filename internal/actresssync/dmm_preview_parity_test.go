package actresssync_test

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/stretchr/testify/require"
)

func TestPreviewNumericIdentityKeepsPrecedenceAcrossClaimVariants(t *testing.T) {
	fresh, stale, leading := record(), record(), record()
	stale.SourceID = "second-claim"
	stale.Provenance.ObservedAt = now.Add(-24 * time.Hour)
	leading.SourceID = "leading-zero"
	leading.ExternalIDs[0].Value = "08"
	snapshot := build(t, fresh, stale, leading)

	exact, other, negative := target(), target(), target()
	other.ID, other.DMMID = 43, 8
	negative.ID, negative.DMMID = 44, -7
	plan, err := actresssync.Preview(t.Context(), []actresssync.Target{exact, other, negative}, snapshot, options())
	require.NoError(t, err)
	require.Equal(t, snapshot.Digest(), plan.SnapshotDigest)
	require.Len(t, plan.Items, 3)
	require.Equal(t, actresssync.Ambiguous, plan.Items[0].Outcome)
	require.Equal(t, actresssync.Unmatched, plan.Items[1].Outcome)
	require.Equal(t, actresssync.MissingIdentity, plan.Items[2].Outcome)
	for _, item := range plan.Items {
		require.Nil(t, item.Evidence)
	}

	plan, err = actresssync.Preview(t.Context(), []actresssync.Target{negative, exact}, nil, options())
	require.NoError(t, err)
	require.Equal(t, actresssync.MissingIdentity, plan.Items[0].Outcome)
	require.Equal(t, actresssync.NoSnapshot, plan.Items[1].Outcome)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	plan, err = actresssync.Preview(ctx, []actresssync.Target{exact}, snapshot, options())
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, plan)
}
