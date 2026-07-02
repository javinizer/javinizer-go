// Package r18devdump provides a local lookup over a cached r18.dev database
// dump. The dump is a PostgreSQL pg_dump whose public.derived_video table maps
// DMM content_ids to display dvd_ids. This package streams that dump into a
// sidecar SQLite database and serves read-only content_id <-> dvd_id lookups
// and full movie metadata, letting the r18.dev scraper skip its
// rate-limit-prone HTTP probing entirely when a local copy is present.
package r18devdump

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// nullSentinel is the internal marker for a SQL NULL carried in a DumpRow's
// Values. It is distinct from the dump's "\N" text marker: ParseDump detects
// NULL on the RAW field (before COPY-escape decoding) and substitutes this
// sentinel, so a decoded value can never collide with the NULL marker (a
// literal "\N" produced by decoding "\\N" stays a real string, not NULL).
// nullableValue binds this sentinel as SQL nil.
const nullSentinel = "\x00__r18devdump_null__\x00"

// DumpRow is one row from any COPY block in the pg_dump, with its table name,
// column names (parsed from the COPY header), and tab-delimited values.
type DumpRow struct {
	Table   string
	Columns []string
	Values  []string
}

// ParseDump streams a PostgreSQL pg_dump and emits DumpRows from every COPY
// block to the provided callback. It is a pure consumer of an io.Reader and
// performs no I/O of its own, making it trivially testable with fixture
// strings.
//
// pg_dump encodes NULL values as the literal "\N"; callers receive them
// verbatim and may interpret them as needed.
func ParseDump(r io.Reader, emit func(DumpRow) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var columns []string
	inCopy := false
	table := ""
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if !inCopy {
			if copyInfo, ok := parseCopyHeader(line); ok {
				table = copyInfo.table
				columns = copyInfo.columns
				inCopy = true
			}
			continue
		}

		// pg_dump terminates a COPY block with a line containing exactly "\.".
		if line == "\\." {
			inCopy = false
			table = ""
			columns = nil
			continue
		}

		values := strings.Split(line, "\t")
		for i, v := range values {
			// Detect NULL on the raw field BEFORE decoding: the dump marks NULL
			// as exactly "\N", while a literal "\N" in the data is escaped as
			// "\\N" and must decode to the real string "\N", not NULL. Carry
			// NULL as a private sentinel so nullableValue can bind nil without
			// any collision with decoded text.
			if v == "\\N" {
				values[i] = nullSentinel
			} else {
				values[i] = decodeCopyField(v)
			}
		}
		if err := emit(DumpRow{Table: table, Columns: columns, Values: values}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning dump: %w", err)
	}
	return nil
}

// copyHeader holds the parsed table name and column list from a COPY statement.
type copyHeader struct {
	table   string
	columns []string
}

// parseCopyHeader parses a "COPY public.<table> (col1, col2, ...) FROM stdin;"
// line, returning the table name and column list. Accepts both unquoted
// (COPY public.foo ...) and quoted (COPY "public"."foo" ...) pg_dump forms.
// Returns ok=false for non-COPY lines or malformed statements.
func parseCopyHeader(line string) (copyHeader, bool) {
	if !strings.HasPrefix(line, "COPY ") {
		return copyHeader{}, false
	}
	rest := strings.TrimPrefix(line, "COPY ")

	// Extract table name: "public.<name>" or "public"."<name>" or "<name>"
	table := ""
	switch {
	case strings.HasPrefix(rest, "public."):
		table = strings.TrimPrefix(rest, "public.")
	case strings.HasPrefix(rest, `"public".`):
		table = strings.TrimPrefix(rest, `"public".`)
	default:
		return copyHeader{}, false
	}
	// Strip trailing quoted form or the " (columns...) FROM stdin;" suffix.
	if strings.HasPrefix(table, `"`) {
		// Quoted table name: "name"
		if end := strings.Index(table, `".`); end >= 0 {
			table = table[1:end]
		} else if end := strings.Index(table, `" `); end >= 0 {
			table = table[1:end]
		}
	} else {
		// Unquoted: table name ends at space or "("
		if sp := strings.IndexAny(table, " ("); sp >= 0 {
			table = table[:sp]
		}
	}

	// Extract column list from "(col1, col2, ...)"
	open := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if open < 0 || closeIdx < 0 || closeIdx < open {
		// COPY blocks without an explicit column list — emit empty columns.
		return copyHeader{table: table, columns: nil}, true
	}
	body := line[open+1 : closeIdx]
	cols := strings.Split(body, ",")
	for i, c := range cols {
		c = strings.TrimSpace(c)
		// Strip surrounding quotes from quoted column names.
		c = strings.Trim(c, `"`)
		cols[i] = c
	}
	return copyHeader{table: table, columns: cols}, true
}

// derivedVideoTable is the dump table name for the main video metadata table.
const derivedVideoTable = "derived_video"

// decodeCopyField unescapes a single PostgreSQL COPY text-format field. The
// pg_dump text format encodes special characters with a backslash escape:
// \n (newline), \t (tab), \r (CR), \b (backspace), \f (form feed), \v
// (vertical tab), \\ (literal backslash). NULL is handled separately by
// ParseDump (detected on the raw field) before this runs, so a literal "\N"
// produced by decoding "\\N" is a real string, not a NULL marker. A backslash
// not followed by a recognized escape is kept literal.
func decodeCopyField(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			b.WriteByte('\\')
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '\\':
			b.WriteByte('\\')
		default:
			// Unknown escape: keep the backslash and the next char verbatim.
			b.WriteByte('\\')
			b.WriteByte(s[i+1])
		}
		i++
	}
	return b.String()
}
