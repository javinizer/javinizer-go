package actresscache

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type header struct {
	Format        string `json:"format"`
	SchemaVersion int    `json:"schema_version"`
	RecordCount   int    `json:"record_count"`
}

type keyedEvidence struct {
	key     string
	record  Evidence
	encoded []byte
}

// Build creates a deterministic, immutable snapshot from offline source evidence.
// It never fetches URLs, resolves identities or mutates application records.
func Build(ctx context.Context, records []Evidence, limits Limits) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	if len(records) > limits.MaxRecords || int64(len(records)) > limits.MaxBytes {
		return nil, fmt.Errorf("record count budget exceeded")
	}
	descriptor := []byte(`{"format":"` + formatName + `","schema_version":` + strconv.Itoa(schemaVersion) + `,"record_count":` + strconv.Itoa(len(records)) + `}`)
	if len(descriptor) > limits.MaxRecordBytes || int64(len(descriptor))+1 > limits.MaxBytes {
		return nil, fmt.Errorf("header byte budget exceeded")
	}
	var totalStrings int64
	if err := validateJSON(ctx, descriptor, limits, &totalStrings, "format", "schema_version", "record_count"); err != nil {
		return nil, err
	}
	usedBytes := int64(len(descriptor)) + 1
	var entries []keyedEvidence
	seen := make(map[string]bool)
	for _, record := range records {
		recordBudget := min(int64(limits.MaxRecordBytes), limits.MaxBytes-usedBytes-1)
		entry, err := prepareRecord(ctx, record, limits, recordBudget, &totalStrings)
		if err != nil {
			return nil, err
		}
		if seen[entry.key] {
			return nil, fmt.Errorf("duplicate source key %q", entry.key)
		}
		entries = append(entries, entry)
		seen[entry.key] = true
		usedBytes += int64(len(entry.encoded)) + 1
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	return assembleSnapshot(ctx, descriptor, entries)
}

func prepareRecord(ctx context.Context, record Evidence, limits Limits, recordBudget int64, totalStrings *int64) (keyedEvidence, error) {
	if err := ctx.Err(); err != nil {
		return keyedEvidence{}, err
	}
	key, err := SourceKey(record.Source, record.SourceID)
	if err != nil {
		return keyedEvidence{}, err
	}
	if recordBudget <= 0 {
		return keyedEvidence{}, fmt.Errorf("encoded byte budget exceeded")
	}
	record.Provenance.ObservedAt = record.Provenance.ObservedAt.UTC().Round(0)
	if err := validateEvidence(record, limits, recordBudget, limits.MaxTotalStringBytes-*totalStrings); err != nil {
		return keyedEvidence{}, fmt.Errorf("record %s: %w", key, err)
	}
	if err := validateRecordTokens(record, limits.MaxTokensPerRecord); err != nil {
		return keyedEvidence{}, err
	}
	measured, err := measureRecord(ctx, record, recordBudget)
	if err != nil {
		return keyedEvidence{}, err
	}
	record = ownEvidence(record)
	sort.Strings(record.Metadata.Aliases)
	for i := 1; i < len(record.Metadata.Aliases); i++ {
		if record.Metadata.Aliases[i] == record.Metadata.Aliases[i-1] {
			return keyedEvidence{}, fmt.Errorf("duplicate alias in %s", key)
		}
	}
	sort.Slice(record.ExternalIDs, func(i, j int) bool {
		a, b := record.ExternalIDs[i], record.ExternalIDs[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Value < b.Value
	})
	for i := 1; i < len(record.ExternalIDs); i++ {
		if record.ExternalIDs[i] == record.ExternalIDs[i-1] {
			return keyedEvidence{}, fmt.Errorf("duplicate external-ID claim in %s", key)
		}
	}
	data, err := encodeEvidence(ctx, record, measured, limits, totalStrings)
	if err != nil {
		return keyedEvidence{}, err
	}
	return keyedEvidence{key, record, data}, nil
}

func encodeEvidence(ctx context.Context, record Evidence, measured int64, limits Limits, totalStrings *int64) ([]byte, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != measured {
		return nil, fmt.Errorf("record encoding size differs from preflight")
	}
	if err := validateJSON(ctx, data, limits, totalStrings, "source", "source_id", "provenance", "metadata"); err != nil {
		return nil, err
	}
	return data, nil
}

func assembleSnapshot(ctx context.Context, descriptor []byte, entries []keyedEvidence) (*Snapshot, error) {
	var encoded bytes.Buffer
	_, _ = encoded.Write(descriptor)
	_ = encoded.WriteByte('\n')
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, _ = encoded.Write(entry.encoded)
		_ = encoded.WriteByte('\n')
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newSnapshot(entries, encoded.Bytes()), nil
}

func validateRecordTokens(record Evidence, limit int) error {
	remaining := limit
	spend := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	ok := spend(20)
	for _, value := range []string{record.Metadata.FirstName, record.Metadata.LastName, record.Metadata.JapaneseName} {
		if value != "" {
			ok = ok && spend(2)
		}
	}
	if len(record.Metadata.Aliases) > 0 {
		ok = ok && spend(3) && spend(len(record.Metadata.Aliases))
	}
	if record.Metadata.ThumbnailURL != "" {
		ok = ok && spend(2)
	}
	if len(record.ExternalIDs) > 0 {
		ok = ok && spend(3)
		ok = ok && len(record.ExternalIDs) <= remaining/6
	}
	if !ok {
		return fmt.Errorf("JSON token budget exceeded")
	}
	return nil
}

// Decode reads the bounded v1 NDJSON format and constructs a complete snapshot.
// Blocking readers must provide their own read deadlines or cancellation behavior.
func Decode(ctx context.Context, input io.Reader, limits Limits) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("cache reader is required")
	}
	counted := &countReader{reader: io.LimitReader(contextReader{ctx, input}, limits.MaxBytes+1)}
	reader := bufio.NewReader(counted)
	var stringsUsed int64
	line, err := readCacheLine(ctx, reader, counted, limits)
	if err != nil {
		return nil, fmt.Errorf("read cache header: %w", err)
	}
	if err := validateJSON(ctx, line, limits, &stringsUsed, "format", "schema_version", "record_count"); err != nil {
		return nil, err
	}
	var descriptor header
	if err := decodeLine(line, &descriptor); err != nil {
		return nil, err
	}
	if descriptor.Format != formatName || descriptor.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("unsupported cache format or schema")
	}
	if descriptor.RecordCount < 0 || descriptor.RecordCount > limits.MaxRecords {
		return nil, fmt.Errorf("record count budget exceeded")
	}
	var records []Evidence
	for i := 0; i < descriptor.RecordCount; i++ {
		line, err := readCacheLine(ctx, reader, counted, limits)
		if err != nil {
			return nil, fmt.Errorf("read record %d: %w", i, err)
		}
		if err := validateJSON(ctx, line, limits, &stringsUsed, "source", "source_id", "provenance", "metadata"); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		var record Evidence
		if err := decodeLine(line, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	_, err = reader.Peek(1)
	if counted.bytes > limits.MaxBytes {
		return nil, fmt.Errorf("encoded byte budget exceeded")
	}
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("trailing cache data")
	}
	return Build(ctx, records, limits)
}

func readCacheLine(ctx context.Context, reader *bufio.Reader, counted *countReader, limits Limits) ([]byte, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, err := reader.ReadSlice('\n')
		if counted.bytes > limits.MaxBytes {
			return nil, fmt.Errorf("encoded byte budget exceeded")
		}
		if err == nil {
			part = part[:len(part)-1]
		}
		if len(part) > limits.MaxRecordBytes-len(line) {
			return nil, fmt.Errorf("record byte budget exceeded")
		}
		line = append(line, part...)
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

func decodeLine(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode cache line: %w", err)
	}
	return nil
}

func validateLimits(limits Limits) error {
	if limits.MaxBytes < 1 || limits.MaxBytes == math.MaxInt64 || limits.MaxRecordBytes < 1 || limits.MaxRecords < 1 || limits.MaxStringBytes < 1 || limits.MaxTotalStringBytes < 1 || limits.MaxAliases < 1 || limits.MaxExternalIDs < 1 || limits.MaxTokensPerRecord < 1 {
		return fmt.Errorf("cache limits must be positive and permit encoded overflow detection")
	}
	return nil
}

func validateEvidence(record Evidence, limits Limits, recordBudget, stringBudget int64) error {
	if len(record.Metadata.Aliases) > limits.MaxAliases || len(record.ExternalIDs) > limits.MaxExternalIDs || int64(len(record.Metadata.Aliases)) > recordBudget || int64(len(record.ExternalIDs)) > recordBudget {
		return fmt.Errorf("record collection budget exceeded")
	}
	var usedStrings int64
	checkText := func(value string) error {
		n := int64(len(value))
		if n > recordBudget-usedStrings || n > stringBudget-usedStrings {
			return fmt.Errorf("record or aggregate string budget exceeded")
		}
		if len(value) > limits.MaxStringBytes || !utf8.ValidString(value) {
			return fmt.Errorf("invalid UTF-8 or string byte budget exceeded")
		}
		usedStrings += n
		return nil
	}
	for _, value := range []string{record.Source, record.SourceID, record.Provenance.SourceURL, record.Provenance.DatasetSHA256, record.Provenance.ParserRevision, record.Provenance.ObservedAt.Format(time.RFC3339Nano), record.Metadata.FirstName, record.Metadata.LastName, record.Metadata.JapaneseName, record.Metadata.ThumbnailURL} {
		if err := checkText(value); err != nil {
			return err
		}
	}
	for _, alias := range record.Metadata.Aliases {
		if err := checkText(alias); err != nil {
			return err
		}
	}
	for _, id := range record.ExternalIDs {
		if !validNamespace(id.Namespace) || !validID(id.Value) {
			return fmt.Errorf("invalid external-ID claim")
		}
		if err := checkText(id.Namespace); err != nil {
			return err
		}
		if err := checkText(id.Value); err != nil {
			return err
		}
	}
	if record.Provenance.ObservedAt.IsZero() || strings.TrimSpace(record.Provenance.ParserRevision) == "" {
		return fmt.Errorf("incomplete source provenance")
	}
	digest, err := hex.DecodeString(record.Provenance.DatasetSHA256)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != record.Provenance.DatasetSHA256 {
		return fmt.Errorf("invalid upstream dataset SHA-256")
	}
	if err := validateReference(record.Provenance.SourceURL); err != nil {
		return err
	}
	if record.Metadata.ThumbnailURL != "" {
		if err := validateReference(record.Metadata.ThumbnailURL); err != nil {
			return err
		}
	}
	for _, alias := range record.Metadata.Aliases {
		if strings.TrimSpace(alias) == "" {
			return fmt.Errorf("empty alias")
		}
	}
	if strings.TrimSpace(record.Metadata.FirstName+record.Metadata.LastName+record.Metadata.JapaneseName) == "" && len(record.Metadata.Aliases) == 0 && len(record.ExternalIDs) == 0 && record.Metadata.ThumbnailURL == "" {
		return fmt.Errorf("record has no metadata or external-ID claims")
	}
	return nil
}

func validateReference(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("source references require credential-free HTTP(S) URLs without fragments")
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

type countReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.bytes += int64(n)
	return n, err
}
