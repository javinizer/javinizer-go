package actresscache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

type countedInput struct {
	input io.Reader
	bytes int64
}

func (r *countedInput) Read(data []byte) (int, error) {
	n, err := r.input.Read(data)
	r.bytes += int64(n)
	return n, err
}

func TestReaderConsumptionIsBounded(t *testing.T) {
	valid := build(t, evidence()).Bytes()
	huge := append(append([]byte(nil), valid...), bytes.Repeat([]byte{' '}, 1<<20)...)
	l := limits()
	l.MaxBytes = int64(len(valid) - 1)
	input := &countedInput{input: bytes.NewReader(huge)}
	if _, err := actresscache.Decode(t.Context(), input, l); err == nil {
		t.Fatal("oversized input accepted")
	}
	if input.bytes > l.MaxBytes+1 {
		t.Fatalf("read beyond encoded budget: %d", input.bytes)
	}
	l = limits()
	l.MaxRecordBytes = 128
	l.MaxBytes = 4096
	input = &countedInput{input: strings.NewReader(strings.Repeat("x", 1<<20) + "\n")}
	if _, err := actresscache.Decode(t.Context(), input, l); err == nil {
		t.Fatal("oversized line accepted")
	}
	if input.bytes > l.MaxBytes+1 {
		t.Fatal("oversized line consumed unbounded input")
	}
	sentinel := errors.New("source read failure")
	if _, err := actresscache.Decode(t.Context(), readerFunc(func([]byte) (int, error) { return 0, sentinel }), limits()); !errors.Is(err, sentinel) {
		t.Fatalf("lost input error: %v", err)
	}
	l = limits()
	l.MaxBytes = math.MaxInt64
	if _, err := actresscache.Decode(t.Context(), nil, l); err == nil {
		t.Fatal("overflow-prone byte budget accepted")
	}
}

func TestEscapesAndNestedMalformedInput(t *testing.T) {
	valid := build(t, evidence()).Bytes()
	escapedDuplicate := bytes.Replace(valid, []byte(`"source_id":"actor-1"`), []byte(`"source\u005fid":"other","source_id":"actor-1"`), 1)
	if _, err := actresscache.Decode(t.Context(), bytes.NewReader(escapedDuplicate), limits()); err == nil {
		t.Fatal("escaped duplicate key accepted")
	}
	paired := bytes.Replace(valid, []byte(`"actor-1"`), []byte(`"\ud83d\ude00"`), 1)
	snapshot, err := actresscache.Decode(t.Context(), bytes.NewReader(paired), limits())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := actresscache.SourceKey("fixture_a", "😀")
	record, ok := snapshot.Get(key)
	if !ok || record.SourceID != "😀" {
		t.Fatal("valid surrogate pair changed source ID")
	}
	nested := bytes.Replace(valid, []byte(`["Alias A","Alias B"]`), []byte(strings.Repeat("[", 64)+`"Alias A"`+strings.Repeat("]", 64)), 1)
	if bytes.Equal(nested, valid) {
		t.Fatal("nested fixture mutation did not apply")
	}
	if _, err := actresscache.Decode(t.Context(), bytes.NewReader(nested), limits()); err == nil {
		t.Fatal("deep malformed alias arrays accepted")
	}
}

func TestJSONEscapePreflightMatchesWireEncoding(t *testing.T) {
	values := []string{"<>&\u2028\u2029😀", "quote\" slash\\ newline\n", "literal \\ud800 escaped text"}
	for r := rune(0); r < 128; r++ {
		values = append(values, string(r)+"claim")
	}
	for _, value := range values {
		records := evidence()
		records[0].Metadata.FirstName = value
		snapshot, err := actresscache.Build(t.Context(), records, limits())
		if err != nil {
			t.Fatalf("preflight rejected %q: %v", value, err)
		}
		decoded, err := actresscache.Decode(t.Context(), bytes.NewReader(snapshot.Bytes()), limits())
		if err != nil {
			t.Fatalf("round trip %q: %v", value, err)
		}
		key, _ := actresscache.SourceKey("fixture_a", "actor-1")
		record, _ := decoded.Get(key)
		if record.Metadata.FirstName != value {
			t.Fatalf("source claim changed: %q -> %q", value, record.Metadata.FirstName)
		}
	}
}

func TestTypedBuilderRejectsBeforeBulkWork(t *testing.T) {
	records := evidence()[:1]
	records[0].Metadata.Aliases = make([]string, 1<<18)
	l := limits()
	l.MaxBytes = 1024
	l.MaxRecordBytes = 1024
	l.MaxAliases = math.MaxInt
	if _, err := actresscache.Build(t.Context(), records, l); err == nil {
		t.Fatal("oversized typed collection accepted")
	}
	records = evidence()
	l = limits()
	l.MaxTotalStringBytes = 32
	if _, err := actresscache.Build(t.Context(), records, l); err == nil {
		t.Fatal("tiny aggregate string budget ignored")
	}
	records = evidence()
	records[0].Metadata.FirstName = strings.Repeat("&", 2048)
	l = limits()
	l.MaxRecordBytes = 4096
	if _, err := actresscache.Build(t.Context(), records, l); err == nil {
		t.Fatal("JSON escape expansion ignored by record budget")
	}
}

func TestEmptySnapshotAndCanceledConstruction(t *testing.T) {
	snapshot, err := actresscache.Build(t.Context(), nil, limits())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := actresscache.Decode(t.Context(), bytes.NewReader(snapshot.Bytes()), limits())
	if err != nil || len(decoded.Records()) != 0 {
		t.Fatalf("explicit empty snapshot: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := actresscache.Build(ctx, evidence(), limits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled build: %v", err)
	}
	reads := 0
	if _, err := actresscache.Decode(ctx, readerFunc(func([]byte) (int, error) { reads++; return 0, io.EOF }), limits()); !errors.Is(err, context.Canceled) || reads != 0 {
		t.Fatalf("canceled decode err=%v reads=%d", err, reads)
	}
}
