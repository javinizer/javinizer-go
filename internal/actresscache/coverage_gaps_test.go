package actresscache_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func TestBuildBudgetAndClaimFailures(t *testing.T) {
	headerBytes := bytes.IndexByte(build(t, evidence()).Bytes(), '\n') + 1
	cases := []struct {
		name string
		edit func([]actresscache.Evidence, *actresscache.Limits)
	}{
		{"invalid limits", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxBytes = 0 }},
		{"record count", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxRecords = 1 }},
		{"encoded header", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxBytes = 2 }},
		{"header line", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxRecordBytes = 1 }},
		{"header string", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxStringBytes = 1 }},
		{"no record space", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxBytes = int64(headerBytes) }},
		{"record token budget", func(_ []actresscache.Evidence, l *actresscache.Limits) { l.MaxTokensPerRecord = 10 }},
		{"invalid timestamp year", func(r []actresscache.Evidence, _ *actresscache.Limits) {
			r[0].Provenance.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		}},
		{"oversized alias", func(r []actresscache.Evidence, l *actresscache.Limits) {
			l.MaxStringBytes = 64
			r[0].Metadata.Aliases = []string{strings.Repeat("a", 65)}
		}},
		{"invalid external namespace", func(r []actresscache.Evidence, _ *actresscache.Limits) { r[0].ExternalIDs[0].Namespace = "DMM" }},
		{"empty alias", func(r []actresscache.Evidence, _ *actresscache.Limits) { r[0].Metadata.Aliases = []string{" \t"} }},
		{"no claims", func(r []actresscache.Evidence, _ *actresscache.Limits) {
			r[0].Metadata = actresscache.Metadata{}
			r[0].ExternalIDs = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := evidence()
			l := limits()
			tc.edit(records, &l)
			if _, err := actresscache.Build(t.Context(), records, l); err == nil {
				t.Fatal("invalid build accepted")
			}
		})
	}
	for _, extra := range []int{0, len("other")} {
		record := evidence()[0]
		claims := record.ExternalIDs
		record.ExternalIDs = nil
		base := build(t, []actresscache.Evidence{record})
		record.ExternalIDs = claims
		l := limits()
		l.MaxTotalStringBytes = decodedStringBytes(t, base.Bytes()) + int64(extra)
		if _, err := actresscache.Build(t.Context(), []actresscache.Evidence{record}, l); err == nil {
			t.Fatal("external claim exceeded remaining string budget")
		}
	}
	if _, err := actresscache.SourceKey("fixture.name", "id"); err == nil {
		t.Fatal("invalid namespace punctuation accepted")
	}
	snapshot := build(t, evidence())
	if _, ok := snapshot.Get("v1:fixture:missing"); ok {
		t.Fatal("missing source key returned evidence")
	}
}

func decodedStringBytes(t *testing.T, data []byte) int64 {
	t.Helper()
	var total int64
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case string:
			total += int64(len(v))
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
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		visit(value)
	}
	return total
}

func TestDecodeTypedErrorsAndReadBoundaries(t *testing.T) {
	valid := build(t, evidence()).Bytes()
	if _, err := actresscache.Decode(t.Context(), nil, limits()); err == nil {
		t.Fatal("nil reader accepted")
	}
	for _, tc := range []struct {
		name             string
		old, replacement string
	}{
		{"header unknown location", `"record_count":2`, `"source":"unexpected","record_count":2`},
		{"fractional count", `"record_count":2`, `"record_count":2.5`},
		{"record unknown location", `"source_id":"actor-1"`, `"format":"unexpected","source_id":"actor-1"`},
		{"malformed date", `"observed_at":"2026-01-02T03:04:05Z"`, `"observed_at":"not-a-date"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Replace(valid, []byte(tc.old), []byte(tc.replacement), 1)
			if bytes.Equal(data, valid) {
				t.Fatal("mutation did not apply")
			}
			if _, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits()); err == nil {
				t.Fatal("typed decoding error ignored")
			}
		})
	}
	record := evidence()[0]
	record.Metadata.FirstName = strings.Repeat("é", 3000)
	l := limits()
	l.MaxStringBytes = 8000
	large, err := actresscache.Build(t.Context(), []actresscache.Evidence{record}, l)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := actresscache.Decode(t.Context(), bytes.NewReader(large.Bytes()), l)
	if err != nil || !bytes.Equal(decoded.Bytes(), large.Bytes()) {
		t.Fatalf("long line round trip: %v", err)
	}
	l = limits()
	l.MaxBytes = int64(len(valid))
	appended := io.MultiReader(bytes.NewReader(valid), strings.NewReader("x"))
	if _, err := actresscache.Decode(t.Context(), appended, l); err == nil {
		t.Fatal("late appended byte exceeded budget without rejection")
	}
	sentinel := errors.New("late source read failure")
	lateError := io.MultiReader(bytes.NewReader(valid), readerFunc(func([]byte) (int, error) { return 0, sentinel }))
	if _, err := actresscache.Decode(t.Context(), lateError, limits()); !errors.Is(err, sentinel) {
		t.Fatalf("lost late read error: %v", err)
	}
}

func TestMalformedJSONAndUnicodeBoundaries(t *testing.T) {
	inputs := []string{
		`{"format":"x\`,
		`{"format":"\u12`,
		`{"format":"\u00xy"}`,
		`{"format":"\ud800\u0041"}`,
		`{"format":"\ud800\u00xy"}`,
		`{"format":"x",7:0}`,
		`{"aliases":["a"}`,
		`{} {}`,
		`{}`,
		strings.Repeat(`{"metadata":`, 10) + `{}` + strings.Repeat("}", 10),
	}
	for _, input := range inputs {
		if _, err := actresscache.Decode(t.Context(), strings.NewReader(input+"\n"), limits()); err == nil {
			t.Fatalf("malformed input accepted: %q", input)
		}
	}
}
