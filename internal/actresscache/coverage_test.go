package actresscache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sourceFunc struct {
	name    string
	collect func(context.Context, SourceOptions, func(Candidate) error) error
}

func (s sourceFunc) Name() string { return s.name }
func (s sourceFunc) Collect(ctx context.Context, options SourceOptions, emit func(Candidate) error) error {
	return s.collect(ctx, options, emit)
}

func registryWith(source Source) *Registry {
	registry := NewRegistry()
	registry.Register(source.Name(), func() Source { return source })
	return registry
}

func TestBuildDefaultValidatorAndSourceCallbacks(t *testing.T) {
	image := makeJPEG(t, 80, 80)
	fetcher := testFetcher(http.StatusOK, "image/jpeg", image, nil)
	source := sourceFunc{name: "test", collect: func(_ context.Context, options SourceOptions, emit func(Candidate) error) error {
		options.MarkSeen("")
		options.MarkSeen("one")
		assert.False(t, options.ShouldSkip("missing"))
		require.NoError(t, emit(Candidate{Key: "one", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/one.jpg"}))
		options.MarkComplete()
		return nil
	}}
	cache, report, err := Build(context.Background(), BuildOptions{
		Registry: registryWith(source), Sources: []string{" TEST "},
		SourceOptions: SourceOptions{Fetcher: fetcher},
	})
	require.NoError(t, err)
	require.Len(t, cache.Records, 1)
	assert.Equal(t, "test", cache.Records[0].PrimarySource)
	assert.Equal(t, 1, report.Validated)
}

func TestBuildRejectsStateAndCandidateFailures(t *testing.T) {
	registry := registryWith(sourceFunc{name: "test", collect: func(context.Context, SourceOptions, func(Candidate) error) error { return nil }})
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: t.TempDir(), ValidateThumbnail: testValidator})
	require.ErrorContains(t, err, "open actress cache state")

	cases := []struct {
		name      string
		candidate Candidate
		want      string
	}{
		{name: "missing key", candidate: Candidate{JapaneseName: "花子", ThumbURL: "thumb"}, want: "without a key"},
		{name: "missing identity", candidate: Candidate{Key: "key", ThumbURL: "thumb"}, want: "no stable identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := sourceFunc{name: "test", collect: func(_ context.Context, _ SourceOptions, emit func(Candidate) error) error {
				return emit(tc.candidate)
			}}
			_, report, err := Build(context.Background(), BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, ValidateThumbnail: testValidator})
			if tc.name == "missing key" {
				require.ErrorContains(t, err, tc.want)
			} else {
				// An unmergeable candidate is a healable source-data gap:
				// transient, not a terminal rejection.
				require.NoError(t, err)
				assert.Equal(t, 0, report.Rejected)
				assert.Equal(t, 1, report.Failed)
			}
		})
	}
}

func TestBuildIgnoresUnusableAndUnselectedCachedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	entries := []StateEntry{
		{Key: "failed", Status: "failed"},
		{Key: "nil-candidate", Status: "ok", Thumbnail: &ThumbnailValidation{}},
		{Key: "nil-thumbnail", Status: "ok", Candidate: &Candidate{ThumbURL: "https://example.test/x.jpg"}},
		{Key: "blank-thumbnail", Status: "ok", Candidate: &Candidate{}, Thumbnail: &ThumbnailValidation{}},
		{Key: "placeholder", Status: "ok", Candidate: &Candidate{ThumbURL: "https://www.minnano-av.com/p_actress_125_125/000/np.gif"}, Thumbnail: &ThumbnailValidation{}},
		{Key: "other", Status: "ok", Candidate: &Candidate{Source: "other", JapaneseName: "他", ThumbURL: "https://example.test/x.jpg"}, Thumbnail: &ThumbnailValidation{CheckedAt: "now", SHA256: "o", Bytes: 10, Width: 100, Height: 100, Format: "jpeg"}},
	}
	var data bytes.Buffer
	for _, entry := range entries {
		require.NoError(t, json.NewEncoder(&data).Encode(entry))
	}
	require.NoError(t, os.WriteFile(path, data.Bytes(), 0o600))
	source := sourceFunc{name: "test", collect: func(context.Context, SourceOptions, func(Candidate) error) error { return nil }}
	cache, report, err := Build(context.Background(), BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: path, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	assert.Empty(t, cache.Records)
	assert.Zero(t, report.Cached)
}

func TestBuildSourceErrorsAndCancellationPreference(t *testing.T) {
	firstStarted := make(chan struct{})
	registry := NewRegistry()
	registry.Register("cancelled", func() Source {
		return sourceFunc{name: "cancelled", collect: func(ctx context.Context, _ SourceOptions, _ func(Candidate) error) error {
			close(firstStarted)
			<-ctx.Done()
			return ctx.Err()
		}}
	})
	registry.Register("failed", func() Source {
		return sourceFunc{name: "failed", collect: func(context.Context, SourceOptions, func(Candidate) error) error {
			<-firstStarted
			return errors.New("specific failure")
		}}
	})
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"cancelled", "failed"}, ValidateThumbnail: testValidator})
	require.ErrorContains(t, err, "specific failure")
}

func TestBuildPruningHandlesSupersededStateAndUnknownCandidateSource(t *testing.T) {
	source := sourceFunc{name: "test", collect: func(_ context.Context, options SourceOptions, emit func(Candidate) error) error {
		candidate := Candidate{Key: "same", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "thumb"}
		require.NoError(t, emit(candidate))
		require.NoError(t, options.RecordFailure(candidate, errors.New("temporary")))
		require.NoError(t, emit(Candidate{Key: "foreign", Source: "other", SourceID: "2", JapaneseName: "他", ThumbURL: "thumb"}))
		options.MarkComplete()
		return nil
	}}
	cache, report, err := Build(context.Background(), BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	assert.Empty(t, cache.Records)
	assert.Equal(t, 1, report.Failed)
}

func TestBuildRefreshDoesNotSkipAndDisablesPruningAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	candidate := Candidate{Key: "one", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}
	entry := StateEntry{Key: "one", Status: "ok", Candidate: &candidate, Thumbnail: &ThumbnailValidation{CheckedAt: "now", SHA256: "one", Bytes: 10, Width: 100, Height: 100, Format: "jpeg"}}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))
	source := sourceFunc{name: "test", collect: func(_ context.Context, options SourceOptions, _ func(Candidate) error) error {
		assert.False(t, options.ShouldSkip("one"))
		options.MarkComplete()
		return nil
	}}
	_, _, err = Build(context.Background(), BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: path, Refresh: true, SourceOptions: SourceOptions{Limit: 1}, ValidateThumbnail: testValidator})
	// --refresh with --limit would publish unrevalidated state while pruning
	// is disabled: refused loud instead.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--refresh cannot be combined with --limit")
}

// A single candidate matching two groups merges them into the base one;
// a fully stale/emptied group sessions through the record pivot.
func TestClassifyStateFailureLegs(t *testing.T) {
	// Durable content defects are permanent.
	assert.Equal(t, "rejected", classifyStateFailure(Candidate{}, &FetchTooLargeError{URL: "x", Limit: 1}))
	assert.Equal(t, "rejected", classifyStateFailure(Candidate{}, &ThumbnailRejectedError{Reason: "known placeholder"}))
	// Healable source-data gaps are transient.
	assert.Equal(t, "failed", classifyStateFailure(Candidate{}, &ThumbnailRejectedError{Reason: "thumbnail URL is empty"}))
	assert.Equal(t, "failed", classifyStateFailure(Candidate{Source: "r18dev"}, &HTTPError{StatusCode: 404, URL: "u"}), "fabricated-guess 404 must not ghost the actress")
	assert.Equal(t, "failed", classifyStateFailure(Candidate{}, errors.New("candidate has no stable identity")))
	// Mixed rule: rejected-type bit 404 for a DIFFERENT source stays permanent.
	assert.Equal(t, "rejected", classifyStateFailure(Candidate{Source: "test"}, &ThumbnailRejectedError{Reason: "404 fake"}))
	// Plain fetch failures are transient.
	assert.Equal(t, "failed", classifyStateFailure(Candidate{}, &HTTPError{StatusCode: 404, URL: "u"}))
}

func TestMergeCandidatesMultiGroupProbe(t *testing.T) {
	mk := func(key, jp string, aliases ...string) rankedCandidate {
		return rankedCandidate{candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}
	dmk := func(key, jp string, dmmID int, aliases ...string) rankedCandidate {
		return rankedCandidate{candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", DMMID: dmmID, JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}

	groups := make([]candidateGroup, 0)
	identityGroups := map[string]map[int]struct{}{}
	for _, c := range []rankedCandidate{mk("a", "一", "shared-a"), mk("b", "二", "shared-b")} {
		ids := candidateIdentities(c.candidate.Candidate)
		groups = append(groups, newCandidateGroup(c, ids))
		registerGroup(identityGroups, len(groups)-1, ids)
	}
	// A nameless DMM bridge must not join groups whose Japanese identities
	// conflict: that merges distinct actresses sharing a romanized name.
	bridge := dmk("bridge", "", 7, "shared-a", "shared-b")
	matches := compatibleGroups(groups, identityGroups, candidateIdentities(bridge.candidate.Candidate), bridge.candidate.Candidate)
	require.Empty(t, matches)

	// Non-conflicting nameless groups sharing a romanized key still bridge.
	nameless := make([]candidateGroup, 0)
	identity2 := map[string]map[int]struct{}{}
	for _, c := range []rankedCandidate{mk("x", "", "shared-a"), mk("y", "", "shared-b")} {
		ids := candidateIdentities(c.candidate.Candidate)
		nameless = append(nameless, newCandidateGroup(c, ids))
		registerGroup(identity2, len(nameless)-1, ids)
	}
	matches = compatibleGroups(nameless, identity2, candidateIdentities(bridge.candidate.Candidate), bridge.candidate.Candidate)
	require.Len(t, matches, 2, "no Japanese-name conflict: bridge may join both")
}

func TestMergeCandidatesMergesMultipleMatchedGroups(t *testing.T) {
	mk := func(key, jp string, aliases ...string) rankedCandidate {
		return rankedCandidate{candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}
	dmk := func(key, jp string, dmmID int, aliases ...string) rankedCandidate {
		return rankedCandidate{candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", DMMID: dmmID, JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}
	// A romanized-only DMM bridge with no Japanese name must not collapse
	// both groups onto one record regardless of merge order: the invariant is
	// that distinct actresses are never unified under this bridge.
	records := mergeCandidates([]rankedCandidate{
		mk("a", "一", "shared-a"),
		mk("b", "二", "shared-b"),
		dmk("bridge", "", 7, "shared-a", "shared-b"),
	})
	require.GreaterOrEqual(t, len(records), 2, "conflicting groups must survive the nameless bridge")
}

func TestMergeCandidatesBridgesCompatibleGroups(t *testing.T) {
	candidate := func(key, jp string, dmmID, rank int, aliases ...string) rankedCandidate {
		return rankedCandidate{rank: rank, candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", DMMID: dmmID, JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}

	// Same-name bridge still merges the two groups onto the DMM identity.
	records := mergeCandidates([]rankedCandidate{
		candidate("one", "中", 0, 0, "alias-one"),
		candidate("two", "中", 0, 0, "alias-two"),
		candidate("bridge", "中", 1, 1, "alias-one", "alias-two"),
	})
	require.Len(t, records, 1)
	assert.Len(t, records[0].Sources, 3)

	// A DMM-backed bridge whose own name conflicts must not collapse different
	// actresses onto that identity just because romanized aliases overlap.
	conflicting := mergeCandidates([]rankedCandidate{
		candidate("one", "一", 0, 0, "alias-one"),
		candidate("two", "二", 0, 0, "alias-two"),
		candidate("bridge", "三", 1, 1, "alias-one", "alias-two"),
	})
	require.Len(t, conflicting, 3)
}

func TestMergeCandidateGroupCarriesDMMIDs(t *testing.T) {
	groups := []candidateGroup{
		{dmmIDs: map[int]struct{}{}},
		{items: []rankedCandidate{{}}, identities: []string{"jp:name"}, dmmIDs: map[int]struct{}{42: {}}},
	}
	identityGroups := make(map[string]map[int]struct{})
	mergeCandidateGroup(groups, identityGroups, 0, 1)
	assert.Contains(t, groups[0].dmmIDs, 42)
	assert.Contains(t, identityGroups["jp:name"], 0)
	assert.Empty(t, groups[1].items)
}

func TestCompatibleGroupsAmbiguityBranches(t *testing.T) {
	groups := []candidateGroup{
		{items: []rankedCandidate{{candidate: ValidatedCandidate{Candidate: Candidate{JapaneseName: "same"}}}}, identities: []string{"jp:same"}, dmmIDs: map[int]struct{}{1: {}}},
		{items: []rankedCandidate{{candidate: ValidatedCandidate{Candidate: Candidate{JapaneseName: "same"}}}}, identities: []string{"jp:same"}, dmmIDs: map[int]struct{}{}},
	}
	identityGroups := map[string]map[int]struct{}{"jp:same": {0: {}, 1: {}, 99: {}}}
	assert.Equal(t, []int{1}, compatibleGroups(groups, identityGroups, []string{"jp:same"}, Candidate{JapaneseName: "same"}))
	groups[0].dmmIDs = map[int]struct{}{}
	assert.Nil(t, compatibleGroups(groups, identityGroups, []string{"jp:same"}, Candidate{JapaneseName: "same"}))
	assert.True(t, compatibleGroup(groups[0], Candidate{}))
}

func TestNormalizeSourcesAndBuiltinKeyEdges(t *testing.T) {
	_, err := normalizeSources([]string{" "})
	require.ErrorContains(t, err, "at least one")
	assert.Equal(t, "actress:name:jane doe", builtinKey(Record{FirstName: "Jane", LastName: "Doe"}, Candidate{}))
}

func TestFetchErrorAndRequestBranches(t *testing.T) {
	var nilFetcher *Fetcher
	_, _, err := nilFetcher.Get(context.Background(), "https://example.test", "*/*", 1)
	require.ErrorContains(t, err, "not initialized")
	_, _, err = mustFetcher(NewFetcher(nil, 0, "test")).Get(context.Background(), "://bad", "*/*", 1)
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = mustFetcher(NewFetcher(nil, time.Second, "test")).Get(ctx, "https://example.test", "*/*", 1)
	require.ErrorIs(t, err, context.Canceled)

	calls := 0
	client := &http.Client{Transport: fetchTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("network down")
	})}
	_, _, err = mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test", "*/*", 1)
	require.ErrorContains(t, err, "network down")
	assert.Equal(t, maxFetchAttempts, calls)

	client = &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"X-Test": []string{"yes"}}, Body: io.NopCloser(strings.NewReader("no")), Request: req}, nil
	})}
	_, headers, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test", "*/*", 1)
	var statusErr *HTTPError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, "yes", headers.Get("X-Test"))
	assert.False(t, statusErr.IsTransient())
	assert.False(t, (*HTTPError)(nil).IsTransient())
}

func TestFetcherDefaultsLimitAndRejectsOversize(t *testing.T) {
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	body, _, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test", "*/*", 0)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	_, _, err = mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(context.Background(), "https://example.test", "*/*", 1)
	require.ErrorContains(t, err, "exceeds")
}

func TestFetcherRedirectCallbackBranches(t *testing.T) {
	// Stub the resolver: fake .test names must never hit the system resolver
	// (hijacking resolvers answering .test privately would flip the asserted
	// branch to a block).
	prevLookup := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, errors.New("offline") }
	defer func() { lookupIP = prevLookup }()
	redirectErr := errors.New("redirect denied")
	// Explicit no-proxy transport: the default transport honors HTTP(S)_PROXY,
	// and a proxy-enabled CI host would route the goal-URL's DNS preflight into
	// an UnverifiableHostError before the supplied callback branch runs.
	client := &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return redirectErr }}
	fetcher := mustFetcher(NewFetcherWithHostDelays(client, 0, "test", nil))
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	require.ErrorIs(t, fetcher.client.CheckRedirect(req, nil), redirectErr)

	fetcher = mustFetcher(NewFetcher(&http.Client{Transport: &http.Transport{}}, time.Hour, "test"))
	req, err = http.NewRequest(http.MethodGet, "https://other.test", nil)
	require.NoError(t, err)
	require.NoError(t, fetcher.client.CheckRedirect(req, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://other.test", nil)
	require.NoError(t, err)
	require.ErrorIs(t, fetcher.client.CheckRedirect(req, nil), context.Canceled)
}

func TestRetryDelayVariants(t *testing.T) {
	assert.Equal(t, 2*time.Second, retryDelay(http.Header{"Retry-After": []string{"2"}}, 0))
	assert.Equal(t, maxRetryDelay, retryDelay(http.Header{"Retry-After": []string{fmt.Sprint(int64(maxRetryDelay / time.Second))}}, 0))
	assert.Equal(t, maxRetryDelay, retryDelay(http.Header{"Retry-After": []string{"999999999999999999999"}}, 0))
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	assert.InDelta(t, time.Minute, retryDelay(http.Header{"Retry-After": []string{future}}, 0), float64(2*time.Second))
	farFuture := time.Now().Add(2 * maxRetryDelay).UTC().Format(http.TimeFormat)
	assert.Equal(t, maxRetryDelay, retryDelay(http.Header{"Retry-After": []string{farFuture}}, 0))
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	assert.Zero(t, retryDelay(http.Header{"Retry-After": []string{past}}, 0))
	assert.Equal(t, retryBaseDelay, retryDelay(nil, 0))
	assert.Equal(t, maxRetryDelay, retryDelay(nil, 20))
}

func TestFetcherCancellationDuringRetries(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &http.Client{Transport: fetchTransport(func(*http.Request) (*http.Response, error) {
			time.AfterFunc(time.Millisecond, cancel)
			return nil, errors.New("retry")
		})}
		_, _, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(ctx, "https://example.test", "*/*", 1)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("body", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
			time.AfterFunc(time.Millisecond, cancel)
			return &http.Response{StatusCode: http.StatusOK, Body: &failAfterReader{}, Request: req}, nil
		})}
		_, _, err := mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(ctx, "https://example.test", "*/*", 100)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestFetcherLimiterCancellation(t *testing.T) {
	fetcher := mustFetcher(NewFetcher(nil, time.Hour, "test"))
	fetcher.limiterForHost("example.test").Wait(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := fetcher.Get(ctx, "https://example.test", "*/*", 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, waitRetry(ctx, time.Hour), context.Canceled)
}

func TestRuntimeReachabilityAndDecodeFailures(t *testing.T) {
	assert.False(t, runtimeRecordReachable(RuntimeRecord{}, 0, nil, nil, nil))
	assert.True(t, runtimeRecordReachable(RuntimeRecord{FirstName: "Jane"}, 0, nil, nil, map[string]map[int]struct{}{"jane": {0: {}}}))

	encodeGzip := func(parts ...string) []byte {
		var data bytes.Buffer
		writer := gzip.NewWriter(&data)
		for _, part := range parts {
			_, err := writer.Write([]byte(part))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		return data.Bytes()
	}
	_, err := decodeRuntimeCache(bytes.NewReader(encodeGzip("not-json")))
	require.ErrorContains(t, err, "parse")
	_, err = decodeRuntimeCache(bytes.NewReader(encodeGzip(`{"schema_version":1}{"extra":true}`)))
	require.ErrorContains(t, err, "trailing JSON")
	_, err = decodeRuntimeCache(bytes.NewReader(encodeGzip(`{"schema_version":1}x`)))
	require.ErrorContains(t, err, "parse")
}

func TestFileWritersRejectInvalidDirectories(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(bad, []byte("x"), 0o600))
	assert.Error(t, WriteFile(filepath.Join(bad, "cache.json"), Cache{}))
	assert.Error(t, WriteRuntimeFile(filepath.Join(bad, "cache.json.gz"), Cache{}))
}

func TestFileWritersRejectPermissionDeniedDirectories(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission-denied assertions require a non-root Unix user")
	}

	locked := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	assert.Error(t, WriteFile(filepath.Join(locked, "cache.json"), Cache{}))
	assert.Error(t, WriteRuntimeFile(filepath.Join(locked, "cache.json.gz"), Cache{}))
}

func TestStateErrorAndParsingBranches(t *testing.T) {
	_, err := openState(t.TempDir())
	require.Error(t, err)
	bad := filepath.Join(t.TempDir(), "parent")
	require.NoError(t, os.WriteFile(bad, []byte("x"), 0o600))
	_, err = openState(filepath.Join(bad, "state.jsonl"))
	require.Error(t, err)

	entries := make(map[string]StateEntry)
	incomplete, err := parseState([]byte("  \n{bad"), entries)
	require.NoError(t, err)
	assert.True(t, incomplete)
	incomplete, err = parseState([]byte("{bad\n"), entries)
	require.Error(t, err)
	assert.False(t, incomplete)

	type failingReader struct{}
	_, _ = failingReader{}, entries
	assert.NoError(t, repairStateTail(filepath.Join(t.TempDir(), "missing"), nil, false))
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadStateReaderErrorAndBlankLines(t *testing.T) {
	sentinel := errors.New("read failed")
	require.ErrorIs(t, readState(errorReader{sentinel}, map[string]StateEntry{}), sentinel)
	entries := make(map[string]StateEntry)
	require.NoError(t, readState(strings.NewReader(" \n{\"key\":\"\",\"status\":\"ok\"}\n"), entries))
	assert.Empty(t, entries)
}

func TestStateAppendEncoderError(t *testing.T) {
	store := &stateStore{entries: make(map[string]StateEntry)}
	store.encoder = json.NewEncoder(errorWriter{})
	require.Error(t, store.append(StateEntry{Key: "one"}))
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestKnownSourcePlaceholderNilURL(t *testing.T) {
	assert.False(t, isKnownSourcePlaceholder(nil))
}

func TestThumbnailValidatorCacheInvalidAndBlankURL(t *testing.T) {
	var nilCache *thumbnailValidatorCache
	_, err := nilCache.Validate(context.Background(), Candidate{})
	require.ErrorContains(t, err, "not initialized")
	calls := 0
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		calls++
		return ThumbnailValidation{SHA256: fmt.Sprint(calls)}, nil
	})
	first, err := cache.Validate(context.Background(), Candidate{})
	require.NoError(t, err)
	second, err := cache.Validate(context.Background(), Candidate{})
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestThumbnailValidatorCacheHandlesForeignSingleflightValue(t *testing.T) {
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		return ThumbnailValidation{}, nil
	})
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _, _ = cache.group.Do("https://example.test/x.jpg", func() (any, error) {
			close(started)
			<-release
			return "foreign", errors.New("foreign error")
		})
	}()
	<-started
	time.AfterFunc(time.Millisecond, func() { close(release) })
	_, err := cache.Validate(context.Background(), Candidate{ThumbURL: "https://example.test/x.jpg"})
	require.ErrorContains(t, err, "foreign error")
}

func TestCachedCandidateReusablePolicyBranches(t *testing.T) {
	mkEntry := func(url string, thumb *ThumbnailValidation, private bool) *StateEntry {
		return &StateEntry{Candidate: &Candidate{ThumbURL: url}, Thumbnail: thumb, ValidatedWithPrivateHosts: private}
	}
	monoThumb := &ThumbnailValidation{Bytes: 100, Width: 100, Height: 100}
	assert.False(t, cachedCandidateReusable(mkEntry("https://example.test/t.jpg", &ThumbnailValidation{Bytes: 0, Width: 100, Height: 100}, false), 64, 1<<20, false), "unknown bytes must revalidate")
	assert.False(t, cachedCandidateReusable(mkEntry("https://example.test/t.jpg", &ThumbnailValidation{Bytes: 2 << 20, Width: 100, Height: 100}, false), 64, 1<<20, false), "oversized bytes must revalidate")
	assert.True(t, cachedCandidateReusable(mkEntry("https://example.test/t.jpg", monoThumb, false), 64, 1<<20, false))

	// Thumbnails fetched under --allow-private-hosts may carry private URLs:
	// a default-safe run must not reuse (and later embed) them.
	assert.False(t, cachedCandidateReusable(mkEntry("http://127.0.0.1:8080/t.jpg", monoThumb, true), 64, 1<<20, false), "mode-flagged private entry rejected when private hosts disabled")
	// Even without the mode flag, a lexically private URL is rejected.
	assert.False(t, cachedCandidateReusable(mkEntry("http://127.0.0.1:8080/t.jpg", monoThumb, false), 64, 1<<20, false), "lexically private URL rejected for unflagged legacy entries")
	// Hostnames that resolve privately are caught via the mode marker even
	// though they read as public: mirror.lan -> 10.0.0.5.
	assert.False(t, cachedCandidateReusable(mkEntry("http://mirror.lan/t.jpg", monoThumb, true), 64, 1<<20, false), "internal hostname proven under mirror mode must revalidate")
	assert.True(t, cachedCandidateReusable(mkEntry("http://mirror.lan/t.jpg", monoThumb, true), 64, 1<<20, true), "mirror mode may reuse its own entries")
	assert.True(t, cachedCandidateReusable(mkEntry("http://127.0.0.1:8080/t.jpg", monoThumb, false), 64, 1<<20, true), "allowPrivateHosts keeps legacy unflagged entries usable")
}

func TestBuiltinIndexHelpersAndLookupNames(t *testing.T) {
	index := make(map[int]int)
	ambiguous := make(map[int]struct{})
	addDMMIndex(index, ambiguous, 0, 1)
	addDMMIndex(index, ambiguous, 1, 1)
	addDMMIndex(index, ambiguous, 1, 2)
	addDMMIndex(index, ambiguous, 1, 3)
	assert.NotContains(t, index, 1)
	assert.Contains(t, ambiguous, 1)

	loadBuiltin()
	var jpRecord, nameRecord *RuntimeRecord
	for i := range builtinIndex.records {
		record := &builtinIndex.records[i]
		if jpRecord == nil && normalizeIdentity(record.JapaneseName) != "" {
			jpRecord = record
		}
		if nameRecord == nil && normalizeIdentity(record.FirstName+" "+record.LastName) != "" {
			nameRecord = record
		}
		if jpRecord != nil && nameRecord != nil {
			break
		}
	}
	require.NotNil(t, jpRecord)
	found, ok := Lookup(0, jpRecord.JapaneseName, "", "")
	require.True(t, ok)
	assert.Equal(t, jpRecord.BuiltinKey, found.BuiltinKey)
	require.NotNil(t, nameRecord)
	found, ok = Lookup(0, "", nameRecord.FirstName, nameRecord.LastName)
	require.True(t, ok)
	assert.Equal(t, nameRecord.BuiltinKey, found.BuiltinKey)
}

func resetBuiltinIndex() {
	builtinIndex.Once = sync.Once{}
	builtinIndex.records = nil
	builtinIndex.err = nil
	builtinIndex.byDMM = nil
	builtinIndex.byJP = nil
	builtinIndex.byName = nil
	builtinIndex.ambiguousD = nil
	builtinIndex.ambiguousP = nil
	builtinIndex.ambiguousN = nil
}

func TestBuiltinErrorBranches(t *testing.T) {
	originalData := builtinData
	defer func() {
		builtinData = originalData
		resetBuiltinIndex()
		loadBuiltin()
	}()
	builtinData = []byte("invalid")
	resetBuiltinIndex()
	cache, err := Builtin()
	require.Error(t, err)
	assert.Empty(t, cache.Records)
	assert.Nil(t, BuiltinData())
	_, ok := Lookup(1, "x", "y", "z")
	assert.False(t, ok)
}

type fakeTempFile struct {
	name       string
	writeErr   error
	chmodErr   error
	closeErr   error
	syncErr    error
	writeCount int
	failWrite  int
	data       bytes.Buffer
}

func (f *fakeTempFile) Write(p []byte) (int, error) {
	f.writeCount++
	if f.writeErr != nil && (f.failWrite == 0 || f.writeCount >= f.failWrite) {
		return 0, f.writeErr
	}
	return f.data.Write(p)
}
func (f *fakeTempFile) Name() string            { return f.name }
func (f *fakeTempFile) Chmod(os.FileMode) error { return f.chmodErr }
func (f *fakeTempFile) Sync() error             { return f.syncErr }
func (f *fakeTempFile) Close() error            { return f.closeErr }

func TestWriteFileInjectedFailures(t *testing.T) {
	original := createCacheTemp
	defer func() { createCacheTemp = original }()
	path := filepath.Join(t.TempDir(), "cache.json")
	for _, tc := range []struct {
		name string
		file *fakeTempFile
	}{
		{name: "encode", file: &fakeTempFile{writeErr: errors.New("encode")}},
		{name: "chmod", file: &fakeTempFile{chmodErr: errors.New("chmod")}},
		{name: "close", file: &fakeTempFile{closeErr: errors.New("close")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.file.name = filepath.Join(t.TempDir(), "temporary")
			createCacheTemp = func(string, string) (cacheTempFile, error) { return tc.file, nil }
			require.Error(t, WriteFile(path, Cache{}))
		})
	}
}

func TestWriteRuntimeFileInjectedFailures(t *testing.T) {
	originalTemp, originalWriter := createRuntimeTemp, newRuntimeGzipWriter
	defer func() { createRuntimeTemp, newRuntimeGzipWriter = originalTemp, originalWriter }()
	path := filepath.Join(t.TempDir(), "cache.json.gz")

	newRuntimeGzipWriter = func(io.Writer, int) (*gzip.Writer, error) { return nil, errors.New("gzip") }
	createRuntimeTemp = func(string, string) (cacheTempFile, error) {
		return &fakeTempFile{name: filepath.Join(t.TempDir(), "tmp")}, nil
	}
	require.ErrorContains(t, WriteRuntimeFile(path, Cache{}), "gzip")
	newRuntimeGzipWriter = originalWriter

	for _, tc := range []struct {
		name string
		file *fakeTempFile
	}{
		{name: "encode", file: &fakeTempFile{writeErr: errors.New("encode"), failWrite: 1}},
		{name: "writer close", file: &fakeTempFile{writeErr: errors.New("flush"), failWrite: 2}},
		{name: "chmod", file: &fakeTempFile{chmodErr: errors.New("chmod")}},
		{name: "close", file: &fakeTempFile{closeErr: errors.New("close")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.file.name = filepath.Join(t.TempDir(), "temporary")
			createRuntimeTemp = func(string, string) (cacheTempFile, error) { return tc.file, nil }
			require.Error(t, WriteRuntimeFile(path, Cache{}))
		})
	}
}

func TestDecodeRuntimeCacheCloseFailure(t *testing.T) {
	original := newRuntimeGzipReader
	defer func() { newRuntimeGzipReader = original }()
	newRuntimeGzipReader = func(io.Reader) (io.ReadCloser, error) {
		return &errorReadCloser{Reader: strings.NewReader(`{"schema_version":1}`), closeErr: errors.New("trailer")}, nil
	}
	_, err := decodeRuntimeCache(strings.NewReader("ignored"))
	require.ErrorContains(t, err, "trailer")
}

type errorReadCloser struct {
	io.Reader
	closeErr error
}

func (r *errorReadCloser) Close() error { return r.closeErr }

func TestBuiltinDataMarshalFailure(t *testing.T) {
	original := marshalBuiltinCache
	defer func() { marshalBuiltinCache = original }()
	marshalBuiltinCache = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	assert.Nil(t, BuiltinData())
}

func TestFetcherExhaustedAndBodyCancellation(t *testing.T) {
	original := fetchAttempts
	fetchAttempts = 0
	// Restore via defer: a require failure here must not poison every later
	// fetcher test in the process.
	defer func() { fetchAttempts = original }()
	// Resolver stub: fake .test names must never reach the system resolver; a
	// hijacking resolver answering .test privately would flip the outcome.
	prevLookup := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, errors.New("offline") }
	defer func() { lookupIP = prevLookup }()
	// Explicit no-proxy transport: the default transport honors HTTP(S)_PROXY,
	// which would route this request through proxy DNS preflight and surface
	// UnverifiableHostError instead of the exhaustion error under test.
	fetcher := mustFetcher(NewFetcher(&http.Client{Transport: &http.Transport{}}, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "https://example.test", "*/*", 1)
	require.ErrorContains(t, err, "attempts exhausted")
	fetchAttempts = original

	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: cancelErrorReader{cancel: cancel}, Request: req}, nil
	})}
	_, _, err = mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)).Get(ctx, "https://example.test", "*/*", 100)
	require.ErrorIs(t, err, context.Canceled)
}

type cancelErrorReader struct{ cancel context.CancelFunc }

func (r cancelErrorReader) Read([]byte) (int, error) { r.cancel(); return 0, context.Canceled }
func (cancelErrorReader) Close() error               { return nil }

func TestBuildStateWriteFailures(t *testing.T) {
	originalOpen := stateOpenFile
	defer func() { stateOpenFile = originalOpen }()
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	stateOpenFile = func(string, int, os.FileMode) (*os.File, error) { return closed, nil }

	source := sourceFunc{name: "test", collect: func(_ context.Context, _ SourceOptions, emit func(Candidate) error) error {
		return emit(Candidate{Key: "one", SourceID: "1", ThumbURL: "thumb"})
	}}
	_, _, err = Build(context.Background(), BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: filepath.Join(t.TempDir(), "state.jsonl"), ValidateThumbnail: testValidator})
	require.ErrorContains(t, err, "write state")

	store := &stateStore{entries: make(map[string]StateEntry), encoder: json.NewEncoder(errorWriter{})}
	report := BuildReport{}
	err = recordFailure(store, Candidate{Key: "one"}, errors.New("failure"), "p", &sync.Mutex{}, &report)
	require.ErrorContains(t, err, "write failed state")
}

func TestOpenStateQuarantinesCorruptCompleteLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("not-json\n"), 0o600))
	store, err := openState(path)
	require.NoError(t, err, "mid-file corruption must quarantine, not fail the build")
	require.NotNil(t, store)
	require.NoError(t, store.close())
	_, statErr := os.Stat(path + ".corrupt")
	require.NoError(t, statErr, "corrupt file must be quarantined aside")
	entries, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Empty(t, strings.TrimSpace(string(entries)), "fresh state file starts empty")
}

func TestStateInjectedFailures(t *testing.T) {
	sentinel := errors.New("injected")
	t.Run("read", func(t *testing.T) {
		original := stateReadFile
		defer func() { stateReadFile = original }()
		stateReadFile = func(string) ([]byte, error) { return nil, sentinel }
		_, err := openState("state")
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("repair", func(t *testing.T) {
		original := stateRepairTail
		defer func() { stateRepairTail = original }()
		stateRepairTail = func(string, []byte, bool) error { return sentinel }
		path := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		_, err := openState(path)
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("mkdir", func(t *testing.T) {
		original := stateMkdirAll
		defer func() { stateMkdirAll = original }()
		stateMkdirAll = func(string, os.FileMode) error { return sentinel }
		_, err := openStateWriter("state", &stateStore{})
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("open writer", func(t *testing.T) {
		original := stateOpenFile
		defer func() { stateOpenFile = original }()
		stateOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel }
		_, err := openStateWriter("state", &stateStore{})
		require.ErrorIs(t, err, sentinel)
	})
	t.Run("open repair", func(t *testing.T) {
		original := stateOpenRepairFile
		defer func() { stateOpenRepairFile = original }()
		stateOpenRepairFile = func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel }
		require.ErrorIs(t, repairStateTail("state", []byte("x"), false), sentinel)
	})
	for _, tc := range []struct {
		name       string
		incomplete bool
		install    func() func()
	}{
		{name: "truncate", incomplete: true, install: func() func() {
			original := stateTruncate
			stateTruncate = func(*os.File, int64) error { return sentinel }
			return func() { stateTruncate = original }
		}},
		{name: "seek", install: func() func() {
			original := stateSeekEnd
			stateSeekEnd = func(*os.File) error { return sentinel }
			return func() { stateSeekEnd = original }
		}},
		{name: "write", install: func() func() {
			original := stateWriteNewline
			stateWriteNewline = func(*os.File) error { return sentinel }
			return func() { stateWriteNewline = original }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore := tc.install()
			defer restore()
			path := filepath.Join(t.TempDir(), "state")
			require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
			require.ErrorIs(t, repairStateTail(path, []byte("x"), tc.incomplete), sentinel)
		})
	}
}

func TestThumbnailValidateAndCacheCachedBranch(t *testing.T) {
	cache := newThumbnailValidatorCache(func(context.Context, Candidate) (ThumbnailValidation, error) {
		t.Fatal("validator should not run")
		return ThumbnailValidation{}, nil
	})
	cache.entries["key"] = thumbnailValidationResult{validation: ThumbnailValidation{SHA256: "cached"}}
	result, err := cache.validateAndCache(context.Background(), "key", Candidate{})
	require.NoError(t, err)
	assert.Equal(t, "cached", result.validation.SHA256)
}

func TestWriteFileSyncFailure(t *testing.T) {
	original := createCacheTemp
	defer func() { createCacheTemp = original }()
	sentinel := errors.New("sync failed")
	createCacheTemp = func(dir, pattern string) (cacheTempFile, error) {
		return &fakeTempFile{name: filepath.Join(dir, "tmp.json"), syncErr: sentinel}, nil
	}
	err := WriteFile(filepath.Join(t.TempDir(), "out.json"), Cache{})
	require.ErrorIs(t, err, sentinel)
}

func TestWriteRuntimeFileSyncFailure(t *testing.T) {
	original := createRuntimeTemp
	defer func() { createRuntimeTemp = original }()
	sentinel := errors.New("sync failed")
	createRuntimeTemp = func(dir, pattern string) (cacheTempFile, error) {
		return &fakeTempFile{name: filepath.Join(dir, "tmp.json.gz"), syncErr: sentinel}, nil
	}
	err := WriteRuntimeFile(filepath.Join(t.TempDir(), "out.json.gz"), Cache{})
	require.ErrorIs(t, err, sentinel)
}

// A candidate whose canonical name B paired with alias A must merge into a
// group whose canonical name is A — the reverse direction was ignored before.
func TestBuildMergesIncomingAliasIntoExistingCanonicalGroup(t *testing.T) {
	mk := func(key, jp string, aliases ...string) rankedCandidate {
		return rankedCandidate{candidate: ValidatedCandidate{Candidate: Candidate{Key: key, Source: "test", JapaneseName: jp, Aliases: aliases, ThumbURL: "thumb"}}}
	}
	groups := make([]candidateGroup, 0)
	identityGroups := map[string]map[int]struct{}{}
	first := mk("a", "青田 加代子")
	ids := candidateIdentities(first.candidate.Candidate)
	groups = append(groups, newCandidateGroup(first, ids))
	registerGroup(identityGroups, len(groups)-1, ids)

	bridge := mk("b", "野上 律子", "青田 加代子")
	matches := compatibleGroups(groups, identityGroups, candidateIdentities(bridge.candidate.Candidate), bridge.candidate.Candidate)
	require.Len(t, matches, 1, "incoming alias must join the canonical-named group")
	records := mergeCandidates([]rankedCandidate{first, bridge})
	require.Len(t, records, 1)
	universe := append([]string{records[0].JapaneseName}, records[0].Aliases...)
	assert.ElementsMatch(t, []string{"青田 加代子", "野上 律子"}, universe, "both identities survive")
}
