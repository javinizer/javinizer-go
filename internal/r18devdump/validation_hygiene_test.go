package r18devdump

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hygieneContentIDTables() []struct{ table, otherColumn, otherValue string } {
	return []struct{ table, otherColumn, otherValue string }{
		{derivedVideoTable, "dvd_id", "CONTROL-001"},
		{trailerTable, "url", "https://example.invalid/trailer.mp4"},
		{derivedVideoActressTable, "actress_id", "actress1"},
		{derivedVideoCategoryTable, "category_id", "category1"},
		{derivedVideoDirectorTable, "director_id", "director1"},
	}
}

func hygieneControlCharacters() []rune {
	var controls []rune
	for control := rune(0); control <= 0x1f; control++ {
		controls = append(controls, control)
	}
	for control := rune(0x7f); control <= 0x9f; control++ {
		controls = append(controls, control)
	}
	return controls
}

func TestImport_RejectsControlContentIDBeforeSwap(t *testing.T) {
	escape := strings.NewReplacer("\b", `\b`, "\t", `\t`, "\n", `\n`, "\v", `\v`, "\f", `\f`, "\r", `\r`)
	for _, tc := range hygieneContentIDTables() {
		t.Run(tc.table, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			path := seedDump(t, "old00001\tOLD-001")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			old, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = old.Close() })
			for _, control := range hygieneControlCharacters() {
				t.Run(fmt.Sprintf("control_%04x", control), func(t *testing.T) {
					contentID := escape.Replace("bad" + string(control) + "00001")
					dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tNEW-001\n\\.\n" +
						fmt.Sprintf("COPY public.%s (content_id, %s) FROM stdin;\n%s\t%s\n\\.\n", tc.table, tc.otherColumn, contentID, tc.otherValue)
					beforeSwap, afterSwap := false, false
					_, err := Import(ctx, strings.NewReader(dump), path, ImportOptions{
						RequireAllRows: true,
						BeforeSwap:     func() error { beforeSwap = true; return nil },
						AfterSwap:      func() { afterSwap = true },
					})
					require.ErrorIs(t, err, ErrDumpNonCanonicalID)
					assert.False(t, beforeSwap)
					assert.False(t, afterSwap)
					after, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
					dvdID, lookupErr := old.LookupByContentID(ctx, "old00001")
					require.NoError(t, lookupErr)
					assert.Equal(t, "OLD-001", dvdID)
					for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
						assert.NoFileExists(t, path+suffix)
					}
				})
			}
		})
	}
}

func TestValidateSidecar_RejectsControlContentID(t *testing.T) {
	for _, tc := range hygieneContentIDTables() {
		table := sqliteTableName(tc.table)
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			path := importFixture(t)
			if table != "videos" {
				alterFixture(t, path, fmt.Sprintf("INSERT INTO %s (content_id, %s) VALUES ('118ipx00535', '%s')", table, tc.otherColumn, tc.otherValue))
			}
			for _, control := range hygieneControlCharacters() {
				t.Run(fmt.Sprintf("control_%04x", control), func(t *testing.T) {
					alterFixture(t, path, "PRAGMA synchronous = OFF", fmt.Sprintf("UPDATE %s SET content_id = 'bad'||char(%d)||'00001'", table, control))
					store, err := ValidateSidecar(context.Background(), path)
					if store != nil {
						require.NoError(t, store.Close())
					}
					require.ErrorIs(t, err, ErrDumpInvalid)
					require.Nil(t, store)
					require.Contains(t, err.Error(), table)
				})
			}
		})
	}
}

func TestValidateSidecar_RejectsBlankRequiredProvenance(t *testing.T) {
	for _, collation := range []string{"BINARY", "NOCASE", "RTRIM"} {
		for _, key := range []string{"source_url", "imported_at"} {
			path := collatedProvenanceFixture(t, collation)
			for _, value := range []struct{ name, sql string }{
				{"empty", "''"},
				{"spaces", "'   '"},
				{"ascii_whitespace", "char(9)||char(10)||char(11)||char(12)||char(13)"},
				{"unicode_whitespace", "char(160)||char(8195)"},
				{"empty_blob", "X''"},
			} {
				t.Run(collation+"/"+key+"/"+value.name, func(t *testing.T) {
					alterFixture(t, path, "PRAGMA synchronous = OFF", fmt.Sprintf("UPDATE dump_meta SET value = %s WHERE key COLLATE BINARY = '%s'", value.sql, key))
					store, err := ValidateSidecar(context.Background(), path)
					if store != nil {
						require.NoError(t, store.Close())
					}
					require.ErrorIs(t, err, ErrDumpInvalid)
					require.Nil(t, store)
					require.Contains(t, err.Error(), "provenance")
				})
			}
		}
	}
}

func TestValidateSidecar_EmptyOptionalProvenanceRemainsValid(t *testing.T) {
	for _, collation := range []string{"BINARY", "NOCASE", "RTRIM"} {
		t.Run(collation, func(t *testing.T) {
			path := collatedProvenanceFixture(t, collation)
			alterFixture(t, path,
				"UPDATE dump_meta SET value = '' WHERE key = 'source_date'",
				"INSERT INTO dump_meta VALUES ('future_metadata', '')")
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			defer store.Close()
			stats, err := store.Stats(context.Background())
			require.NoError(t, err)
			require.Empty(t, stats.SourceDate)
			require.NotEmpty(t, stats.SourceURL)
			require.NotEmpty(t, stats.ImportedAt)
		})
	}
}
