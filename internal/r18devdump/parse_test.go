package r18devdump

import (
	"errors"
	"io"
	"strings"
	"testing"
)

const sampleDump = `--
-- PostgreSQL database dump
--

SET statement_timeout = 0;
COPY public.some_other (id, name) FROM stdin;
1	a
\.
COPY public.derived_video (content_id, dvd_id) FROM stdin;
118ipx00535	IPX-535
118abw00001	\N
h_086mesu00103	MESU-103
\.
COPY public.unrelated (x) FROM stdin;
z
\.
`

// videoRow is the (content_id, dvd_id) pair extracted from a derived_video
// DumpRow, used by the parser tests to assert on parsed content.
type videoRow struct {
	ContentID string
	DVDID     string
}

// indexOf returns the index of s in slice, or -1 if absent.
func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

// parseVideos extracts (content_id, dvd_id) pairs from the derived_video
// table in a dump. It mirrors the column-name resolution that the import
// pipeline relies on, exercising ParseDump's robustness (column ordering,
// quoted tables, malformed headers, chunked readers, large buffers).
func parseVideos(r io.Reader) ([]videoRow, error) {
	var rows []videoRow
	found := false
	err := ParseDump(r, func(row DumpRow) error {
		if row.Table != derivedVideoTable {
			return nil
		}
		found = true
		cidIdx, didIdx := indexOf(row.Columns, "content_id"), indexOf(row.Columns, "dvd_id")
		if cidIdx < 0 || didIdx < 0 || cidIdx >= len(row.Values) || didIdx >= len(row.Values) {
			return nil
		}
		contentID := row.Values[cidIdx]
		dvdID := row.Values[didIdx]
		if dvdID == nullSentinel {
			dvdID = ""
		}
		if contentID == "" || contentID == nullSentinel {
			return nil
		}
		rows = append(rows, videoRow{ContentID: contentID, DVDID: dvdID})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no public.derived_video COPY block found in dump")
	}
	return rows, nil
}

func TestParseVideos(t *testing.T) {
	rows, err := parseVideos(strings.NewReader(sampleDump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	want := []videoRow{
		{ContentID: "118ipx00535", DVDID: "IPX-535"},
		{ContentID: "118abw00001", DVDID: ""},
		{ContentID: "h_086mesu00103", DVDID: "MESU-103"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d (%+v)", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d: got %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestParseVideos_ColumnOrderingRobust(t *testing.T) {
	// Columns in reverse order: parser must locate content_id and dvd_id by name.
	dump := `COPY public.derived_video (dvd_id, content_id) FROM stdin;
IPX-535	118ipx00535
\.
`
	rows, err := parseVideos(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ContentID != "118ipx00535" || rows[0].DVDID != "IPX-535" {
		t.Fatalf("got %+v", rows)
	}
}

func TestParseVideos_QuotedTable(t *testing.T) {
	dump := `COPY "public"."derived_video" (content_id, dvd_id) FROM stdin;
118ipx00535	IPX-535
\.
`
	rows, err := parseVideos(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
}

func TestParseVideos_EmptyDVDIDSkippedContentID(t *testing.T) {
	dump := `COPY public.derived_video (content_id, dvd_id) FROM stdin;
\N	IPX-999
118abc00001	ABC-001
\.
`
	rows, err := parseVideos(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ContentID != "118abc00001" {
		t.Fatalf("expected only the non-null content_id row, got %+v", rows)
	}
}

func TestParseVideos_NoCopyBlock(t *testing.T) {
	_, err := parseVideos(strings.NewReader("nothing here"))
	if err == nil {
		t.Fatal("expected error for missing COPY block")
	}
}

func TestParseVideos_CopyHeaderMissingColumns(t *testing.T) {
	// The parser is lenient: when the COPY header lacks content_id or dvd_id,
	// rows are silently skipped (no error) rather than aborting the entire
	// dump. This is the correct behavior for a streaming parser — one
	// malformed table shouldn't fail the whole import.
	tests := []struct{ name, dump string }{
		{"missing content_id", "COPY public.derived_video (dvd_id) FROM stdin;\na\tb\n\\.\n"},
		{"missing dvd_id", "COPY public.derived_video (content_id) FROM stdin;\na\tb\n\\.\n"},
		{"no parens no columns", "COPY public.derived_video FROM stdin;\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := parseVideos(strings.NewReader(tt.dump))
			// No error: malformed headers are skipped, not fatal.
			if err != nil {
				t.Fatalf("expected no error for malformed header, got: %v", err)
			}
			if len(rows) != 0 {
				t.Errorf("expected 0 rows from malformed header, got %d", len(rows))
			}
		})
	}
}

func TestParseVideos_RowWithTooFewColumnsSkipped(t *testing.T) {
	// A data row with fewer tab-separated fields than the COPY header declares
	// is skipped rather than producing an out-of-range access.
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nonlyonecol\n118ipx00535\tIPX-535\n\\.\n"
	rows, err := parseVideos(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ContentID != "118ipx00535" {
		t.Fatalf("expected only the well-formed row, got %+v", rows)
	}
}

func TestParseVideos_EmitErrorStops(t *testing.T) {
	sentinel := errors.New("stop")
	dump := `COPY public.derived_video (content_id, dvd_id) FROM stdin;
a	b
c	d
\.
`
	err := ParseDump(strings.NewReader(dump), func(DumpRow) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want sentinel", err)
	}
}

func TestParseVideos_EmptyInput(t *testing.T) {
	_, err := parseVideos(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestExtractSourceDate(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://r18dotdev.s3.eu-west-1.wasabisys.com/dumps/r18dotdev_dump_2026-04-28.sql.gz", "2026-04-28"},
		{"https://example.com/r18dotdev_dump_2025-12-01.sql.gz", "2025-12-01"},
		{"https://example.com/no_date.sql.gz", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractSourceDate(tt.url); got != tt.want {
			t.Errorf("extractSourceDate(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestParseVideos_LargeLineBuffer(t *testing.T) {
	// A line well above the default 64KB scanner buffer to verify buffering.
	longCID := strings.Repeat("a", 100*1024)
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + longCID + "\tIPX-1\n\\.\n"
	rows, err := parseVideos(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 || rows[0].ContentID != longCID {
		t.Fatalf("long line not handled: %+v", rows)
	}
}

// Ensure ParseDump works with a reader that yields bytes in tiny chunks,
// exercising the bufio.Scanner across Read boundaries.
func TestParseVideos_ChunkedReader(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	rows, err := parseVideos(&chunkReader{s: dump, size: 3})
	if err != nil {
		t.Fatalf("parseVideos: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
}

type chunkReader struct {
	s    string
	pos  int
	size int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.s) {
		return 0, io.EOF
	}
	n := c.size
	if remaining := len(c.s) - c.pos; n > remaining {
		n = remaining
	}
	copy(p, c.s[c.pos:c.pos+n])
	c.pos += n
	return n, nil
}

// TestParseDump_DecodeCopyEscapes verifies that PostgreSQL COPY text-format
// escapes are decoded: \n, \t, \r, \\ become the literal characters. NULL is
// detected on the raw field and carried as nullSentinel; a literal "\N"
// produced by decoding "\\N" stays a real string (NOT NULL), proving the
// NULL/text collision is avoided.
func TestParseDump_DecodeCopyEscapes(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" +
		"118esc00001\tESC\\t-1\n" + // dvd_id contains an escaped tab
		"118esc00002\tESC\\\\-2\n" + // dvd_id contains an escaped backslash
		"118esc00003\t\\N\n" + // NULL dvd_id (raw \N -> nullSentinel)
		"118esc00004\tESC\\\\N-4\n" + // escaped backslash + N -> literal "\N" (NOT NULL)
		"\\.\n"
	var got []DumpRow
	if err := ParseDump(strings.NewReader(dump), func(r DumpRow) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4", len(got))
	}
	// Escaped tab decoded to a literal tab.
	if got[0].Values[1] != "ESC\t-1" {
		t.Errorf("row 0 dvd_id: got %q, want %q (escaped tab decoded)", got[0].Values[1], "ESC\t-1")
	}
	// Escaped backslash decoded to a single backslash.
	if got[1].Values[1] != "ESC\\-2" {
		t.Errorf("row 1 dvd_id: got %q, want %q (escaped backslash decoded)", got[1].Values[1], "ESC\\-2")
	}
	// Raw \N -> NULL sentinel.
	if got[2].Values[1] != nullSentinel {
		t.Errorf("row 2 dvd_id: got %q, want nullSentinel (NULL)", got[2].Values[1])
	}
	// Escaped \\N -> literal "\N" string, NOT the NULL sentinel.
	if got[3].Values[1] != "ESC\\N-4" {
		t.Errorf("row 3 dvd_id: got %q, want %q (escaped \\N decodes to literal backslash-N, not NULL)", got[3].Values[1], "ESC\\N-4")
	}
}

// TestDecodeCopyField_AllEscapes verifies every PostgreSQL COPY text escape
// sequence decodes correctly, including the rarely-hit branches (backspace,
// form feed, vertical tab, a trailing lone backslash, and an unknown escape).
func TestDecodeCopyField_AllEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no escapes", "no escapes"},             // fast path (no backslash)
		{"line\\nbreak", "line\nbreak"},          // \n
		{"tab\\there", "tab\there"},              // \t
		{"cr\\rreturn", "cr\rreturn"},            // \r
		{"bs\\bhere", "bs\bhere"},                // \b
		{"ff\\fhere", "ff\fhere"},                // \f
		{"vt\\vhere", "vt\vhere"},                // \v
		{"back\\\\slash", "back\\slash"},         // \\
		{"unknown\\qescape", "unknown\\qescape"}, // unknown escape kept verbatim
		{"lone\\", "lone\\"},                     // trailing lone backslash
	}
	for _, c := range cases {
		if got := decodeCopyField(c.in); got != c.want {
			t.Errorf("decodeCopyField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestParseCopyHeader_QuotedForms covers the quoted public/schema and quoted
// table-name branches of parseCopyHeader that the basic fixtures don't hit.
func TestParseCopyHeader_QuotedForms(t *testing.T) {
	cases := []struct {
		line  string
		table string
		cols  int
	}{
		{`COPY "public"."derived_video" (content_id, dvd_id) FROM stdin;`, "derived_video", 2},
		{`COPY "public"."video_actresses" FROM stdin;`, "video_actresses", 0},
		{`COPY public."video_categories" (content_id, name) FROM stdin;`, "video_categories", 2},
	}
	for _, c := range cases {
		h, ok := parseCopyHeader(c.line)
		if !ok {
			t.Errorf("parseCopyHeader(%q): expected ok", c.line)
			continue
		}
		if h.table != c.table {
			t.Errorf("parseCopyHeader(%q) table = %q, want %q", c.line, h.table, c.table)
		}
		if len(h.columns) != c.cols {
			t.Errorf("parseCopyHeader(%q) cols = %d, want %d", c.line, len(h.columns), c.cols)
		}
	}
	if _, ok := parseCopyHeader("COPY schema.foo (a) FROM stdin;"); ok {
		t.Error("non-public schema should not parse")
	}
}

// TestParseDump_EmptyStringDistinctFromNull verifies that a genuine empty
// string field is preserved as "" (not coerced to the NULL marker) so the
// import path can keep NULL and empty-string distinct.
func TestParseDump_EmptyStringDistinctFromNull(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" +
		"118emp00001\t\n" + // empty-string dvd_id
		"\\.\n"
	var got []DumpRow
	if err := ParseDump(strings.NewReader(dump), func(r DumpRow) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 1 || got[0].Values[1] != "" {
		t.Fatalf("empty string should stay empty, got %+v", got)
	}
}

// TestParseDump_QuotedTableWithSchema covers the '".' branch of the quoted
// table name stripping (line 121): a COPY with a schema-qualified quoted
// table name like public."schema"."table" must extract "schema" as the table.
func TestParseDump_QuotedTableWithSchema(t *testing.T) {
	dump := `COPY public."schema"."table" (col) FROM stdin;
val1
\.
`
	var gotTable string
	err := ParseDump(strings.NewReader(dump), func(row DumpRow) error {
		gotTable = row.Table
		return nil
	})
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if gotTable != "schema" {
		t.Errorf("table name: got %q, want %q", gotTable, "schema")
	}
}
