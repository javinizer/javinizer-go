package scrapeevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actressmetadata"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func record(id, name string, observed time.Time) actresscache.Evidence {
	return actresscache.Evidence{
		Source: r18Source, SourceID: id,
		Provenance:  actresscache.Provenance{SourceURL: "https://fixtures.invalid/dump?secret=hidden", DatasetSHA256: strings.Repeat("a", 64), ParserRevision: "synthetic-v1", ObservedAt: observed},
		Metadata:    actresscache.Metadata{JapaneseName: name, Aliases: []string{"Whole Alias"}, FirstName: "Never split", LastName: "Never merge", ThumbnailURL: "https://fixtures.invalid/image"},
		ExternalIDs: []actresscache.ExternalID{{Namespace: "dmm", Value: "7"}},
	}
}

// Install a synthetic portable artifact through the actual reader format. The
// publisher's separately verified platform contract is not bypassed in runtime.
func install(t *testing.T, directory, slot string, records []actresscache.Evidence) string {
	t.Helper()
	snapshot, err := actresscache.Build(t.Context(), records, offline.DefaultLimits().Store.Artifact)
	require.NoError(t, err)
	manifest := artifactstore.Manifest{
		Version: 1, Scope: "supplied-file-only", ArtifactSHA256: snapshot.Digest(), ArtifactBytes: int64(len(snapshot.Bytes())),
		Source: r18dump.Receipt{SHA256: strings.Repeat("a", 64), Bytes: 1, Rows: len(records), ActressRows: len(records), EmptyActressBlock: len(records) == 0},
	}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, snapshot.Digest()+".ndjson"), snapshot.Bytes(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, slot), append(encoded, '\n'), 0o600))
	return snapshot.Digest()
}

func service(t *testing.T, directory string) *Service {
	t.Helper()
	s, err := New(Options{Enabled: true, ArtifactDirectory: directory, MaxAge: 24 * time.Hour})
	require.NoError(t, err)
	s.now = func() time.Time { return testNow }
	return s
}

func raw(ids ...string) *models.ScraperResult {
	r := &models.ScraperResult{Source: r18Source, Actresses: []models.ActressInfo{{DMMID: 7, JapaneseName: "Canonical original"}}}
	r.SetR18DumpActorReferences(ids)
	return r
}

func resolve(t *testing.T, s *Service, input []*models.ScraperResult, cached bool) *Report {
	t.Helper()
	ctx, release := s.Begin(t.Context(), true)
	defer release()
	return Resolve(ctx, input, cached)
}

func TestDefaultOffNilServiceConfigAndNoIO(t *testing.T) {
	s, err := New(Options{})
	require.NoError(t, err)
	s.load = func(context.Context, string) (artifactstore.View, error) { panic("must not load") }
	ctx, release := s.Begin(t.Context(), false)
	defer release()
	require.Same(t, t.Context(), ctx)
	require.Nil(t, Resolve(ctx, []*models.ScraperResult{raw("7")}, false))
	require.Nil(t, Resolve(nil, nil, false))
	require.Equal(t, Disabled, resolve(t, s, nil, false).Outcome)
	require.Equal(t, Unavailable, resolve(t, nil, nil, false).Outcome)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx, release = (*Service)(nil).Begin(ctx, true)
	defer release()
	require.Equal(t, Cancelled, Resolve(ctx, nil, false).Outcome)
	ctx, release = (*Service)(nil).Begin(nil, true)
	defer release()
	require.Equal(t, Unavailable, Resolve(ctx, nil, false).Outcome)
	missing := filepath.Join(t.TempDir(), "not-created")
	s = service(t, missing)
	_, err = os.Stat(missing)
	require.True(t, os.IsNotExist(err), "New must not access/create the directory")
	require.Equal(t, Unavailable, resolve(t, s, nil, false).Outcome)
	_, err = os.Stat(missing)
	require.True(t, os.IsNotExist(err))
	for _, opts := range []Options{{Enabled: true}, {MaxAge: time.Minute}, {MaxAge: 8761 * time.Hour}, {Enabled: true, MaxAge: time.Hour, ArtifactDirectory: "relative"}, {Enabled: true, MaxAge: time.Hour, ArtifactDirectory: t.TempDir() + "\x00"}} {
		_, err := New(opts)
		require.Error(t, err)
	}
}

func TestExactSourceReferencesOwnershipAndNoCanonicalMutation(t *testing.T) {
	dir := t.TempDir()
	digest := install(t, dir, "current.json", []actresscache.Evidence{record("7", "Seven", testNow), record("007", "Zero Seven", testNow)})
	s := service(t, dir)
	input := raw("7", "007", "missing", "7")
	before := input.Clone()
	report := resolve(t, s, []*models.ScraperResult{input}, false)
	require.Equal(t, Available, report.Outcome)
	require.Equal(t, digest, report.SnapshotDigest)
	require.Len(t, report.Observations, 3)
	require.Equal(t, "Seven", report.Observations[0].JapaneseName)
	require.Equal(t, "Zero Seven", report.Observations[1].JapaneseName)
	require.Equal(t, Unmatched, report.Observations[2].Outcome)
	require.Equal(t, "https://fixtures.invalid/dump", report.Observations[0].SourceURL)
	require.Equal(t, before, input)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	for _, forbidden := range []string{"thumbnail", "image", "external", "first_name", "last_name", "secret", "dmm"} {
		require.NotContains(t, string(encoded), forbidden)
	}
	clone := report.Clone()
	clone.Observations[0].Aliases[0] = "mutated"
	*clone.Observations[0].ObservedAt = testNow.Add(time.Hour)
	require.Equal(t, "Whole Alias", report.Observations[0].Aliases[0])
	require.Equal(t, testNow, *report.Observations[0].ObservedAt)
	require.Nil(t, (*Report)(nil).Clone())
	report.Observations[0].Aliases[0] = "also mutated"
	require.Equal(t, "Whole Alias", resolve(t, s, []*models.ScraperResult{input}, false).Observations[0].Aliases[0])
}

func TestNoNumericNameJSONCacheOrCrossSourceAuthority(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "current.json", []actresscache.Evidence{record("7", "Supplemental", testNow)})
	s := service(t, dir)
	for _, input := range []*models.ScraperResult{
		{Source: r18Source, Actresses: []models.ActressInfo{{DMMID: 7, JapaneseName: "Supplemental"}}},
		func() *models.ScraperResult {
			r := raw("7")
			require.NoError(t, json.Unmarshal([]byte(`{"source":"r18dev","r18_dump_actor_references":["7"]}`), r))
			return r
		}(),
	} {
		report := resolve(t, s, []*models.ScraperResult{input}, false)
		require.Equal(t, UnsupportedIdentity, report.Outcome)
		require.Empty(t, report.Observations[0].SourceID)
		require.Empty(t, report.Observations[0].JapaneseName)
	}
	wrong := raw("7")
	wrong.Source = "dmm"
	for _, input := range [][]*models.ScraperResult{nil, {nil, wrong}, {raw([]string{}...)}} {
		require.Equal(t, NoSourceReference, resolve(t, s, input, false).Outcome)
	}
	require.Equal(t, NoSourceReference, resolve(t, s, []*models.ScraperResult{raw("7")}, true).Outcome)
	require.Equal(t, UnsupportedIdentity, resolve(t, s, []*models.ScraperResult{raw("7"), {Source: r18Source}}, false).Outcome)
}

func TestGenerationAdmissionFreshnessAndRecoveryArePinned(t *testing.T) {
	dir := t.TempDir()
	a := install(t, dir, "current.json", []actresscache.Evidence{record("7", "A", testNow.Add(-time.Hour))})
	s := service(t, dir)
	ctxA, releaseA := s.BeginBatch(t.Context(), true, MaxReports)
	defer releaseA()
	b := install(t, dir, "current.json", []actresscache.Evidence{record("7", "B", testNow)})
	require.NotEqual(t, a, b)
	s.now = func() time.Time { return testNow.Add(48 * time.Hour) }
	results := make(chan *Report, MaxReports)
	var wg sync.WaitGroup
	for range MaxReports {
		wg.Go(func() { results <- Resolve(ctxA, []*models.ScraperResult{raw("7")}, false) })
	}
	wg.Wait()
	close(results)
	for report := range results {
		require.Equal(t, Available, report.Outcome, "parallel files do not compete for a registry TryLock")
		require.Equal(t, a, report.SnapshotDigest)
		require.Equal(t, "A", report.Observations[0].JapaneseName)
	}
	report := resolve(t, s, []*models.ScraperResult{raw("7")}, false)
	require.Equal(t, b, report.SnapshotDigest)
	require.Equal(t, Stale, report.Outcome)
	require.Empty(t, report.Observations[0].JapaneseName)
	s.now = func() time.Time { return testNow.Add(-time.Minute) }
	require.Equal(t, Future, resolve(t, s, []*models.ScraperResult{raw("7")}, false).Outcome)
	install(t, dir, "previous.json", []actresscache.Evidence{record("7", "Recovered", testNow)})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "current.json"), []byte("bad"), 0o600))
	require.True(t, resolve(t, s, nil, false).Recovered)
	// Restart/new service adopts the committed artifact; stored A reports are
	// immutable values and have no automatic recomputation hook.
	s = service(t, dir)
	require.Equal(t, "Recovered", resolve(t, s, []*models.ScraperResult{raw("7")}, false).Observations[0].JapaneseName)
}

func TestAdmissionCountsCancellationCarryAndPanicRelease(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "current.json", []actresscache.Evidence{record("7", "Name", testNow)})
	s := service(t, dir)
	var releases []func()
	for range MaxOperations {
		ctx, release := s.Begin(t.Context(), true)
		releases = append(releases, release)
		require.NotEqual(t, Busy, Resolve(ctx, nil, false).Outcome)
	}
	require.Equal(t, Busy, resolve(t, s, nil, false).Outcome)
	for _, release := range releases {
		release()
		release()
	}
	require.Empty(t, s.slots)
	ctx, release := s.Begin(t.Context(), true)
	cancelCtx, cancel := context.WithCancel(t.Context())
	carried := Carry(cancelCtx, ctx)
	cancel()
	require.Equal(t, Cancelled, Resolve(carried, nil, false).Outcome)
	require.Nil(t, Resolve(Carry(ctx, nil), nil, false))
	require.Nil(t, Resolve(Carry(nil, nil), nil, false))
	off, offRelease := s.Begin(ctx, false)
	defer offRelease()
	require.Nil(t, Resolve(off, nil, false))
	release()
	require.Equal(t, Cancelled, Resolve(ctx, nil, false).Outcome)
	require.Nil(t, ctx.Value(sessionKey{}).(*session).provider)
	load := s.load
	s.load = func(context.Context, string) (artifactstore.View, error) { panic("synthetic load panic") }
	for _, count := range []int{-1, 0, MaxReports + 1} {
		ctx, release := s.BeginBatch(t.Context(), true, count)
		require.Equal(t, BudgetExceeded, Resolve(ctx, nil, false).Outcome)
		release()
	}
	require.Panics(t, func() { _, _ = s.Begin(t.Context(), true) })
	require.Empty(t, s.slots)
	s.load = load
	ctx, release = s.Begin(t.Context(), true)
	defer release()
	require.Equal(t, Available, Resolve(ctx, []*models.ScraperResult{raw("7")}, false).Outcome)
	require.Equal(t, BudgetExceeded, Resolve(ctx, nil, false).Outcome)
}

func TestCancellationDuringAdmissionReleasesBeforeReturning(t *testing.T) {
	s := service(t, t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	s.load = func(context.Context, string) (artifactstore.View, error) {
		cancel()
		return artifactstore.View{}, errors.New("synthetic cancellation during load")
	}
	ctx, release := s.Begin(ctx, true)
	defer release()
	require.Equal(t, Cancelled, Resolve(ctx, nil, false).Outcome)
	require.Empty(t, s.slots)
}

type cancelAtBoundary struct {
	context.Context
	cancel context.CancelFunc
	at     int
	calls  int
}

func (c *cancelAtBoundary) Err() error {
	c.calls++
	if c.calls == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCancellationAtCollectionAndEncodingFences(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "current.json", []actresscache.Evidence{record("7", "Name", testNow)})
	s := service(t, dir)
	for _, at := range []int{2, 4, 5} {
		op, release := s.Begin(t.Context(), true)
		state := op.Value(sessionKey{}).(*session)
		state.provider = providerFunc(func(context.Context, actressmetadata.Query) (actressmetadata.Result, error) {
			return actressmetadata.Result{Outcome: actressmetadata.Unmatched, SnapshotDigest: state.base.SnapshotDigest}, nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		fence := &cancelAtBoundary{Context: ctx, cancel: cancel, at: at}
		report := Resolve(Carry(fence, op), []*models.ScraperResult{raw("7")}, false)
		cancel()
		release()
		require.Equal(t, Cancelled, report.Outcome)
		require.Empty(t, report.Observations)
	}
}

type providerFunc func(context.Context, actressmetadata.Query) (actressmetadata.Result, error)

func (fn providerFunc) LookupEvidence(ctx context.Context, query actressmetadata.Query) (actressmetadata.Result, error) {
	return fn(ctx, query)
}

func TestProviderFailureIdentityMismatchAmbiguityAndCancellationDiscardPartial(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "current.json", []actresscache.Evidence{record("7", "Name", testNow)})
	s := service(t, dir)
	for _, mode := range []string{"error", "digest", "source", "id", "missing-record", "missing-provenance", "nonavailable-record", "invalid-outcome", "ambiguous", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, release := s.Begin(ctx, true)
			defer release()
			state := ctx.Value(sessionKey{}).(*session)
			state.provider = providerFunc(func(ctx context.Context, q actressmetadata.Query) (actressmetadata.Result, error) {
				record := record(q.Reference.ID, "Name", testNow)
				out := actressmetadata.Result{Outcome: actressmetadata.Available, SnapshotDigest: state.base.SnapshotDigest, Evidence: &record}
				if q.Reference.ID == "7" {
					return out, nil
				}
				switch mode {
				case "error":
					return out, errors.New("provider failed")
				case "digest":
					out.SnapshotDigest = "other"
				case "source":
					out.Evidence.Source = "dmm"
				case "id":
					out.Evidence.SourceID = "7"
				case "missing-record":
					out.Evidence = nil
				case "missing-provenance":
					out.Evidence.Provenance = actresscache.Provenance{}
				case "nonavailable-record":
					out.Outcome = actressmetadata.Unmatched
				case "invalid-outcome":
					out.Outcome, out.Evidence = "invalid", nil
				case "ambiguous":
					out.Outcome, out.Evidence = actressmetadata.Ambiguous, nil
				case "cancelled":
					cancel()
				}
				return out, nil
			})
			report := Resolve(ctx, []*models.ScraperResult{raw("7", "007")}, false)
			switch mode {
			case "ambiguous":
				require.Equal(t, Ambiguous, report.Outcome)
				require.Len(t, report.Observations, 2)
			case "cancelled":
				require.Equal(t, Cancelled, report.Outcome)
				require.Empty(t, report.Observations)
			default:
				require.Equal(t, ProviderFailed, report.Outcome)
				require.Empty(t, report.Observations, "no partial success on provider failure")
			}
		})
	}
}

func TestReferenceAndEncodedBudgets(t *testing.T) {
	dir := t.TempDir()
	var records []actresscache.Evidence
	var ids []string
	for i := range 8 {
		id := fmt.Sprint(i)
		entry := record(id, "Name", testNow)
		entry.Metadata.Aliases = []string{strings.Repeat("a", 4000), strings.Repeat("b", 4000), strings.Repeat("c", 4000)}
		records = append(records, entry)
		ids = append(ids, id)
	}
	install(t, dir, "current.json", records)
	s := service(t, dir)
	for _, input := range [][]*models.ScraperResult{{raw(ids...)}, {raw(make([]string, 65)...)}, make([]*models.ScraperResult, 65)} {
		report := resolve(t, s, input, false)
		require.Equal(t, BudgetExceeded, report.Outcome)
		require.Empty(t, report.Observations)
	}
	report := resolve(t, s, []*models.ScraperResult{raw("bad\x00ref")}, false)
	require.Equal(t, UnsupportedIdentity, report.Outcome)
	require.Empty(t, report.Observations)
}

func TestReleaseJoinsActiveResolutionAndSanitizesPresentation(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "current.json", []actresscache.Evidence{record("7", "Name", testNow)})
	s := service(t, dir)
	ctx, release := s.Begin(t.Context(), true)
	state := ctx.Value(sessionKey{}).(*session)
	started, unblock := make(chan struct{}), make(chan struct{})
	state.provider = providerFunc(func(context.Context, actressmetadata.Query) (actressmetadata.Result, error) {
		close(started)
		<-unblock
		return actressmetadata.Result{Outcome: actressmetadata.Unmatched, SnapshotDigest: state.base.SnapshotDigest}, nil
	})
	result := make(chan *Report, 1)
	go func() { result <- Resolve(ctx, []*models.ScraperResult{raw("7")}, false) }()
	<-started
	require.False(t, state.mu.TryLock(), "active reads retain the provider until they finish")
	closed := make(chan struct{})
	go func() { release(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("release returned before active lookup joined")
	default:
	}
	close(unblock)
	require.Equal(t, Unmatched, (<-result).Outcome)
	<-closed
	require.Empty(t, s.slots)
	require.Nil(t, state.provider)
	for raw, want := range map[string]string{"https://user:pass@fixtures.invalid/path?token=hidden#fragment": "https://fixtures.invalid/path", "http://fixtures.invalid?": "http://fixtures.invalid", "file:///secret": "", "not a URL": "", "https://[": ""} {
		require.Equal(t, want, presentationURL(raw))
	}
}
