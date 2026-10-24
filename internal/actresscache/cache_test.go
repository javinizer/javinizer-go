package actresscache_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func limits() actresscache.Limits {
	return actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
}

func evidence() []actresscache.Evidence {
	digest := sha256.Sum256([]byte("fixture dataset v1\n"))
	provenance := actresscache.Provenance{SourceURL: "https://fixtures.invalid/source", DatasetSHA256: hex.EncodeToString(digest[:]), ParserRevision: "fixture-v1", ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	return []actresscache.Evidence{
		{Source: "fixture_a", SourceID: "actor-1", Provenance: provenance, Metadata: actresscache.Metadata{FirstName: "Alpha", JapaneseName: "甲", Aliases: []string{"Alias B", "Alias A"}}, ExternalIDs: []actresscache.ExternalID{{Namespace: "other", Value: "01"}, {Namespace: "dmm", Value: "7"}}},
		{Source: "fixture_b", SourceID: "actor-2", Provenance: provenance, Metadata: actresscache.Metadata{FirstName: "Different", JapaneseName: "乙", Aliases: []string{"Alias C"}}, ExternalIDs: []actresscache.ExternalID{{Namespace: "dmm", Value: "7"}}},
	}
}

func build(t *testing.T, records []actresscache.Evidence) *actresscache.Snapshot {
	t.Helper()
	snapshot, err := actresscache.Build(t.Context(), records, limits())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestStableKeysAndIndependentAmbiguousClaims(t *testing.T) {
	records := evidence()
	snapshot := build(t, records)
	key, err := actresscache.SourceKey("fixture_a", "actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if key != "v1:fixture_a:YWN0b3ItMQ" {
		t.Fatalf("unexpected stable key: %s", key)
	}
	result, err := snapshot.Lookup("dmm", "7")
	if err != nil || !result.Ambiguous || len(result.Candidates) != 2 {
		t.Fatalf("ambiguity lost: %+v %v", result, err)
	}
	if result.Candidates[0].Metadata.FirstName != "Alpha" || result.Candidates[1].Metadata.FirstName != "Different" {
		t.Fatal("source records merged or enriched")
	}
	for _, query := range []string{"07", "Alpha", "missing"} {
		result, err := snapshot.Lookup("dmm", query)
		if err != nil || len(result.Candidates) != 0 || result.Ambiguous {
			t.Fatalf("invented fallback for %q: %+v %v", query, result, err)
		}
	}
	records[0].Metadata.FirstName = "Renamed"
	records[0].Metadata.ThumbnailURL = "https://fixtures.invalid/new.jpg"
	records[0].Provenance.ObservedAt = records[0].Provenance.ObservedAt.Add(time.Hour)
	updated := build(t, records)
	record, ok := updated.Get(key)
	if !ok || record.Metadata.FirstName != "Renamed" {
		t.Fatal("mutable data changed source key")
	}
	original, _ := snapshot.Get(key)
	if original.Metadata.FirstName != "Alpha" || original.Metadata.ThumbnailURL != "" {
		t.Fatal("old snapshot mutated")
	}
	if snapshot.Digest() == updated.Digest() {
		t.Fatal("different evidence kept artifact digest")
	}
	if _, err := snapshot.Lookup("DMM", "7"); err == nil {
		t.Fatal("accepted noncanonical namespace")
	}
}

func TestDeterministicEncodingAndFixture(t *testing.T) {
	records := evidence()
	a := build(t, records)
	records[0].Metadata.Aliases[0], records[0].Metadata.Aliases[1] = records[0].Metadata.Aliases[1], records[0].Metadata.Aliases[0]
	records[0].ExternalIDs[0], records[0].ExternalIDs[1] = records[0].ExternalIDs[1], records[0].ExternalIDs[0]
	records[0].Provenance.ObservedAt = records[0].Provenance.ObservedAt.In(time.FixedZone("offset", 9*3600))
	records[0], records[1] = records[1], records[0]
	b := build(t, records)
	if !bytes.Equal(a.Bytes(), b.Bytes()) || a.Digest() != b.Digest() {
		t.Fatal("ordering or timezone changed deterministic encoding")
	}
	data, err := os.ReadFile("testdata/evidence-v1.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), data) {
		t.Fatalf("fixture differs from v1 encoding:\n%s", a.Bytes())
	}
	decoded, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Bytes(), data) {
		t.Fatal("fixture round trip changed encoding")
	}
	source, err := os.ReadFile("testdata/source.txt")
	if err != nil {
		t.Fatal(err)
	}
	upstream := sha256.Sum256(source)
	for _, record := range decoded.Records() {
		if record.Provenance.DatasetSHA256 != hex.EncodeToString(upstream[:]) {
			t.Fatal("fixture upstream digest mismatch")
		}
	}
	digest := sha256.Sum256(data)
	if decoded.Digest() != hex.EncodeToString(digest[:]) {
		t.Fatal("artifact digest mismatch")
	}
	if decoded.Digest() == records[0].Provenance.DatasetSHA256 {
		t.Fatal("artifact and upstream hashes conflated")
	}
}

func TestSnapshotOwnsInputsAndOutputs(t *testing.T) {
	input := evidence()
	snapshot := build(t, input)
	key, _ := actresscache.SourceKey("fixture_a", "actor-1")
	input[0].Metadata.Aliases[0] = "caller edit"
	input[0].ExternalIDs[0].Value = "caller edit"
	all := snapshot.Records()
	all[0].Metadata.Aliases[0] = "returned edit"
	all[0].ExternalIDs[0].Value = "returned edit"
	got, _ := snapshot.Get(key)
	got.Metadata.Aliases[0] = "get edit"
	result, _ := snapshot.Lookup("dmm", "7")
	result.Candidates[0].Metadata.Aliases[0] = "lookup edit"
	result.Candidates[0].ExternalIDs[0].Value = "lookup edit"
	data := snapshot.Bytes()
	data[0] = '!'
	pristine := build(t, evidence())
	if !reflect.DeepEqual(snapshot.Records(), pristine.Records()) || !bytes.Equal(snapshot.Bytes(), pristine.Bytes()) {
		t.Fatal("mutable output escaped snapshot")
	}
}

func TestRejectConflictingKeysAndInvalidEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]actresscache.Evidence) []actresscache.Evidence
	}{
		{"duplicate source key", func(r []actresscache.Evidence) []actresscache.Evidence { return append(r, r[0]) }},
		{"empty source ID", func(r []actresscache.Evidence) []actresscache.Evidence { r[0].SourceID = ""; return r }},
		{"positional whitespace ID", func(r []actresscache.Evidence) []actresscache.Evidence { r[0].SourceID = " actor-1"; return r }},
		{"invalid source namespace", func(r []actresscache.Evidence) []actresscache.Evidence { r[0].Source = "Fixture"; return r }},
		{"invalid UTF8", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Metadata.FirstName = string([]byte{0xff})
			return r
		}},
		{"missing provenance", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Provenance = actresscache.Provenance{}
			return r
		}},
		{"uppercase dataset digest", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Provenance.DatasetSHA256 = strings.Repeat("A", 64)
			return r
		}},
		{"URL credentials", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Provenance.SourceURL = "https://user:secret@fixtures.invalid/"
			return r
		}},
		{"nonHTTP image reference", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Metadata.ThumbnailURL = "file:///media/a.jpg"
			return r
		}},
		{"duplicate aliases", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].Metadata.Aliases = []string{"same", "same"}
			return r
		}},
		{"duplicate claims", func(r []actresscache.Evidence) []actresscache.Evidence {
			r[0].ExternalIDs = append(r[0].ExternalIDs, r[0].ExternalIDs[0])
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := actresscache.Build(t.Context(), tc.edit(evidence()), limits()); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	for _, id := range []string{"", "bad\nID", strings.Repeat("x", 257), "\xff"} {
		if _, err := actresscache.SourceKey("fixture", id); err == nil {
			t.Fatalf("accepted invalid source ID %q", id)
		}
	}
	a, _ := actresscache.SourceKey("fixture", "a:b/c")
	b, _ := actresscache.SourceKey("fixture", "a:b")
	if a == b {
		t.Fatal("opaque source IDs collided")
	}
}

func TestRejectCorruptAndAmbiguousWireInput(t *testing.T) {
	valid := build(t, evidence()).Bytes()
	replacements := map[string][2]string{
		"header duplicate":         {"\"record_count\":2", "\"record_count\":0,\"record_count\":2"},
		"record duplicate":         {"\"source_id\":\"actor-1\"", "\"source_id\":\"shadow\",\"source_id\":\"actor-1\""},
		"provenance duplicate":     {"\"parser_revision\":\"fixture-v1\"", "\"parser_revision\":\"shadow\",\"parser_revision\":\"fixture-v1\""},
		"metadata duplicate":       {"\"first_name\":\"Alpha\"", "\"first_name\":\"shadow\",\"first_name\":\"Alpha\""},
		"external claim duplicate": {"\"namespace\":\"dmm\"", "\"namespace\":\"other\",\"namespace\":\"dmm\""},
		"case folded key":          {"\"source_id\"", "\"Source_id\""},
		"unknown key":              {"\"source_id\"", "\"actor_id\""},
		"null metadata":            {"\"metadata\":{\"first_name\":\"Alpha\",\"japanese_name\":\"甲\",\"aliases\":[\"Alias A\",\"Alias B\"]}", "\"metadata\":null"},
		"null count":               {"\"record_count\":2", "\"record_count\":null"},
		"unsupported schema":       {"\"schema_version\":1", "\"schema_version\":2"},
		"missing declared record":  {"\"record_count\":2", "\"record_count\":3"},
		"extra record":             {"\"record_count\":2", "\"record_count\":1"},
		"missing count":            {"\"record_count\":2", "\"other_count\":2"},
		"unpaired high surrogate":  {"\"actor-1\"", "\"\\ud800\""},
		"unpaired low surrogate":   {"\"actor-1\"", "\"\\udc00\""},
	}
	for name, pair := range replacements {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Replace(valid, []byte(pair[0]), []byte(pair[1]), 1)
			if bytes.Equal(changed, valid) {
				t.Fatal("fixture mutation did not apply")
			}
			if _, err := actresscache.Decode(t.Context(), bytes.NewReader(changed), limits()); err == nil {
				t.Fatal("invalid wire input accepted")
			}
		})
	}
	for _, data := range [][]byte{nil, valid[:len(valid)-1], append(append([]byte(nil), valid...), '\n'), append(append([]byte(nil), valid...), '{', '}'), []byte("\x1f\x8bcompressed\n"), bytes.Replace(valid, []byte("Alpha"), []byte{0xff}, 1)} {
		if _, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits()); err == nil {
			t.Fatalf("accepted malformed input of %d bytes", len(data))
		}
	}
}

func TestQuotaBoundaries(t *testing.T) {
	data := build(t, evidence()).Bytes()
	longest := 0
	maxTokens := 0
	var totalStrings int64
	maxString := 0
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
		if len(line) > longest {
			longest = len(line)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		tokens := 0
		for {
			_, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			tokens++
		}
		if tokens > maxTokens {
			maxTokens = tokens
		}
		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		var visit func(any)
		visit = func(value any) {
			switch v := value.(type) {
			case string:
				totalStrings += int64(len(v))
				if len(v) > maxString {
					maxString = len(v)
				}
			case []any:
				for _, child := range v {
					visit(child)
				}
			case map[string]any:
				for _, child := range v {
					visit(child)
				}
			}
		}
		visit(value)
	}
	cases := []struct {
		name  string
		set   func(*actresscache.Limits, int)
		exact int
	}{
		{"encoded bytes", func(l *actresscache.Limits, n int) { l.MaxBytes = int64(n) }, len(data)},
		{"record bytes", func(l *actresscache.Limits, n int) { l.MaxRecordBytes = n }, longest},
		{"records", func(l *actresscache.Limits, n int) { l.MaxRecords = n }, 2},
		{"single string", func(l *actresscache.Limits, n int) { l.MaxStringBytes = n }, maxString},
		{"aggregate strings", func(l *actresscache.Limits, n int) { l.MaxTotalStringBytes = int64(n) }, int(totalStrings)},
		{"aliases", func(l *actresscache.Limits, n int) { l.MaxAliases = n }, 2},
		{"external IDs", func(l *actresscache.Limits, n int) { l.MaxExternalIDs = n }, 2},
		{"tokens", func(l *actresscache.Limits, n int) { l.MaxTokensPerRecord = n }, maxTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := limits()
			tc.set(&l, tc.exact)
			if _, err := actresscache.Decode(t.Context(), bytes.NewReader(data), l); err != nil {
				t.Fatalf("exact limit rejected: %v", err)
			}
			tc.set(&l, tc.exact-1)
			if _, err := actresscache.Decode(t.Context(), bytes.NewReader(data), l); err == nil {
				t.Fatal("limit+1 input accepted")
			}
			tc.set(&l, 0)
			if _, err := actresscache.Decode(t.Context(), bytes.NewReader(data), l); err == nil {
				t.Fatal("zero budget accepted")
			}
		})
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(data []byte) (int, error) { return f(data) }

func TestPublicationFailureCancellationAndBackpressure(t *testing.T) {
	var store actresscache.Store
	if store.Current() != nil {
		t.Fatal("initial store is not empty")
	}
	original := build(t, evidence())
	published, err := store.Replace(t.Context(), bytes.NewReader(original.Bytes()), limits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(t.Context(), strings.NewReader("corrupt\n"), limits()); err == nil || store.Current() != published {
		t.Fatal("invalid input replaced last good snapshot")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	noRead := readerFunc(func([]byte) (int, error) { t.Error("rejected input was read"); return 0, io.EOF })
	if _, err := store.Replace(ctx, noRead, limits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled input: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	input := readerFunc(func(data []byte) (int, error) { close(entered); <-release; return copy(data, original.Bytes()), io.EOF })
	go func() { _, err := store.Replace(ctx, input, limits()); done <- err }()
	<-entered
	if _, err := store.Replace(t.Context(), noRead, limits()); !errors.Is(err, actresscache.ErrBusy) {
		t.Errorf("expected writer backpressure, got %v", err)
	}
	cancel()
	unblock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation: %v", err)
	}
	if store.Current() != published {
		t.Fatal("canceled replacement changed snapshot")
	}
	updated := evidence()
	updated[0].Metadata.FirstName = "Updated"
	replacement := build(t, updated)
	if _, err := store.Replace(t.Context(), bytes.NewReader(replacement.Bytes()), limits()); err != nil {
		t.Fatal(err)
	}
	if published.Records()[0].Metadata.FirstName != "Alpha" || store.Current().Records()[0].Metadata.FirstName != "Updated" {
		t.Fatal("old snapshot lifetime or replacement broken")
	}
}

func TestConcurrentReadersKeepImmutableSnapshots(t *testing.T) {
	var store actresscache.Store
	original := build(t, evidence())
	updated := evidence()
	updated[0].Metadata.FirstName = "Updated"
	replacement := build(t, updated)
	if _, err := store.Replace(t.Context(), bytes.NewReader(original.Bytes()), limits()); err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Go(func() {
			for j := 0; j < 100; j++ {
				snapshot := store.Current()
				result, err := snapshot.Lookup("dmm", "7")
				if err != nil || !result.Ambiguous || len(result.Candidates) != 2 {
					t.Error("torn lookup")
				}
				result.Candidates[0].Metadata.Aliases[0] = "caller edit"
				if snapshot.Digest() != original.Digest() && snapshot.Digest() != replacement.Digest() {
					t.Error("unknown snapshot")
				}
			}
		})
	}
	for i := 0; i < 20; i++ {
		data := original.Bytes()
		if i%2 == 0 {
			data = replacement.Bytes()
		}
		if _, err := store.Replace(t.Context(), bytes.NewReader(data), limits()); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
}
