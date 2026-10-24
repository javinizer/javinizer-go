package actresscache

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCanceledContextReaderDoesNotReadInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := contextReader{ctx: ctx, reader: bytes.NewReader([]byte("unread"))}
	n, err := reader.Read(make([]byte, 4))
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read n=%d err=%v", n, err)
	}
}

func TestJSONSizeCanceledAndTwoByteUnicode(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	size := jsonSize{ctx: ctx, left: 100}
	size.add(1)
	if !errors.Is(size.err, context.Canceled) || size.left != 100 {
		t.Fatalf("canceled size accounting: %+v", size)
	}
	size = jsonSize{ctx: t.Context(), left: 100}
	size.text("é")
	if size.err != nil || size.left != 96 {
		t.Fatalf("UTF8 size accounting: %+v", size)
	}
}

func primitiveLimits() Limits {
	return Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
}

func primitiveSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	data, err := os.ReadFile("testdata/evidence-v1.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Decode(t.Context(), bytes.NewReader(data), primitiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestCancellationAtCachePhaseBoundaries(t *testing.T) {
	snapshot := primitiveSnapshot(t)
	record := snapshot.Records()[0]
	l := primitiveLimits()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var used int64
	if _, err := prepareRecord(ctx, record, l, int64(l.MaxRecordBytes), &used); !errors.Is(err, context.Canceled) || used != 0 {
		t.Fatalf("prepare cancellation: %v", err)
	}
	lines := bytes.Split(snapshot.Bytes(), []byte{'\n'})
	entry := keyedEvidence{record: record, encoded: lines[1]}
	for _, entries := range [][]keyedEvidence{nil, {entry}} {
		if _, err := assembleSnapshot(ctx, lines[0], entries); !errors.Is(err, context.Canceled) {
			t.Fatalf("assembly cancellation: %v", err)
		}
	}
	source := bytes.NewReader(snapshot.Bytes())
	counted := &countReader{reader: source}
	if _, err := readCacheLine(ctx, bufio.NewReader(counted), counted, l); !errors.Is(err, context.Canceled) || source.Len() != len(snapshot.Bytes()) {
		t.Fatalf("line cancellation consumed input: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(lines[1]))
	cursor := jsonTokens{ctx: ctx, decoder: decoder, limits: l, totalStrings: &used}
	if _, err := cursor.next(false); !errors.Is(err, context.Canceled) || decoder.InputOffset() != 0 {
		t.Fatalf("token cancellation consumed input: %v", err)
	}
}

func TestFinalPublicationCancellationPreservesPreviousSnapshot(t *testing.T) {
	snapshot := primitiveSnapshot(t)
	l := primitiveLimits()
	var store Store
	previous, err := store.Replace(t.Context(), bytes.NewReader(snapshot.Bytes()), l)
	if err != nil {
		t.Fatal(err)
	}
	records := snapshot.Records()
	records[0].Metadata.FirstName = "replacement"
	replacement, err := Build(t.Context(), records, l)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.publish(ctx, replacement); !errors.Is(err, context.Canceled) || store.Current() != previous {
		t.Fatalf("publication cancellation replaced previous snapshot: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	committed, err := store.publish(ctx, replacement)
	cancel()
	if err != nil || committed != replacement || store.Current() != replacement {
		t.Fatalf("cancellation after commit retroactively failed publication: %v", err)
	}
}

func TestEncodingConsistencyGateRejectsModelDrift(t *testing.T) {
	record := primitiveSnapshot(t).Records()[0]
	l := primitiveLimits()
	var used int64
	measured, err := measureRecord(t.Context(), record, int64(l.MaxRecordBytes))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodeEvidence(t.Context(), record, measured+1, l, &used); err == nil || !strings.Contains(err.Error(), "differs from preflight") {
		t.Fatalf("inconsistent size model accepted: %v", err)
	}
	l.MaxTokensPerRecord = 10
	if _, err := encodeEvidence(t.Context(), record, measured, l, &used); err == nil {
		t.Fatal("encoding gate trusted an insufficient token budget")
	}
}

func TestRecordTokenPreflightMatchesActualJSON(t *testing.T) {
	original := primitiveSnapshot(t).Records()[0]
	allFields := cloneEvidence(original)
	allFields.Metadata.LastName = "Last"
	allFields.Metadata.ThumbnailURL = "https://fixtures.invalid/thumb.jpg"
	idOnly := cloneEvidence(original)
	idOnly.Metadata = Metadata{}
	metadataOnly := cloneEvidence(original)
	metadataOnly.ExternalIDs = nil
	metadataOnly.Metadata.Aliases = nil
	for _, record := range []Evidence{original, allFields, idOnly, metadataOnly} {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
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
		for limit := 0; limit <= tokens+1; limit++ {
			err := validateRecordTokens(record, limit)
			if (err == nil) != (limit >= tokens) {
				t.Fatalf("token preflight disagrees with JSON: limit=%d actual=%d err=%v", limit, tokens, err)
			}
		}
	}
}
