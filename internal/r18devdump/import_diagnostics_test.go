package r18devdump

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_BatchDiagnosticCountsAndPositions(t *testing.T) {
	var videos strings.Builder
	videos.WriteString("COPY public.derived_video (content_id, dvd_id, service_code, title_en) FROM stdin;\n")
	for i := 0; i < 33; i++ {
		fmt.Fprintf(&videos, "privatekey%03d\tPRIVATE-%03d\tdigital\tprivate title\n", i, i)
	}
	for i := 0; i < 7; i++ {
		fmt.Fprintf(&videos, "privatekey%03d\tPRIVATE-%03d\tdigital\tprivate alternate title\n", i, i)
	}
	videos.WriteString("\\.\n")

	var earlier strings.Builder
	earlier.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < importBatchSize; i++ {
		fmt.Fprintf(&earlier, "privatekey%03d\tPRIVATE-%03d\n", i, i)
	}
	earlier.WriteString("\\.\nCOPY public.derived_video (content_id, dvd_id) FROM stdin;\nprivatekey000\tOTHER-001\nprivatekey000\tOTHER-002\n\\.\n")

	for _, tc := range []struct {
		name, dump, table, keys string
		first, total, stored    int64
		nulls, duplicates       int64
	}{
		{"same composite key batch", videos.String(), derivedVideoTable, "content_id+service_code", 1, 40, 33, 0, 7},
		{"across batches and blocks", earlier.String(), derivedVideoTable, "content_id+service_code", 41, 2, 0, 0, 2},
		{"NULL and duplicate entity keys", oneRowDump +
			"COPY public.derived_actress (name_romaji, id) FROM stdin;\nprivate name\tprivatekey\nprivate name\t\\N\nprivate other name\tprivatekey\nprivate name\t\\N\n\\.\n",
			"derived_actress", "id", 1, 4, 1, 2, 1},
		{"missing required column", oneRowDump +
			"COPY public.derived_category (name_en) FROM stdin;\nprivate name\n\\.\n",
			"derived_category", "id", 1, 1, 0, 1, 0},
		{"composite key", oneRowDump +
			"COPY public.derived_video_actress (content_id, actress_id, ordinality) FROM stdin;\nprivatekey\tprivateactress\t1\nprivatekey\tprivateactress\t2\nprivatekey\t\\N\t\\N\n\\.\n",
			derivedVideoActressTable, "content_id+actress_id", 1, 3, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedDump(t, "old00001\tOLD-001")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			swapped := false
			_, err = Import(context.Background(), strings.NewReader(tc.dump), path, ImportOptions{
				RequireAllRows: true,
				BeforeSwap:     func() error { swapped = true; return nil },
			})
			require.ErrorIs(t, err, ErrDumpInvalid)
			var diagnostic *ImportBatchError
			require.ErrorAs(t, err, &diagnostic)
			assert.Equal(t, tc.table, diagnostic.table)
			assert.Equal(t, tc.keys, strings.Join(diagnostic.keyColumns, "+"))
			assert.Equal(t, tc.first, diagnostic.firstRow)
			assert.Equal(t, tc.total, diagnostic.totalRows)
			assert.Equal(t, tc.stored, diagnostic.storedRows)
			assert.Equal(t, tc.nulls, diagnostic.nullRows)
			assert.Contains(t, diagnostic.Error(), fmt.Sprintf("%d duplicate-key rows, %d NULL-key rows", tc.duplicates, tc.nulls))
			assert.Contains(t, diagnostic.Error(), fmt.Sprintf("source table rows %d-%d", tc.first, tc.first+tc.total-1))
			assert.Contains(t, diagnostic.Error(), "key columns: "+tc.keys)
			assert.NotContains(t, diagnostic.Error(), "private")
			assert.NotContains(t, diagnostic.Error(), "PRIVATE")
			assert.NotContains(t, diagnostic.Error(), "OTHER")
			assert.False(t, swapped)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				assert.NoFileExists(t, path+suffix)
			}
		})
	}
}

func TestImport_BatchDiagnosticDoesNotChangeLegacyOrNullableAcceptance(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id, runtime_mins, title_en) FROM stdin;\n" +
		"new00001\tSAME-001\t\\N\t\\N\nnew00002\tSAME-001\t\\N\t\\N\n\\.\n"
	for _, strict := range []bool{false, true} {
		result, err := Import(context.Background(), strings.NewReader(dump),
			filepath.Join(t.TempDir(), "nullable.db"), ImportOptions{RequireAllRows: strict})
		require.NoError(t, err)
		assert.EqualValues(t, 2, result.Rows)
	}
	dump += "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tOTHER-001\n\\.\n"
	path := filepath.Join(t.TempDir(), "legacy.db")
	result, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, result.Rows)
	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	id, err := store.LookupByContentID(context.Background(), "new00001")
	require.NoError(t, err)
	assert.Equal(t, "SAME-001", id)
}
