package dump

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// TestRunSearch_PresentNoDVDID covers the third lookup state: rows present in
// the dump but with NULL dvd_id must be reported as such, not as "No match".
func TestRunSearch_PresentNoDVDID(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		dump := "COPY public.derived_video (content_id, dvd_id, release_date, service_code) FROM stdin;\n" +
			"lulu00441\t\\N\t2026-07-03\tdigital\n" +
			"lulu441\t\\N\t2026-07-07\tmono\n" +
			"118ipx00535\tIPX-535\t2013-03-01\tdigital\n" +
			"\\.\n"
		if _, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dbPath, r18devdump.ImportOptions{}); err != nil {
			t.Fatalf("Import: %v", err)
		}

		for _, q := range []string{"LULU-441", "lulu00441"} {
			var buf bytes.Buffer
			if err := runSearch(&buf, "config.yaml", q); err != nil {
				t.Fatalf("runSearch(%q): %v", q, err)
			}
			out := buf.String()
			if strings.Contains(out, "No match") {
				t.Errorf("runSearch(%q) must not print 'No match': %s", q, out)
			}
			if !strings.Contains(out, "has no dvd_id") {
				t.Errorf("runSearch(%q) missing present-no-dvd_id explanation: %s", q, out)
			}
			if !strings.Contains(out, "lulu00441") || !strings.Contains(out, "lulu441") {
				t.Errorf("runSearch(%q) should list both matched rows: %s", q, out)
			}
			if strings.Index(out, "lulu00441") > strings.Index(out, "lulu441") {
				t.Errorf("runSearch(%q) candidate order wrong (digital first): %s", q, out)
			}
		}
	})
}

// TestRunSearch_WhitespaceQueryKeepsDirection: padded input must still be
// reported in the content_id -> dvd_id direction (Codex round-4 P1).
func TestRunSearch_WhitespaceQueryKeepsDirection(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
		if _, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dbPath, r18devdump.ImportOptions{}); err != nil {
			t.Fatalf("Import: %v", err)
		}
		var buf bytes.Buffer
		if err := runSearch(&buf, "config.yaml", " 118ipx00535 "); err != nil {
			t.Fatalf("runSearch: %v", err)
		}
		if !strings.Contains(buf.String(), "dvd_id: IPX-535") {
			t.Errorf("direction lost for padded query: %s", buf.String())
		}
	})
}

// TestRunSearch_MappedStillSingleLine keeps the legacy mapped output shape
// for both lookup directions.
func TestRunSearch_MappedStillSingleLine(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
		if _, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dbPath, r18devdump.ImportOptions{}); err != nil {
			t.Fatalf("Import: %v", err)
		}

		var buf bytes.Buffer
		if err := runSearch(&buf, "config.yaml", "IPX-535"); err != nil {
			t.Fatalf("runSearch: %v", err)
		}
		if !strings.Contains(buf.String(), "IPX-535 -> content_id: 118ipx00535") {
			t.Errorf("dvd-style mapped output changed: %s", buf.String())
		}

		buf.Reset()
		if err := runSearch(&buf, "config.yaml", "118ipx00535"); err != nil {
			t.Fatalf("runSearch: %v", err)
		}
		if !strings.Contains(buf.String(), "118ipx00535 -> dvd_id: IPX-535") {
			t.Errorf("content-style mapped output changed: %s", buf.String())
		}
	})
}
