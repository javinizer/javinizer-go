package r18devdump

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_RejectsNullOrMissingContentIDBeforeSwap(t *testing.T) {
	for _, tc := range []struct{ table, otherColumn, otherValue string }{
		{derivedVideoTable, "dvd_id", "NULL-001"},
		{trailerTable, "url", "https://example.invalid/trailer.mp4"},
		{derivedVideoActressTable, "actress_id", "actress1"},
		{derivedVideoCategoryTable, "category_id", "category1"},
		{derivedVideoDirectorTable, "director_id", "director1"},
	} {
		for _, shape := range []string{"null", "missing column", "short row"} {
			t.Run(tc.table+"/"+shape, func(t *testing.T) {
				path := seedDump(t, "old00001\tOLD-001")
				before, err := os.ReadFile(path)
				require.NoError(t, err)
				old, err := Open(path)
				require.NoError(t, err)
				t.Cleanup(func() { _ = old.Close() })

				// Force a complete batch into the transaction before the invalid
				// row: one good video must not hide a silently ignored NULL key.
				var dump strings.Builder
				dump.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
				for i := 0; i < importBatchSize; i++ {
					fmt.Fprintf(&dump, "new%05d\tNEW-%05d\n", i, i)
				}
				dump.WriteString("\\.\n")
				columns, values := tc.otherColumn+", content_id", tc.otherValue+"\t\\N"
				if shape == "missing column" {
					columns, values = tc.otherColumn, tc.otherValue
				} else if shape == "short row" {
					values = tc.otherValue
				}
				fmt.Fprintf(&dump, "COPY public.%s (%s) FROM stdin;\n%s\n\\.\n", tc.table, columns, values)
				beforeSwap, afterSwap := false, false
				_, err = Import(context.Background(), strings.NewReader(dump.String()), path, ImportOptions{
					BeforeSwap: func() error { beforeSwap = true; return nil },
					AfterSwap:  func() { afterSwap = true },
				})
				assert.ErrorIs(t, err, ErrDumpNonCanonicalID)
				if err != nil {
					assert.Contains(t, err.Error(), sqliteTableName(tc.table)+".content_id")
				}
				assert.False(t, beforeSwap, "invalid content must fail before detaching the working handle")
				assert.False(t, afterSwap)
				after, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "working dump bytes must remain unchanged")
				dvdID, lookupErr := old.LookupByContentID(context.Background(), "old00001")
				require.NoError(t, lookupErr)
				assert.Equal(t, "OLD-001", dvdID)
				fresh, openErr := Open(path)
				require.NoError(t, openErr)
				defer func() { _ = fresh.Close() }()
				dvdID, lookupErr = fresh.LookupByContentID(context.Background(), "old00001")
				assert.NoError(t, lookupErr)
				assert.Equal(t, "OLD-001", dvdID, "new readers must still find the working dump")
				for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
					assert.NoFileExists(t, path+suffix)
				}
			})
		}
	}
}

func TestImport_OptionalNullsAndEscapedNullTextRemainValid(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id, runtime_mins, title_en, title_ja) FROM stdin;\n" +
		"new00001\t\\N\t\\N\t\\N\t\\\\N\n\\.\n" +
		"COPY public.source_dmm_trailer (content_id, url) FROM stdin;\nnew00001\t\\N\n\\.\n" +
		"COPY public.derived_video_actress (content_id, actress_id, ordinality, release_date) FROM stdin;\nnew00001\tactress1\t\\N\t\\N\n\\.\n" +
		"COPY public.derived_video_category (content_id, category_id, release_date) FROM stdin;\nnew00001\tcategory1\t\\N\n\\.\n" +
		"COPY public.derived_video_director (content_id, director_id) FROM stdin;\nnew00001\tdirector1\n\\.\n"
	path := filepath.Join(t.TempDir(), "out.db")
	result, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	require.NoError(t, err, "optional NULL values and absent entity blocks remain legal in tiny dumps")
	require.EqualValues(t, 1, result.Rows)
	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	var nullDVD, nullRuntime, nullTitle bool
	var literal string
	require.NoError(t, store.db.QueryRow("SELECT dvd_id IS NULL, runtime_mins IS NULL, title_en IS NULL, title_ja FROM videos").Scan(&nullDVD, &nullRuntime, &nullTitle, &literal))
	assert.True(t, nullDVD)
	assert.True(t, nullRuntime)
	assert.True(t, nullTitle)
	assert.Equal(t, `\N`, literal, "escaped COPY text must not become SQL NULL")
	for _, query := range []string{
		"SELECT url IS NULL FROM trailers",
		"SELECT ordinality IS NULL AND release_date IS NULL FROM video_actresses",
		"SELECT release_date IS NULL FROM video_categories",
		"SELECT count(*) = 1 FROM video_directors",
	} {
		var ok bool
		require.NoError(t, store.db.QueryRow(query).Scan(&ok))
		assert.True(t, ok, query)
	}
}
