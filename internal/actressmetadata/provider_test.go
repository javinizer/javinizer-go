package actressmetadata

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func limits() actresscache.Limits {
	return actresscache.Limits{MaxBytes: 100000, MaxRecordBytes: 10000, MaxRecords: 100, MaxStringBytes: 1000, MaxTotalStringBytes: 100000, MaxAliases: 10, MaxExternalIDs: 10, MaxTokensPerRecord: 200}
}
func fixture(t *testing.T) *actresscache.Snapshot {
	t.Helper()
	data, err := os.ReadFile("../actresscache/testdata/evidence-v1.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	s, err := actresscache.Decode(context.Background(), bytes.NewReader(data), limits())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func provider(t *testing.T, s *actresscache.Snapshot, source string) *SnapshotProvider {
	t.Helper()
	p, err := NewSnapshotProvider(s, source)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func claim(value string) Query {
	return Query{Claim: &actresscache.ExternalID{Namespace: "dmm", Value: value}}
}
func ref(source, id string) Query { return Query{Reference: &Reference{Source: source, ID: id}} }

type fakeClock struct {
	now   time.Time
	waits []time.Duration
	wait  func(context.Context) error
}

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) Wait(ctx context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	if f.wait != nil {
		return f.wait(ctx)
	}
	f.now = f.now.Add(d)
	return nil
}

type stub struct {
	calls int
	fn    func(context.Context, Query) (Result, error)
}

func (s *stub) LookupEvidence(ctx context.Context, q Query) (Result, error) {
	s.calls++
	if s.fn != nil {
		return s.fn(ctx, q)
	}
	return Result{Outcome: Unmatched, SnapshotDigest: strings.Repeat("a", 64)}, nil
}
func registry(t *testing.T, entries ...Registration) *Registry {
	t.Helper()
	r, err := NewRegistry(entries, &fakeClock{now: time.Unix(1, 0)}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestPinnedSnapshotAndCopies(t *testing.T) {
	ctx := context.Background()
	old := fixture(t)
	var store actresscache.Store
	if _, err := store.Replace(ctx, bytes.NewReader(old.Bytes()), limits()); err != nil {
		t.Fatal(err)
	}
	p := provider(t, store.Current(), "fixture_a")
	empty, err := actresscache.Build(ctx, nil, limits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Replace(ctx, bytes.NewReader(empty.Bytes()), limits()); err != nil {
		t.Fatal(err)
	}
	result, err := p.LookupEvidence(ctx, ref("fixture_a", "actor-1"))
	if err != nil || result.Outcome != Available || result.SnapshotDigest != old.Digest() {
		t.Fatalf("%+v %v", result, err)
	}
	result.Evidence.Metadata.Aliases[0] = "changed"
	again, _ := p.LookupEvidence(ctx, ref("fixture_a", "actor-1"))
	if again.Evidence.Metadata.Aliases[0] == "changed" {
		t.Fatal("mutable snapshot")
	}
	refreshed, _ := provider(t, store.Current(), "fixture_a").LookupEvidence(ctx, ref("fixture_a", "actor-1"))
	if refreshed.Outcome != Unmatched || refreshed.SnapshotDigest == old.Digest() {
		t.Fatal(refreshed)
	}
	missing, _ := provider(t, nil, "fixture_a").LookupEvidence(ctx, claim("7"))
	if missing.Outcome != NoSnapshot || missing.SnapshotDigest != "" {
		t.Fatal(missing)
	}
}
func TestSourcePriorityNeverRemovesAmbiguity(t *testing.T) {
	s := fixture(t)
	a := provider(t, s, "fixture_a")
	b := provider(t, s, "fixture_b")
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}, {"a"}} {
		r := registry(t, Registration{Name: "a", Provider: a, Enabled: true}, Registration{Name: "b", Provider: b, Enabled: true})
		result, err := r.Resolve(context.Background(), claim("7"), order)
		if err != nil || result.Outcome != Ambiguous {
			t.Fatalf("%+v %v", result, err)
		}
		for i, o := range result.Observations {
			if o.Provider != order[i] || o.Result.Evidence != nil {
				t.Fatal(result)
			}
		}
	}
	r := registry(t, Registration{Name: "a", Provider: a, Enabled: true}, Registration{Name: "b", Provider: b, Enabled: false})
	result, err := r.Resolve(context.Background(), claim("7"), []string{"a", "b"})
	if err != nil || result.Outcome != Ambiguous {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestDigitsAndNamesAreNotAuthority(t *testing.T) {
	records := fixture(t).Records()[:1]
	records[0].Source = "r18dev"
	records[0].SourceID = "7"
	records[0].ExternalIDs = nil
	s, err := actresscache.Build(context.Background(), records, limits())
	if err != nil {
		t.Fatal(err)
	}
	p := provider(t, s, "r18dev")
	for _, q := range []Query{claim("7"), ref("dmm", "7"), ref("r18dev", records[0].Metadata.JapaneseName)} {
		result, err := p.LookupEvidence(context.Background(), q)
		if err != nil || result.Outcome != Unmatched {
			t.Fatalf("%+v %v", result, err)
		}
	}
	exact, _ := p.LookupEvidence(context.Background(), ref("r18dev", "7"))
	if exact.Outcome != Available {
		t.Fatal(exact)
	}
	records[0].ExternalIDs = []actresscache.ExternalID{{Namespace: "dmm", Value: "07"}}
	s, err = actresscache.Build(context.Background(), records, limits())
	if err != nil {
		t.Fatal(err)
	}
	p = provider(t, s, "r18dev")
	for value, want := range map[string]Outcome{"7": Unmatched, "07": Available} {
		got, _ := p.LookupEvidence(context.Background(), claim(value))
		if got.Outcome != want {
			t.Fatal(got)
		}
	}
}
func TestSkipDisabledAndInvalidSelectionHaveZeroCalls(t *testing.T) {
	a := &stub{}
	b := &stub{}
	r := registry(t, Registration{Name: "a", Provider: a, Enabled: true}, Registration{Name: "b", Provider: b})
	for _, priorities := range [][]string{nil, {"b"}, {"a", Skip}, {Skip, "a"}} {
		got, err := r.Resolve(context.Background(), claim("7"), priorities)
		if err != nil || got.Outcome != Skipped {
			t.Fatalf("%+v %v", got, err)
		}
	}
	for _, priorities := range [][]string{{"a", "unknown"}, {"a", "a"}, make([]string, MaxProviders+1)} {
		if got, err := r.Resolve(context.Background(), claim("7"), priorities); err == nil || got != nil {
			t.Fatal("accepted invalid priorities")
		}
	}
	if a.calls != 0 || b.calls != 0 {
		t.Fatal("unexpected provider call")
	}
}
func TestThrottleCancellationAndZero(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1, 0)}
	s := &stub{}
	r, err := NewRegistry([]Registration{{Name: "a", Provider: s, Enabled: true, MinInterval: time.Second}}, clock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = r.Resolve(context.Background(), claim("7"), []string{"a"}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{time.Second}) {
		t.Fatal(clock.waits)
	}
	clock.wait = func(context.Context) error { return context.Canceled }
	got, err := r.Resolve(context.Background(), claim("7"), []string{"a"})
	if !errors.Is(err, context.Canceled) || got != nil || s.calls != 2 {
		t.Fatalf("%+v %v calls %d", got, err, s.calls)
	}
	zero := registry(t, Registration{Name: "a", Provider: s, Enabled: true})
	for i := 0; i < 2; i++ {
		if _, err := zero.Resolve(context.Background(), claim("7"), []string{"a"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(zero.clock.(*fakeClock).waits) != 0 {
		t.Fatal("zero interval defaulted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := zero.Resolve(ctx, claim("7"), []string{"a"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestBoundedAdmissionDeadlineAndNoPartialResults(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s := &stub{fn: func(ctx context.Context, q Query) (Result, error) {
		close(entered)
		select {
		case <-release:
			return Result{Outcome: Unmatched, SnapshotDigest: strings.Repeat("a", 64)}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}}
	r := registry(t, Registration{Name: "a", Provider: s, Enabled: true})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := r.Resolve(context.Background(), claim("7"), []string{"a"}); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	if _, err := r.Resolve(context.Background(), claim("7"), []string{"a"}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	blocking := &stub{fn: func(ctx context.Context, q Query) (Result, error) { <-ctx.Done(); return Result{}, ctx.Err() }}
	timed, err := NewRegistry([]Registration{{Name: "a", Provider: &stub{}, Enabled: true}, {Name: "b", Provider: blocking, Enabled: true}}, SystemClock{}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := timed.Resolve(context.Background(), claim("7"), []string{"a", "b"}); !errors.Is(err, context.DeadlineExceeded) || result != nil {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SystemClock{}).Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestValidationAndNoMovieCapability(t *testing.T) {
	for _, q := range []Query{{}, {Reference: &Reference{Source: "a", ID: "7"}, Claim: &actresscache.ExternalID{Namespace: "dmm", Value: "7"}}, {Reference: &Reference{Source: "a", ID: ""}}, {Claim: &actresscache.ExternalID{Namespace: "DMM", Value: "7"}}} {
		if _, err := provider(t, nil, "a").LookupEvidence(context.Background(), q); err == nil {
			t.Fatal("invalid query")
		}
	}
	if _, err := NewSnapshotProvider(nil, "__skip__"); err == nil {
		t.Fatal("invalid source")
	}
	for _, entries := range [][]Registration{{{Name: "a"}}, {{Name: "a", Provider: &stub{}, MinInterval: -1}}, {{Name: "a", Provider: &stub{}}, {Name: "a", Provider: &stub{}}}, make([]Registration, MaxProviders+1)} {
		if _, err := NewRegistry(entries, SystemClock{}, time.Second); err == nil {
			t.Fatal("invalid registrations")
		}
	}
	if _, err := NewRegistry(nil, nil, time.Second); err == nil {
		t.Fatal("nil clock")
	}
	if _, err := NewRegistry(nil, SystemClock{}, 0); err == nil {
		t.Fatal("zero budget")
	}
	typ := reflect.TypeOf((*SnapshotProvider)(nil))
	for i := 0; i < typ.NumMethod(); i++ {
		if strings.Contains(strings.ToLower(typ.Method(i).Name), "scrape") {
			t.Fatal("movie capability leaked")
		}
	}
}
func TestMixedSnapshotGenerationsFailClosed(t *testing.T) {
	old := fixture(t)
	records := old.Records()
	records[0].Metadata.JapaneseName = "different generation"
	fresh, err := actresscache.Build(context.Background(), records, limits())
	if err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registration{Name: "old", Provider: provider(t, old, "fixture_a"), Enabled: true}, Registration{Name: "new", Provider: provider(t, fresh, "fixture_a"), Enabled: true})
	got, err := r.Resolve(context.Background(), ref("fixture_a", "actor-1"), []string{"old", "new"})
	if err == nil || got != nil {
		t.Fatalf("mixed generations accepted: %+v %v", got, err)
	}
}
func TestRegistryEvidenceAndProviderFailures(t *testing.T) {
	s := fixture(t)
	r := registry(t, Registration{Name: "a", Provider: provider(t, s, "fixture_a"), Enabled: true})
	got, err := r.Resolve(context.Background(), ref("fixture_a", "actor-1"), []string{"a"})
	if err != nil || got.Outcome != Available || got.Observations[0].Result.Evidence.Provenance.DatasetSHA256 == "" {
		t.Fatalf("%+v %v", got, err)
	}
	missing := registry(t, Registration{Name: "a", Provider: provider(t, nil, "fixture_a"), Enabled: true})
	got, err = missing.Resolve(context.Background(), claim("7"), []string{"a"})
	if err != nil || got.Outcome != NoSnapshot {
		t.Fatalf("%+v %v", got, err)
	}
	for _, result := range []Result{{Outcome: Available}, {Outcome: "invalid"}, {Outcome: Available, Evidence: &actresscache.Evidence{Source: "!", SourceID: "7"}}} {
		bad := registry(t, Registration{Name: "bad", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) { return result, nil }}})
		if got, err := bad.Resolve(context.Background(), claim("7"), []string{"bad"}); err == nil || got != nil {
			t.Fatalf("%+v %v", got, err)
		}
	}
	a, b := s.Records()[0], s.Records()[1]
	competing := registry(t, Registration{Name: "a", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, Evidence: &a, SnapshotDigest: s.Digest()}, nil
	}}}, Registration{Name: "b", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, Evidence: &b, SnapshotDigest: s.Digest()}, nil
	}}})
	got, err = competing.Resolve(context.Background(), claim("7"), []string{"a", "b"})
	if err != nil || got.Outcome != Ambiguous {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestMalformedProviderEvidenceFailsClosed(t *testing.T) {
	s := fixture(t)
	record := s.Records()[0]
	for _, tc := range []struct {
		name   string
		result Result
		query  Query
	}{
		{"bad digest", Result{Outcome: Unmatched, SnapshotDigest: "not a digest"}, claim("7")},
		{"missing with digest", Result{Outcome: NoSnapshot, SnapshotDigest: s.Digest()}, claim("7")},
		{"unmatched evidence", Result{Outcome: Unmatched, SnapshotDigest: s.Digest(), Evidence: &record}, claim("7")},
		{"available missing", Result{Outcome: Available, SnapshotDigest: s.Digest()}, claim("7")},
		{"invalid outcome", Result{Outcome: "invalid", SnapshotDigest: s.Digest()}, claim("7")},
		{"reference mismatch", Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, ref("fixture_a", "other")},
		{"claim mismatch", Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, claim("8")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := registry(t, Registration{Name: "a", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) { return tc.result, nil }}})
			if got, err := r.Resolve(context.Background(), tc.query, []string{"a"}); err == nil || got != nil {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	record.Provenance = actresscache.Provenance{}
	r := registry(t, Registration{Name: "a", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, nil
	}}})
	if got, err := r.Resolve(context.Background(), claim("7"), []string{"a"}); err == nil || got != nil {
		t.Fatalf("%+v %v", got, err)
	}
	record.Source = "!"
	if got, err := r.Resolve(context.Background(), claim("7"), []string{"a"}); err == nil || got != nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := r.Resolve(context.Background(), Query{}, []string{"a"}); err == nil {
		t.Fatal("invalid query")
	}
	if _, err := NewRegistry([]Registration{{Name: "!", Provider: &stub{}}}, SystemClock{}, time.Second); err == nil {
		t.Fatal("invalid name")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider(t, s, "fixture_a").LookupEvidence(ctx, claim("7")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := (SystemClock{}).Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}
func TestMissingProviderCannotImplyCompleteUniqueEvidence(t *testing.T) {
	s := fixture(t)
	r := registry(t, Registration{Name: "present", Enabled: true, Provider: provider(t, s, "fixture_a")}, Registration{Name: "missing", Enabled: true, Provider: provider(t, nil, "fixture_b")})
	got, err := r.Resolve(context.Background(), ref("fixture_a", "actor-1"), []string{"present", "missing"})
	if err != nil || got.Outcome != NoSnapshot || len(got.Observations) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAdapterCannotRewriteFrozenQuery(t *testing.T) {
	s := fixture(t)
	record := s.Records()[0]
	for _, q := range []Query{ref(record.Source, "different-requested-identity"), claim("different-claim")} {
		before := q.clone()
		r := registry(t, Registration{Name: "adapter", Enabled: true, Provider: &stub{fn: func(_ context.Context, in Query) (Result, error) {
			if in.Reference != nil {
				in.Reference.ID = record.SourceID
			} else {
				in.Claim.Value = "7"
			}
			return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, nil
		}}})
		if got, err := r.Resolve(context.Background(), q, []string{"adapter"}); err == nil || got != nil {
			t.Fatalf("rewritten query accepted: %+v %v", got, err)
		}
		if !reflect.DeepEqual(q, before) {
			t.Fatal("adapter mutated caller query")
		}
	}
}
func TestEachAdapterReceivesIndependentQuery(t *testing.T) {
	s := fixture(t)
	for _, q := range []Query{ref("fixture_a", "actor-1"), claim("7")} {
		before := q.clone()
		first := &stub{fn: func(_ context.Context, in Query) (Result, error) {
			if in.Reference != nil {
				in.Reference.ID = "changed"
			} else {
				in.Claim.Value = "changed"
			}
			return Result{Outcome: Unmatched, SnapshotDigest: s.Digest()}, nil
		}}
		second := &stub{fn: func(_ context.Context, in Query) (Result, error) {
			if !reflect.DeepEqual(in, before) {
				t.Fatal("adapter saw another adapter's mutation")
			}
			return Result{Outcome: Unmatched, SnapshotDigest: s.Digest()}, nil
		}}
		r := registry(t, Registration{Name: "first", Enabled: true, Provider: first}, Registration{Name: "second", Enabled: true, Provider: second})
		if _, err := r.Resolve(context.Background(), q, []string{"first", "second"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(q, before) {
			t.Fatal("caller query changed")
		}
	}
}
func TestConflictingSameKeyAndDigestFailsClosed(t *testing.T) {
	s := fixture(t)
	a := s.Records()[0]
	b := s.Records()[0]
	b.Metadata.JapaneseName = "different metadata under same digest"
	r := registry(t, Registration{Name: "a", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &a}, nil
	}}}, Registration{Name: "b", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &b}, nil
	}}})
	if got, err := r.Resolve(context.Background(), ref(a.Source, a.SourceID), []string{"a", "b"}); err == nil || got != nil {
		t.Fatalf("conflicting evidence accepted: %+v %v", got, err)
	}
}
func TestRegistryOwnsAdapterEvidence(t *testing.T) {
	s := fixture(t)
	record := s.Records()[0]
	expected := cloneEvidence(&record)
	r := registry(t, Registration{Name: "a", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, nil
	}}}, Registration{Name: "b", Enabled: true, Provider: &stub{fn: func(context.Context, Query) (Result, error) {
		// A subsequent adapter can reuse/mutate storage retained by the first.
		record.Metadata.Aliases[0] = "changed during next call"
		record.ExternalIDs[0].Value = "changed during next call"
		return Result{Outcome: Unmatched, SnapshotDigest: s.Digest()}, nil
	}}})
	got, err := r.Resolve(context.Background(), ref(record.Source, record.SourceID), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	record.SourceID = "changed after return"
	record.Metadata.JapaneseName = "changed"
	record.Metadata.Aliases[0] = "changed"
	record.ExternalIDs[0].Value = "changed"
	if !reflect.DeepEqual(got.Observations[0].Result.Evidence, expected) {
		t.Fatal("returned evidence aliases adapter storage")
	}
	got.Observations[0].Result.Evidence.Metadata.Aliases[0] = "caller edit"
	if record.Metadata.Aliases[0] == "caller edit" {
		t.Fatal("caller can mutate adapter storage")
	}
}
func TestIdenticalDuplicateEvidenceHasIndependentObservations(t *testing.T) {
	s := fixture(t)
	record := s.Records()[0]
	adapter := &stub{fn: func(context.Context, Query) (Result, error) {
		return Result{Outcome: Available, SnapshotDigest: s.Digest(), Evidence: &record}, nil
	}}
	r := registry(t, Registration{Name: "a", Enabled: true, Provider: adapter}, Registration{Name: "b", Enabled: true, Provider: adapter})
	got, err := r.Resolve(context.Background(), ref(record.Source, record.SourceID), []string{"a", "b"})
	if err != nil || got.Outcome != Available {
		t.Fatalf("%+v %v", got, err)
	}
	got.Observations[0].Result.Evidence.Metadata.Aliases[0] = "changed"
	got.Observations[0].Result.Evidence.ExternalIDs[0].Value = "changed"
	if got.Observations[1].Result.Evidence.Metadata.Aliases[0] == "changed" || got.Observations[1].Result.Evidence.ExternalIDs[0].Value == "changed" {
		t.Fatal("observations share slices")
	}
}
