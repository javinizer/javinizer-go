package actresscache

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSource struct {
	name       string
	candidates []Candidate
	collects   int
	emitted    int
}

func (s *testSource) Name() string {
	return s.name
}

func (s *testSource) Collect(_ context.Context, options SourceOptions, emit func(Candidate) error) error {
	s.collects++
	for _, candidate := range s.candidates {
		if options.MarkSeen != nil {
			options.MarkSeen(candidate.Key)
		}
		if options.ShouldSkip != nil && options.ShouldSkip(candidate.Key) {
			continue
		}
		s.emitted++
		if err := emit(candidate); err != nil {
			return err
		}
	}
	if options.MarkComplete != nil {
		options.MarkComplete()
	}
	return nil
}

func testValidator(_ context.Context, candidate Candidate) (ThumbnailValidation, error) {
	if candidate.ThumbURL == "reject" {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "invalid thumbnail"}
	}
	return ThumbnailValidation{CheckedAt: "now", SHA256: candidate.Key, Bytes: 1024, Width: 100, Height: 100, Format: "jpeg"}, nil
}

func TestBuildMergesSourcesByPriority(t *testing.T) {
	high := &testSource{
		name: "high",
		candidates: []Candidate{{
			Key: "high:42", Source: "high", SourceID: "h42", DMMID: 42,
			FirstName: "Preferred", JapaneseName: "花子", ThumbURL: "high-thumb", Aliases: []string{"旧名"},
		}},
	}
	low := &testSource{
		name: "low",
		candidates: []Candidate{{
			Key: "low:42", Source: "low", SourceID: "l42", DMMID: 42,
			LastName: "Fallback", JapaneseName: "花子", ThumbURL: "low-thumb", Aliases: []string{"別名"},
		}},
	}
	registry := NewRegistry()
	registry.Register("high", func() Source { return high })
	registry.Register("low", func() Source { return low })

	cache, report, err := Build(context.Background(), BuildOptions{
		Registry:          registry,
		Sources:           []string{"high", "low"},
		StatePath:         filepath.Join(t.TempDir(), "state.jsonl"),
		ValidateThumbnail: testValidator,
	})

	require.NoError(t, err)
	require.Len(t, cache.Records, 1)
	assert.Equal(t, 2, report.Validated)
	assert.Equal(t, "high", cache.Records[0].PrimarySource)
	assert.Equal(t, "Preferred", cache.Records[0].FirstName)
	assert.Equal(t, "Fallback", cache.Records[0].LastName)
	assert.Equal(t, "high-thumb", cache.Records[0].ThumbURL)
	assert.ElementsMatch(t, []string{"旧名", "別名"}, cache.Records[0].Aliases)
	assert.Len(t, cache.Records[0].Sources, 2)
}

func TestBuildReusesSuccessfulState(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	validatorCalls := 0
	validator := func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
		validatorCalls++
		return testValidator(ctx, candidate)
	}
	options := BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: validator}

	first, firstReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	second, secondReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Len(t, first.Records, 1)
	assert.Len(t, second.Records, 1)
	assert.Equal(t, 1, firstReport.Validated)
	assert.Equal(t, 0, secondReport.Candidates)
	assert.Equal(t, 1, secondReport.Cached)
	assert.Equal(t, 1, validatorCalls)
	assert.Equal(t, 1, source.emitted)
}

func TestBuildCachedMetricCountsOnlyReusedEntries(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	options := BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator}

	// A clean build that validates candidates reports zero reused entries.
	_, firstReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 0, firstReport.Cached, "freshly validated candidates are not cached reuse")

	// A resume that reuses the entry reports one.
	_, secondReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 1, secondReport.Cached)

	// The source disappearing wholesale would publish an EMPTY cache while
	// pruning journaled entries: refuse transactionally before any journal
	// write, so the reused entry stays intact for a later healthy run.
	source.candidates = nil
	_, _, err = Build(context.Background(), options)
	require.ErrorContains(t, err, "refusing to publish empty")
	data, readErr := os.ReadFile(statePath)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "\"status\":\"stale\"", "journal untouched by the refused publish")

	// A partial disappearance (source still emits other entries) still prunes
	// and the metric drops the pruned reuse.
	source.candidates = []Candidate{{Key: "test:2", Source: "test", SourceID: "2", JapaneseName: "插花", ThumbURL: "https://example.test/t2.jpg"}}
	_, thirdReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 0, thirdReport.Cached, "pruned previously-reused entries must decrement the metric")
}

func TestBuildRevalidatesCachedThumbnailsUnderStricterPolicy(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	validates := 0
	validator := func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
		validates++
		return testValidator(ctx, candidate)
	}
	options := BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: validator}

	_, firstReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	require.Equal(t, 1, firstReport.Validated)
	_, secondReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	require.Equal(t, 1, secondReport.Cached, "same policy reuses the cached thumbnail")
	require.Equal(t, 1, validates, "reuse means no second fetch/validation")

	// A stricter minimum dimension than the stored 100px thumbnail satisfies
	// must force revalidation instead of republishing the stale approval.
	options.MinThumbnailDimension = 200
	_, thirdReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 0, thirdReport.Cached)
	assert.Equal(t, 1, thirdReport.Validated)
	assert.Equal(t, 2, validates, "stricter policy forced revalidation")
}

// A hostname that reads as public but was validated under --allow-private-hosts
// (e.g. mirror.lan resolving to an internal address) must force revalidation
// in any later default-safe run — the lexical host check cannot see this.
func TestBuildDowngradeRevalidatesMirrorValidatedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	candidate := Candidate{Key: "mirror:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "http://mirror.lan/thumb.jpg"}
	thumb := ThumbnailValidation{CheckedAt: "now", SHA256: "m", Bytes: 100, Width: 100, Height: 100, Format: "jpeg"}
	entry := StateEntry{Key: "mirror:1", Status: "ok", Candidate: &candidate, Thumbnail: &thumb, ValidatedWithPrivateHosts: true}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, byte(0x0a)), 0o600))

	source := sourceFunc{name: "test", collect: func(context.Context, SourceOptions, func(Candidate) error) error { return nil }}
	options := BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: path, ValidateThumbnail: testValidator}

	cache, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Empty(t, cache.Records, "default-safe run must not reuse a mirror-validated thumbnail")
	assert.Zero(t, report.Cached)

	options.AllowPrivateHosts = true
	cache, report, err = Build(context.Background(), options)
	require.NoError(t, err)
	assert.Len(t, cache.Records, 1, "mirror run may reuse its own marked entries")
	assert.Equal(t, 1, report.Cached)
}

func TestBuildDoesNotReusePrivateURLThumbnailsWithoutOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	candidate := Candidate{Key: "one", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "http://127.0.0.1:8080/thumb.jpg"}
	thumb := ThumbnailValidation{CheckedAt: "now", SHA256: "one", Bytes: 100, Width: 100, Height: 100, Format: "jpeg"}
	data, err := json.Marshal(StateEntry{Key: "one", Status: "ok", Candidate: &candidate, Thumbnail: &thumb})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, byte(0x0a)), 0o600))

	source := sourceFunc{name: "test", collect: func(context.Context, SourceOptions, func(Candidate) error) error { return nil }}
	options := BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: path, ValidateThumbnail: testValidator}

	// Without the opt-in, the private URL is not trusted from state.
	cache, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Empty(t, cache.Records)
	assert.Zero(t, report.Cached)

	// With the trusted-mirror opt-in, the same state entry is reused.
	options.AllowPrivateHosts = true
	cache, report, err = Build(context.Background(), options)
	require.NoError(t, err)
	assert.Len(t, cache.Records, 1)
	assert.Equal(t, 1, report.Cached)
}

func TestBuildRefreshReportsZeroCached(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	validates := 0
	validator := func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
		validates++
		return testValidator(ctx, candidate)
	}
	options := BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: validator}

	_, firstReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	require.Equal(t, 1, firstReport.Validated)
	require.Equal(t, 0, firstReport.Cached, "fresh validation is not reuse")
	options.Refresh = true
	_, refreshReport, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 2, validates, "refresh revalidates every state entry")
	assert.Equal(t, 0, refreshReport.Cached, "refresh reuses nothing; Cached must be zero")
	assert.Equal(t, 1, refreshReport.Validated)
}

// A permanent rejection under --refresh still journals over a last-good
// entry: rejected means the thumbnail is a durable defect, not a transient
// outage, and the candidate must not come back on the next resume.
func TestBuildRefreshPermanentRejectionStillOverwritesState(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator})
	require.NoError(t, err)

	rejecter := func(context.Context, Candidate) (ThumbnailValidation, error) {
		return ThumbnailValidation{}, &ThumbnailRejectedError{Reason: "permanent"}
	}
	cache, report, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, Refresh: true, ValidateThumbnail: rejecter})
	require.NoError(t, err, "permanent rejection is not a publish-blocking failure")
	assert.Empty(t, cache.Records)
	assert.Equal(t, 1, report.Rejected)
	assert.Equal(t, 0, report.Cached, "a refresh-rejected reused entry is not a reuse survivor")

	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "\"status\":\"rejected\"")
}

func TestBuildRevalidatesRejectedStateOnResume(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "reject"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	_, firstReport, err := Build(context.Background(), BuildOptions{
		Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator,
	})
	require.NoError(t, err)
	_, secondReport, err := Build(context.Background(), BuildOptions{
		Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, firstReport.Rejected)
	// Rejected entries are RE-VALIDATED on resume: a corrected URL at the
	// same identity key must get a chance, not be suppressed forever.
	assert.Equal(t, 1, secondReport.Candidates, "rejected entries re-enter validation")
	assert.Equal(t, 1, secondReport.Rejected, "still rejected: same invalid thumbnail")
	assert.Equal(t, 2, source.emitted, "the candidate is emitted on both runs")
}

func TestBuildKeepsRejectedCandidatesOutOfCache(t *testing.T) {
	source := &testSource{
		name:       "test",
		candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "reject"}},
	}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	cache, report, err := Build(context.Background(), BuildOptions{
		Registry:          registry,
		Sources:           []string{"test"},
		StatePath:         statePath,
		ValidateThumbnail: testValidator,
	})
	require.NoError(t, err)
	assert.Empty(t, cache.Records)
	assert.Equal(t, 1, report.Rejected)
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"status":"rejected"`)
}

func TestBuildLimitedEnumerationDoesNotPruneCachedEntries(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{
		{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "一", ThumbURL: "https://example.test/one.jpg"},
		{Key: "test:2", Source: "test", SourceID: "2", JapaneseName: "二", ThumbURL: "https://example.test/two.jpg"},
	}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	source.candidates = source.candidates[:1]
	cache, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, SourceOptions: SourceOptions{Limit: 1}, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	assert.Len(t, cache.Records, 2)
}

func TestBuildPrunesMissingSourceEntries(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{
		{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "一", ThumbURL: "https://example.test/one.jpg"},
		{Key: "test:2", Source: "test", SourceID: "2", JapaneseName: "二", ThumbURL: "https://example.test/two.jpg"},
	}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	source.candidates = source.candidates[:1]
	cache, report, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	assert.Len(t, cache.Records, 1)
	assert.Equal(t, 1, report.Cached)

	// Build carries prune decisions; the caller commits them post-publish.
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\"status\":\"stale\"", "journal untouched until publish")
	require.Equal(t, []string{"test:2"}, report.StaleKeys)
	require.NoError(t, JournalStale(statePath, report.StaleKeys))
	data, err = os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "\"status\":\"stale\"")
}

func TestBuildRefreshFailureRemovesStaleCandidate(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	fail := false
	validator := func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
		if fail {
			return ThumbnailValidation{}, errors.New("temporary failure")
		}
		return testValidator(ctx, candidate)
	}
	cache, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: validator})
	require.NoError(t, err)
	assert.Len(t, cache.Records, 1)
	fail = true
	cache, report, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, Refresh: true, ValidateThumbnail: validator})
	require.Error(t, err)
	assert.Empty(t, cache.Records)
	assert.Equal(t, 1, report.Failed)
	assert.Contains(t, err.Error(), "refusing to publish")

	// The aborted refresh must not poison the journal: the last-good entry
	// stays effective (no "failed" line appended for the key).
	data, readErr := os.ReadFile(statePath)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "\"status\":\"failed\"", "transient refresh failure must not overwrite last-good state")

	// A later default build therefore resumes from the cached validation
	// instead of refetching the thumbnail.
	validated := 0
	countingValidator := func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
		validated++
		return testValidator(ctx, candidate)
	}
	cache, report, err = Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: countingValidator})
	require.NoError(t, err)
	assert.Len(t, cache.Records, 1)
	assert.Equal(t, 1, report.Cached, "transient refresh failure must not destroy reusability")
	assert.Zero(t, validated, "no revalidation needed: last-good state was preserved")
}
func TestBuildValidatesOptions(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test", func() Source { return &testSource{name: "test"} })
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"missing"}, ValidateThumbnail: testValidator})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown actress cache source")
	_, _, err = Build(context.Background(), BuildOptions{Registry: registry, Sources: nil, ValidateThumbnail: testValidator})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one actress cache source")
}

func TestReadStateIgnoresInterruptedFinalLine(t *testing.T) {
	entries := make(map[string]StateEntry)
	data := `{"key":"test:1","status":"ok"}
{"key":"interrupted"`
	require.NoError(t, readState(strings.NewReader(data), entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "ok", entries["test:1"].Status)
}

func TestWriteFileAtomicallyWritesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache.json")
	cache := Cache{SchemaVersion: 1, Sources: []string{"test"}, Records: []Record{{BuiltinKey: "actress:dmm:1"}}}
	require.NoError(t, WriteFile(path, cache))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"schema_version": 1`)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	}
	_, err = os.Stat(path + ".tmp")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestBuildDoesNotMergeDifferentDMMIDsBySharedName(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{
		{Key: "test:1", Source: "test", SourceID: "1", DMMID: 1, FirstName: "Same", LastName: "Name", JapaneseName: "同名", ThumbURL: "thumb-1"},
		{Key: "test:2", Source: "test", SourceID: "2", DMMID: 2, FirstName: "Same", LastName: "Name", JapaneseName: "同名", ThumbURL: "thumb-2"},
	}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	cache, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	require.Len(t, cache.Records, 2)
}

func TestBuildPreservesAlternateJapaneseNamesAsAliases(t *testing.T) {
	high := &testSource{name: "high", candidates: []Candidate{{Key: "high:1", Source: "high", SourceID: "1", DMMID: 1, JapaneseName: "現在名", ThumbURL: "thumb"}}}
	low := &testSource{name: "low", candidates: []Candidate{{Key: "low:1", Source: "low", SourceID: "1", DMMID: 1, JapaneseName: "旧名", ThumbURL: "thumb"}}}
	registry := NewRegistry()
	registry.Register("high", func() Source { return high })
	registry.Register("low", func() Source { return low })
	cache, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"high", "low"}, ValidateThumbnail: testValidator})
	require.NoError(t, err)
	require.Len(t, cache.Records, 1)
	assert.Equal(t, []string{"旧名"}, cache.Records[0].Aliases)
}

func TestBuildRecordsTransientValidationFailureForRetry(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	cache, report, err := Build(context.Background(), BuildOptions{
		Registry: registry, Sources: []string{"test"}, StatePath: filepath.Join(t.TempDir(), "state.jsonl"),
		ValidateThumbnail: func(context.Context, Candidate) (ThumbnailValidation, error) {
			return ThumbnailValidation{}, errors.New("timeout")
		},
	})
	require.NoError(t, err)
	assert.Empty(t, cache.Records)
	assert.Equal(t, 0, report.Rejected)
	assert.Equal(t, 1, report.Failed)
}

func TestRegistryAndBuilderEdgeCases(t *testing.T) {
	var nilRegistry *Registry
	assert.Nil(t, nilRegistry.Names())
	_, ok := nilRegistry.Create("missing")
	assert.False(t, ok)
	registry := NewRegistry()
	registry.Register("", func() Source { return nil })
	registry.Register("test", func() Source { return &testSource{name: "test"} })
	assert.Equal(t, []string{"test"}, registry.Names())
	_, _, err := Build(context.Background(), BuildOptions{Sources: []string{"test"}})
	require.ErrorContains(t, err, "registry is required")
	_, _, err = Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}})
	require.ErrorContains(t, err, "fetcher is required")
	_, _, err = Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test", "test"}, ValidateThumbnail: testValidator})
	require.ErrorContains(t, err, "selected more than once")
}

func TestWriteFileRejectsEmptyPath(t *testing.T) {
	assert.Error(t, WriteFile("", Cache{}))
}

func TestBuiltinKeyUsesSourceFallback(t *testing.T) {
	record := Record{}
	assert.Equal(t, "actress:source:123", builtinKey(record, Candidate{Source: "Source", SourceID: "123"}))
}

func TestMergeJpOnlyRecordIntoDmmBackedRecordBySharedAlias(t *testing.T) {
	items := []rankedCandidate{
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "minnanoav:1069632", Source: "minnanoav", DMMID: 1069632, JapaneseName: "miru", Aliases: []string{"坂道みる"}, ThumbURL: "https://example.test/miru.jpg"}}},
		{rank: 1, candidate: ValidatedCandidate{Candidate: Candidate{Key: "legacy:坂道みる", Source: "legacy-jvthumbs", JapaneseName: "坂道みる", FirstName: "Miru", LastName: "Sakamichi", ThumbURL: "https://example.test/sakamichi.jpg"}}},
	}
	records := mergeCandidates(items)
	require.Len(t, records, 1)
	assert.Equal(t, 1069632, records[0].DMMID)
	assert.Equal(t, "坂道みる", records[0].JapaneseName)
	assert.Contains(t, records[0].Aliases, "miru")
}

func TestMergeJpOnlyCandidateProcessedBeforeDmmRecords(t *testing.T) {
	items := []rankedCandidate{
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "legacy:坂道みる", Source: "legacy-jvthumbs", JapaneseName: "坂道みる", FirstName: "Miru", LastName: "Sakamichi", ThumbURL: "https://example.test/sakamichi.jpg"}}},
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "minnanoav:1069632", Source: "minnanoav", DMMID: 1069632, JapaneseName: "miru", Aliases: []string{"坂道みる"}, ThumbURL: "https://example.test/miru.jpg"}}},
	}
	records := mergeCandidates(items)
	require.Len(t, records, 1)
	assert.Equal(t, 1069632, records[0].DMMID)
	assert.Equal(t, "坂道みる", records[0].JapaneseName)
}

func TestMergeDoesNotBridgeJpOnlyWhenAliasSharedByMultipleDmmRecords(t *testing.T) {
	items := []rankedCandidate{
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "a:1", Source: "minnanoav", DMMID: 1, JapaneseName: "一人目", Aliases: []string{"共通名"}, ThumbURL: "https://example.test/1.jpg"}}},
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "a:2", Source: "minnanoav", DMMID: 2, JapaneseName: "二人目", Aliases: []string{"共通名"}, ThumbURL: "https://example.test/2.jpg"}}},
		{rank: 1, candidate: ValidatedCandidate{Candidate: Candidate{Key: "b:1", Source: "legacy-jvthumbs", JapaneseName: "共通名", ThumbURL: "https://example.test/3.jpg"}}},
	}
	records := mergeCandidates(items)
	require.Len(t, records, 3)
}

func TestMergeAvoidsAmbiguousNoDMMBridge(t *testing.T) {
	items := []rankedCandidate{
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "one", Source: "a", DMMID: 1, JapaneseName: "同名", ThumbURL: "https://example.test/one.jpg"}}},
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "two", Source: "a", DMMID: 2, JapaneseName: "同名", ThumbURL: "https://example.test/two.jpg"}}},
		{rank: 1, candidate: ValidatedCandidate{Candidate: Candidate{Key: "unknown", Source: "b", JapaneseName: "同名", ThumbURL: "unknown"}}},
	}
	records := mergeCandidates(items)
	require.Len(t, records, 3)
}

func TestMergeDoesNotCollapseLegacyRowsWithSameEnglishName(t *testing.T) {
	items := []rankedCandidate{
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "one", Source: "legacy", SourceID: "one", FirstName: "Aika", JapaneseName: "AIKA", ThumbURL: "https://example.test/one.jpg"}}},
		{rank: 0, candidate: ValidatedCandidate{Candidate: Candidate{Key: "two", Source: "legacy", SourceID: "two", FirstName: "Aika", JapaneseName: "藍花", ThumbURL: "https://example.test/two.jpg"}}},
	}
	records := mergeCandidates(items)
	require.Len(t, records, 2)
}

func TestBuildPrunesPolicyExcludedStateEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	gone := Candidate{Key: "test:gone", Source: "test", DMMID: 77, JapaneseName: "消える子", FirstName: "Kieru", LastName: "Ko", ThumbURL: "https://cdn.test/gone.jpg"}
	stay := Candidate{Key: "test:stay", Source: "test", DMMID: 78, JapaneseName: "残る子", FirstName: "Nokoru", LastName: "Ko", ThumbURL: "https://cdn.test/stay.jpg"}
	source := &testSource{name: "test", candidates: []Candidate{gone, stay}}
	options := BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator}

	// Run 1: default policy publishes both entries.
	cache, _, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Len(t, cache.Records, 2)

	// Run 2: stricter policy (100x100 thumbs now too small) and "gone" is no
	// longer enumerated upstream. "gone" never enters the survivor set, so a
	// candidates-only prune misses it; the state sweep must stale it.
	source.candidates = []Candidate{stay}
	options.MinThumbnailDimension = 150
	_, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	require.Equal(t, []string{"test:gone"}, report.StaleKeys)

	// The CLI commits prune lines only post-publish; mirror that ordering.
	require.NoError(t, JournalStale(statePath, report.StaleKeys))

	// Run 3: relaxed policy with a limit (pruning disabled). The removed,
	// policy-excluded record must stay dead -- no resurrection from a stale
	// last-good journal line.
	source.candidates = nil
	options.MinThumbnailDimension = 0
	options.SourceOptions.Limit = 5
	cache, report, err = Build(context.Background(), options)
	require.NoError(t, err)
	for _, record := range cache.Records {
		assert.NotEqual(t, 77, record.DMMID, "removed record must not be resurrected")
	}
	assert.Equal(t, 1, report.Cached, "only the surviving reusable entry may count as cached")
}

func TestBuildStaleKeysDedupForReusedEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	gone := Candidate{Key: "test:gone", Source: "test", DMMID: 177, JapaneseName: "消える子", FirstName: "Kieru", LastName: "Ko", ThumbURL: "https://cdn.test/gone.jpg"}
	stay := Candidate{Key: "test:stay", Source: "test", DMMID: 178, JapaneseName: "残る子", FirstName: "Nokoru", LastName: "Ko", ThumbURL: "https://cdn.test/stay.jpg"}
	source := &testSource{name: "test", candidates: []Candidate{gone, stay}}
	options := BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator}
	_, _, err := Build(context.Background(), options)
	require.NoError(t, err)

	// Same policy: "gone" is reused into candidates and reaches pruning via
	// the candidate pass AND the state sweep -- but must be staled exactly once.
	source.candidates = []Candidate{stay}
	_, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, []string{"test:gone"}, report.StaleKeys, "candidate+state sweep must dedup stale keys")
}

func TestBuildPruneSweepSkipsIneligibleEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	main := &testSource{name: "test", candidates: []Candidate{{Key: "test:gone", Source: "test", DMMID: 277, ThumbURL: "https://cdn.test/gone.jpg"}}}
	other := &testSource{name: "other", candidates: []Candidate{{Key: "other:keep", Source: "other", DMMID: 308, ThumbURL: "https://cdn.test/keep.jpg"}}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return main })
	registry.Register("other", func() Source { return other })
	options := BuildOptions{Registry: registry, Sources: []string{"test", "other"}, StatePath: statePath, ValidateThumbnail: testValidator}
	_, _, err := Build(context.Background(), options)
	require.NoError(t, err)

	// Tombstone "test:gone" before the sweep runs, then complete both sources
	// without re-listing it: the sweep must skip the non-ok entry, skip OK
	// entries owned by another source, and skip seen (reused) entries.
	require.NoError(t, JournalStale(statePath, []string{"test:gone"}))
	main.candidates = nil
	_, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Empty(t, report.StaleKeys, "tombstoned/seen/cross-source entries are not newly staled")
}

// Cancellation arriving while validation is in flight (after enumeration
// finished) must abort the build instead of publishing a partial cache:
// built-in sources can convert context.Canceled into per-candidate failures
// and still return nil from Collect.
func TestBuildAbortsWhenContextCanceledMidValidation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	source := &testSource{name: "test", candidates: []Candidate{
		{Key: "test:a", Source: "test", DMMID: 501, ThumbURL: "https://cdn.test/a.jpg"},
		{Key: "test:b", Source: "test", DMMID: 502, ThumbURL: "https://cdn.test/b.jpg"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	validator := func(_ context.Context, c Candidate) (ThumbnailValidation, error) {
		once.Do(func() { cancel() }) // outer cancellation mid-flight
		return ThumbnailValidation{CheckedAt: "now", SHA256: c.Key, Bytes: 1024, Width: 100, Height: 100}, nil
	}
	_, _, err := Build(ctx, BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: validator})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// Sentinel discipline: explicit --min-dimension 0 must reach validation as
// "disabled" (no minimum), while the omitted flag still lands at 64.
func TestBuildExplicitZeroMinDimensionDisablesThreshold(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	row := Candidate{Key: "test:tiny", Source: "test", DMMID: 331, JapaneseName: "小畑", FirstName: "Kohata", LastName: "Obata", ThumbURL: "https://cdn.test/tiny.jpg"}
	smallValidator := func(_ context.Context, c Candidate) (ThumbnailValidation, error) {
		return ThumbnailValidation{CheckedAt: "now", SHA256: c.Key, Bytes: 512, Width: 32, Height: 32, Format: "jpeg"}, nil
	}
	source := &testSource{name: "test", candidates: []Candidate{row}}
	options := BuildOptions{Registry: registryWith(source), Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: smallValidator}
	_, _, err := Build(context.Background(), options)
	require.NoError(t, err)

	// Explicit 0 disables the minimum: the 32x32 thumb stays reusable (no fetch).
	calls := 0
	counting := func(ctx context.Context, c Candidate) (ThumbnailValidation, error) {
		calls++
		return smallValidator(ctx, c)
	}
	options.MinThumbnailDimension = 0
	options.ValidateThumbnail = counting
	_, report, err := Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 0, calls, "explicit zero keeps the thumb reusable")
	assert.Equal(t, 1, report.Cached)

	// An explicit positive threshold forces revalidation.
	options.MinThumbnailDimension = 40
	_, _, err = Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "tightening revalidates")

	// Omitted (sentinel) maps to the 64 default.
	options.MinThumbnailDimension = -1
	options.MinThumbnailDimension = -1 // double-set mirrors the parse sentinel
	_, _, err = Build(context.Background(), options)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "omitted => 64 default revalidates 32px thumbs")
}
