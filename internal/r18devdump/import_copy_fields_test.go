package r18devdump

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_RejectsCOPYFieldCountBeforeSwap(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for table, schema := range tableSchema {
			for _, shape := range []struct{ name, row string }{
				{"missing", "new00001"},
				{"excess", "new00001\tNEW-001\textra"},
			} {
				t.Run(fmt.Sprintf("strict_%t/%s/%s", strict, table, shape.name), func(t *testing.T) {
					t.Parallel()
					ctx := context.Background()
					path := seedDump(t, "old00001\tOLD-001")
					before, err := os.ReadFile(path)
					require.NoError(t, err)
					old, err := Open(path)
					require.NoError(t, err)
					t.Cleanup(func() { _ = old.Close() })
					dump := fmt.Sprintf("COPY public.%s (%s) FROM stdin;\n%s\n\\.\n", table, strings.Join(schema.columns[:2], ", "), shape.row)
					if table != derivedVideoTable {
						dump = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tNEW-001\n\\.\n" + dump
					}
					beforeSwap, afterSwap := false, false
					_, err = Import(ctx, strings.NewReader(dump), path, ImportOptions{
						RequireAllRows: strict,
						BeforeSwap:     func() error { beforeSwap = true; return nil },
						AfterSwap:      func() { afterSwap = true },
					})
					require.ErrorIs(t, err, ErrDumpInvalid)
					require.Contains(t, err.Error(), table)
					require.Contains(t, err.Error(), fmt.Sprintf("%d values for 2 columns", len(strings.Split(shape.row, "\t"))))
					assert.False(t, beforeSwap)
					assert.False(t, afterSwap)
					after, err := os.ReadFile(path)
					require.NoError(t, err)
					assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
					dvdID, err := old.LookupByContentID(ctx, "old00001")
					require.NoError(t, err)
					assert.Equal(t, "OLD-001", dvdID)
					for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
						assert.NoFileExists(t, path+suffix)
					}
				})
			}
		}
	}
}

func TestImport_AcceptsCOPYFieldBoundaries(t *testing.T) {
	const header = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n"
	for _, tc := range []struct {
		name, dump string
		dvdID      sql.NullString
	}{
		{"omitted optional columns", "COPY public.derived_video (content_id) FROM stdin;\nnew00001\n\\.\n", sql.NullString{}},
		{"explicit NULL", header + "new00001\t\\N\n\\.\n", sql.NullString{}},
		{"empty trailing field", header + "new00001\t\n\\.\n", sql.NullString{Valid: true}},
		{"escaped delimiter", "COPY public.derived_video (content_id, dvd_id, title_en) FROM stdin;\nnew00001\tNEW-001\tleft\\tright\n\\.\n", sql.NullString{String: "NEW-001", Valid: true}},
		{"unknown column", "COPY public.derived_video (content_id, dvd_id, future_column) FROM stdin;\nnew00001\tNEW-001\tfuture\n\\.\n", sql.NullString{String: "NEW-001", Valid: true}},
		{"unstored table", header + "new00001\tNEW-001\n\\.\nCOPY public.unstored (one, two) FROM stdin;\nonly_one\none\ttwo\tthree\n\\.\n", sql.NullString{String: "NEW-001", Valid: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dump.db")
			result, err := Import(context.Background(), strings.NewReader(tc.dump), path, ImportOptions{SourceURL: "https://example.invalid/dump.sql", RequireAllRows: true})
			require.NoError(t, err)
			require.EqualValues(t, 1, result.Rows)
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			var dvdID sql.NullString
			require.NoError(t, store.db.QueryRowContext(context.Background(), "SELECT dvd_id FROM videos WHERE content_id = ?", "new00001").Scan(&dvdID))
			assert.Equal(t, tc.dvdID, dvdID)
		})
	}
}
