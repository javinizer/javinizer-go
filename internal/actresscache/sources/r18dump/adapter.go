// Package r18dump converts bounded offline PostgreSQL COPY dumps into source
// evidence. It performs no fetching, persistence or application identity writes.
package r18dump

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// Limits bounds the entire uncompressed input, each LF-terminated line
// (excluding LF), and all COPY rows, including tables not extracted.
// MaxLineBytes cannot exceed 1 MiB, below the reused parser's 4 MiB ceiling.
// Output limits independently bound accumulated evidence and serialized output.
type Limits struct {
	MaxInputBytes int64
	MaxLineBytes  int
	MaxRows       int
	Output        actresscache.Limits
}

// Receipt describes only the supplied input, never upstream completeness or
// permission to prune. EmptyActressBlock distinguishes an explicit empty block
// from a missing block, which is rejected. SHA256 covers every accepted byte.
type Receipt struct {
	SHA256            string
	Bytes             int64
	Rows              int
	ActressRows       int
	EmptyActressBlock bool
}

// Convert consumes a caller-owned uncompressed reader through EOF, then builds
// an immutable snapshot. Provenance.DatasetSHA256 must be empty: it is computed
// from these exact input bytes. Other provenance is caller asserted. A result is
// returned only after all parsing, read errors and output validation succeed.
// Reads and codec work are cooperatively cancellable, not CPU/Read preemptible.
func Convert(ctx context.Context, input io.Reader, provenance actresscache.Provenance, limits Limits) (*actresscache.Snapshot, Receipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, Receipt{}, err
	}
	if input == nil || limits.MaxInputBytes <= 0 || limits.MaxInputBytes == math.MaxInt64 || limits.MaxLineBytes <= 0 || limits.MaxLineBytes > 1<<20 || limits.MaxRows <= 0 || provenance.DatasetSHA256 != "" {
		return nil, Receipt{}, fmt.Errorf("invalid input limits, reader or supplied digest")
	}
	provenance.ObservedAt = provenance.ObservedAt.UTC().Round(0)
	// An empty artifact validates output limits without charging fictitious rows.
	empty, err := actresscache.Build(ctx, nil, limits.Output)
	if err != nil {
		return nil, Receipt{}, err
	}
	if err := validateProvenance(provenance, limits.Output.MaxStringBytes); err != nil {
		return nil, Receipt{}, err
	}
	probe := provenance
	probe.DatasetSHA256 = strings.Repeat("0", 64)
	gate := &lineGate{ctx: ctx, reader: bufio.NewReaderSize(io.LimitReader(input, limits.MaxInputBytes+1), limits.MaxLineBytes+2), limits: limits, digest: sha256.New()}
	var records []actresscache.Evidence
	c := collector{ctx: ctx, provenance: probe, limits: limits.Output, usedBytes: int64(len(empty.Bytes())), seen: map[string]bool{}}
	err = r18devdump.ParseDump(gate, c.add)
	records = c.records
	if err != nil {
		return nil, Receipt{}, err
	}
	if !gate.actress {
		return nil, Receipt{}, fmt.Errorf("missing actress COPY block")
	}
	if err := ctx.Err(); err != nil {
		return nil, Receipt{}, err
	}
	digest := fmt.Sprintf("%x", gate.digest.Sum(nil))
	receipt := Receipt{digest, gate.total, gate.rows, len(records), len(records) == 0}
	return buildResult(ctx, records, provenance, receipt, limits.Output)
}

func buildResult(ctx context.Context, records []actresscache.Evidence, provenance actresscache.Provenance, receipt Receipt, limits actresscache.Limits) (*actresscache.Snapshot, Receipt, error) {
	for i := range records {
		records[i].Provenance = provenance
		records[i].Provenance.DatasetSHA256 = receipt.SHA256
	}
	snapshot, err := actresscache.Build(ctx, records, limits)
	if err != nil {
		return nil, Receipt{}, err
	}
	return snapshot, receipt, nil
}

type collector struct {
	ctx         context.Context
	provenance  actresscache.Provenance
	limits      actresscache.Limits
	records     []actresscache.Evidence
	usedBytes   int64
	usedStrings int64
	seen        map[string]bool
}

func (c *collector) add(row r18devdump.DumpRow) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if row.Table != "derived_actress" {
		return nil
	}
	if len(c.records) >= c.limits.MaxRecords {
		return fmt.Errorf("actress count budget exceeded")
	}
	record := actresscache.Evidence{Source: "r18dev", Provenance: c.provenance}
	for i, column := range row.Columns {
		if row.IsNull(i) {
			if column == "id" {
				return fmt.Errorf("NULL actress ID")
			}
			continue
		}
		value := row.Values[i]
		switch column {
		case "id":
			record.SourceID = value
		case "name_kanji":
			record.Metadata.JapaneseName = value
		case "name_romaji", "name_kana":
			if value != "" && !contains(record.Metadata.Aliases, value) {
				record.Metadata.Aliases = append(record.Metadata.Aliases, value)
			}
		}
	}
	key, err := actresscache.SourceKey(record.Source, record.SourceID)
	if err != nil {
		return err
	}
	if c.seen[key] {
		return fmt.Errorf("duplicate source key")
	}
	// Each one-record Build reserves the same header strings. Subtract retained
	// record strings from its budget, and charge only its encoded record line.
	remaining := c.limits
	remaining.MaxTotalStringBytes -= c.usedStrings
	single, err := actresscache.Build(c.ctx, []actresscache.Evidence{record}, remaining)
	if err != nil {
		return err
	}
	encoded := single.Bytes()
	recordBytes := int64(len(encoded) - bytes.IndexByte(encoded, '\n') - 1)
	headerGrowth := len(strconv.Itoa(len(c.records)+1)) - len(strconv.Itoa(len(c.records)))
	cost := recordBytes + int64(headerGrowth)
	if cost > c.limits.MaxBytes-c.usedBytes {
		return fmt.Errorf("aggregate encoded output budget exceeded")
	}
	// Build already checked the remaining decoded-text budget. This mirrors only
	// the explicit evidence values (fixed JSON keys are excluded by the format).
	for _, text := range []string{record.Source, record.SourceID, record.Provenance.SourceURL, record.Provenance.DatasetSHA256, record.Provenance.ParserRevision, record.Provenance.ObservedAt.Format(time.RFC3339Nano), record.Metadata.JapaneseName} {
		c.usedStrings += int64(len(text))
	}
	for _, text := range record.Metadata.Aliases {
		c.usedStrings += int64(len(text))
	}
	c.usedBytes += cost
	c.seen[key] = true
	// Retain the compact owned strings, not parser substrings that could keep
	// a large ignored image field alive through the shared source-row backing.
	c.records = append(c.records, single.Records()[0])
	return nil
}

func validateProvenance(p actresscache.Provenance, maxString int) error {
	for _, value := range []string{p.SourceURL, p.ParserRevision, p.ObservedAt.Format(time.RFC3339Nano)} {
		if !utf8.ValidString(value) || len(value) > maxString {
			return fmt.Errorf("invalid provenance text")
		}
	}
	parsed, err := url.Parse(p.SourceURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || strings.TrimSpace(p.ParserRevision) == "" || p.ObservedAt.IsZero() || p.ObservedAt.Year() < 0 || p.ObservedAt.Year() > 9999 {
		return fmt.Errorf("invalid source provenance")
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

var copyHeader = regexp.MustCompile(`^COPY (?:public\.|"public"\.)(?:([a-z_][a-z0-9_]*)|"([a-z_][a-z0-9_]*)") \(([^()]+)\) FROM stdin;$`)
var columnName = regexp.MustCompile(`^(?:[a-z_][a-z0-9_]*|"[a-z_][a-z0-9_]*")$`)

type lineGate struct {
	ctx     context.Context
	reader  *bufio.Reader
	limits  Limits
	digest  hash.Hash
	pending []byte
	total   int64
	rows    int
	columns int
	inBlock bool
	actress bool
}

func (g *lineGate) Read(p []byte) (int, error) {
	if err := g.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(g.pending) == 0 {
		line, err := g.reader.ReadSlice('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("unfinished or oversized line: %w", err)
		}
		if len(line)-1 > g.limits.MaxLineBytes || int64(len(line)) > g.limits.MaxInputBytes-g.total {
			return 0, fmt.Errorf("input byte or line budget exceeded")
		}
		if !utf8.Valid(line) || bytes.IndexByte(line, 0) >= 0 || bytes.IndexByte(line, '\r') >= 0 {
			return 0, fmt.Errorf("invalid text input")
		}
		if err := g.checkLine(string(line[:len(line)-1])); err != nil {
			return 0, err
		}
		g.total += int64(len(line))
		_, _ = g.digest.Write(line)
		g.pending = line
	}
	n := copy(p, g.pending)
	g.pending = g.pending[n:]
	return n, nil
}

func (g *lineGate) checkLine(line string) error {
	if g.inBlock {
		if line == `\.` {
			g.inBlock = false
			return nil
		}
		if g.rows >= g.limits.MaxRows {
			return fmt.Errorf("COPY row budget exceeded")
		}
		fields := strings.Split(line, "\t")
		if len(fields) != g.columns {
			return fmt.Errorf("COPY field count mismatch")
		}
		for _, field := range fields {
			if err := checkEscapes(field); err != nil {
				return err
			}
		}
		g.rows++
		return nil
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "COPY") {
		return nil
	}
	match := copyHeader.FindStringSubmatch(line)
	if match == nil {
		return fmt.Errorf("unsupported COPY header")
	}
	table := match[1] + match[2]
	columns := strings.Split(match[3], ",")
	seen := map[string]bool{}
	for _, raw := range columns {
		raw = strings.TrimSpace(raw)
		if !columnName.MatchString(raw) {
			return fmt.Errorf("invalid COPY column")
		}
		column := strings.Trim(raw, `"`)
		if seen[column] {
			return fmt.Errorf("duplicate COPY column")
		}
		seen[column] = true
		if table == "derived_actress" && column != "id" && column != "name_romaji" && column != "name_kanji" && column != "name_kana" && column != "image_url" {
			return fmt.Errorf("unsupported actress column")
		}
	}
	if table == "derived_actress" {
		if g.actress || !seen["id"] {
			return fmt.Errorf("duplicate actress block or missing ID column")
		}
		g.actress = true
	}
	g.columns = len(columns)
	g.inBlock = true
	return nil
}

func checkEscapes(field string) error {
	if field == `\N` {
		return nil
	}
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' {
			i++
			if i >= len(field) || !strings.ContainsRune(`ntrbfv\`, rune(field[i])) {
				return fmt.Errorf("unsupported COPY escape")
			}
		}
	}
	return nil
}
