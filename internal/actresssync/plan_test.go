package actresssync_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)

func budgets() actresscache.Limits {
	return actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
}
func options() actresssync.Options {
	return actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 20}
}
func record() actresscache.Evidence {
	return actresscache.Evidence{Source: "fixture", SourceID: "source-7", Provenance: actresscache.Provenance{SourceURL: "https://example.invalid/actors/7", DatasetSHA256: strings.Repeat("a", 64), ParserRevision: "fixture-v1", ObservedAt: now}, Metadata: actresscache.Metadata{JapaneseName: "Claim", Aliases: []string{"Untrusted alias"}, ThumbnailURL: "http://127.0.0.1/not-fetch-permission"}, ExternalIDs: []actresscache.ExternalID{{Namespace: "dmm", Value: "7"}}}
}
func build(t *testing.T, records ...actresscache.Evidence) *actresscache.Snapshot {
	t.Helper()
	s, e := actresscache.Build(t.Context(), records, budgets())
	require.NoError(t, e)
	return s
}
func target() actresssync.Target {
	return actresssync.Target{ID: 42, Exists: true, DMMID: 7, Verified: true, UpdatedAt: now}
}

func TestPreviewOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*actresssync.Target, *actresscache.Evidence)
		outcome actresssync.Outcome
	}{
		{"available", func(*actresssync.Target, *actresscache.Evidence) {}, actresssync.EvidenceAvailable},
		{"missing", func(a *actresssync.Target, _ *actresscache.Evidence) { a.Exists = false }, actresssync.Missing},
		{"quarantined", func(a *actresssync.Target, _ *actresscache.Evidence) { a.Quarantined = true; a.Verified = false }, actresssync.Quarantined},
		{"candidate", func(a *actresssync.Target, _ *actresscache.Evidence) { a.Verified = false }, actresssync.Unverified},
		{"manual", func(a *actresssync.Target, _ *actresscache.Evidence) { a.Protected = true }, actresssync.Protected},
		{"missing ID", func(a *actresssync.Target, _ *actresscache.Evidence) { a.DMMID = 0 }, actresssync.MissingIdentity},
		{"negative ID", func(a *actresssync.Target, _ *actresscache.Evidence) { a.DMMID = -7 }, actresssync.MissingIdentity},
		{"other ID", func(_ *actresssync.Target, e *actresscache.Evidence) { e.ExternalIDs[0].Value = "8" }, actresssync.Unmatched},
		{"leading zero", func(_ *actresssync.Target, e *actresscache.Evidence) { e.ExternalIDs[0].Value = "07" }, actresssync.Unmatched},
		{"namespace exact", func(_ *actresssync.Target, e *actresscache.Evidence) { e.ExternalIDs[0].Namespace = "r18dump" }, actresssync.Unmatched},
		{"source numeric coincidence", func(_ *actresssync.Target, e *actresscache.Evidence) { e.SourceID = "7"; e.ExternalIDs = nil }, actresssync.Unmatched},
		{"stale", func(_ *actresssync.Target, e *actresscache.Evidence) {
			e.Provenance.ObservedAt = now.Add(-time.Hour - time.Nanosecond)
		}, actresssync.Stale},
		{"fresh boundary", func(_ *actresssync.Target, e *actresscache.Evidence) { e.Provenance.ObservedAt = now.Add(-time.Hour) }, actresssync.EvidenceAvailable},
		{"future", func(_ *actresssync.Target, e *actresscache.Evidence) {
			e.Provenance.ObservedAt = now.Add(time.Nanosecond)
		}, actresssync.FutureObservation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, e := target(), record()
			tc.change(&a, &e)
			s := build(t, e)
			p, err := actresssync.Preview(t.Context(), []actresssync.Target{a}, s, options())
			require.NoError(t, err)
			require.Equal(t, s.Digest(), p.SnapshotDigest)
			require.Equal(t, now, p.EvaluatedAt)
			require.Equal(t, tc.outcome, p.Items[0].Outcome)
			require.Equal(t, a, p.Items[0].Target)
			if tc.outcome != actresssync.EvidenceAvailable {
				require.Nil(t, p.Items[0].Evidence)
			} else {
				require.Equal(t, e, *p.Items[0].Evidence)
			}
		})
	}
}

func TestPreviewNoSnapshotAndAmbiguityBeforeFreshness(t *testing.T) {
	p, err := actresssync.Preview(t.Context(), []actresssync.Target{target()}, nil, options())
	require.NoError(t, err)
	require.Equal(t, actresssync.NoSnapshot, p.Items[0].Outcome)
	require.Empty(t, p.SnapshotDigest)
	a, b := record(), record()
	b.Source = "other"
	b.Provenance.ObservedAt = now.Add(-24 * time.Hour)
	p, err = actresssync.Preview(t.Context(), []actresssync.Target{target()}, build(t, a, b), options())
	require.NoError(t, err)
	require.Equal(t, actresssync.Ambiguous, p.Items[0].Outcome)
	require.Nil(t, p.Items[0].Evidence)
	p, err = actresssync.Preview(t.Context(), []actresssync.Target{target()}, build(t), options())
	require.NoError(t, err)
	require.Equal(t, actresssync.Unmatched, p.Items[0].Outcome)
}
func TestPreviewAdmissionAndCancellation(t *testing.T) {
	for _, ids := range [][]uint{nil, {0}, {1, 1}, make([]uint, 501)} {
		require.Error(t, options().ValidateSelection(ids))
	}
	for _, o := range []actresssync.Options{{}, {Now: now, MaxAge: -1, MaxActors: 1}, {Now: now, MaxAge: 1, MaxActors: 501}, {Now: now, MaxAge: 1, MaxActors: 1}} {
		require.Error(t, o.ValidateSelection([]uint{1, 2}))
	}
	for _, targets := range [][]actresssync.Target{nil, make([]actresssync.Target, 501), {{ID: 0}}, {{ID: 1}, {ID: 1}}} {
		p, err := actresssync.Preview(t.Context(), targets, nil, options())
		require.Error(t, err)
		require.Nil(t, p)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, err := actresssync.Preview(ctx, []actresssync.Target{target()}, nil, options())
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, p)
}
func TestPinnedSnapshotAndOwnedResultsAcrossRefresh(t *testing.T) {
	first, second := record(), record()
	second.Metadata.JapaneseName = "New generation"
	s1, s2 := build(t, first), build(t, second)
	var store actresscache.Store
	pinned, err := store.Replace(t.Context(), bytes.NewReader(s1.Bytes()), budgets())
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			_, e := store.Replace(t.Context(), bytes.NewReader(s2.Bytes()), budgets())
			if e != nil {
				t.Error(e)
			}
		}
	}()
	for i := 0; i < 30; i++ {
		targets := []actresssync.Target{target(), target()}
		targets[1].ID = 99
		p, e := actresssync.Preview(t.Context(), targets, pinned, options())
		require.NoError(t, e)
		require.Equal(t, s1.Digest(), p.SnapshotDigest)
		require.Equal(t, uint(99), p.Items[1].Target.ID)
		p.Items[0].Evidence.Metadata.Aliases[0] = "mutated"
		p.Items[0].Evidence.ExternalIDs[0].Value = "changed"
		require.Equal(t, first, *p.Items[1].Evidence)
	}
	wg.Wait()
	require.Equal(t, s2.Digest(), store.Current().Digest())
}

// Cancel at deterministic cooperation boundaries rather than depending on sleeps.
type checkpointContext struct {
	context.Context
	cancel context.CancelFunc
	calls  int
	at     int
}

func (c *checkpointContext) Err() error {
	c.calls++
	if c.calls == c.at {
		c.cancel()
	}
	return c.Context.Err()
}
func TestPreviewCancellationDiscardsPartialResult(t *testing.T) {
	for _, at := range []int{2, 3, 4} {
		ctx, cancel := context.WithCancel(t.Context())
		checkpoint := &checkpointContext{Context: ctx, cancel: cancel, at: at}
		targets := []actresssync.Target{target(), {ID: 99}}
		p, err := actresssync.Preview(checkpoint, targets, build(t, record()), options())
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, p)
		cancel()
	}
}
func TestPreviewFreshnessDoesNotSaturateDuration(t *testing.T) {
	e := record()
	e.Provenance.ObservedAt = time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)
	o := options()
	o.MaxAge = time.Duration(1<<63 - 1)
	p, err := actresssync.Preview(t.Context(), []actresssync.Target{target()}, build(t, e), o)
	require.NoError(t, err)
	require.Equal(t, actresssync.Stale, p.Items[0].Outcome)
}

func TestSelectionExactBudgets(t *testing.T) {
	ids := make([]uint, actresssync.MaxSelection)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	o := options()
	o.MaxActors = actresssync.MaxSelection
	require.NoError(t, o.ValidateSelection(ids))
	require.Error(t, o.ValidateSelection(append(ids, 501)))
	o.MaxActors = 1
	require.NoError(t, o.ValidateSelection([]uint{1}))
	require.Error(t, o.ValidateSelection([]uint{1, 2}))
}
