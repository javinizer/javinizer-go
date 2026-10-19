package r18devdump

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_RejectsNullOrMissingEntityKeysBeforeSwap(t *testing.T) {
	for _, tc := range []struct{ table, key, otherColumn, otherValue string }{
		{"derived_actress", "id", "name_romaji", "Synthetic"},
		{"derived_maker", "id", "name_en", "Synthetic"},
		{"derived_label", "id", "name_en", "Synthetic"},
		{"derived_series", "id", "name_en", "Synthetic"},
		{"derived_director", "id", "name_romaji", "Synthetic"},
		{"derived_category", "id", "name_en", "Synthetic"},
		{derivedVideoActressTable, "actress_id", contentIDColumn, "new00000"},
		{derivedVideoCategoryTable, "category_id", contentIDColumn, "new00000"},
		{derivedVideoDirectorTable, "director_id", contentIDColumn, "new00000"},
	} {
		for _, shape := range []string{"null", "missing column", "short row"} {
			t.Run(tc.table+"/"+shape, func(t *testing.T) {
				columns, values := tc.otherColumn+", "+tc.key, tc.otherValue+"\t\\N"
				if shape == "missing column" {
					columns = tc.otherColumn
					values = tc.otherValue
				} else if shape == "short row" {
					values = tc.otherValue
				}
				dump := fmt.Sprintf("COPY public.%s (%s) FROM stdin;\n%s\n\\.\n", tc.table, columns, values)
				assertConstraintImportPreservesDump(t, dump, tc.table)
			})
		}
	}
}

func TestImport_RejectsDuplicateLogicalKeysBeforeSwap(t *testing.T) {
	for _, tc := range []struct{ table, columns, first, second string }{
		{derivedVideoTable, "content_id, dvd_id", "duplicate00001\tDUP-001", "duplicate00001\tDUP-002"},
		{"derived_actress", "id, name_romaji", "id1\tFirst", "id1\tSecond"},
		{"derived_maker", "id, name_en", "id1\tFirst", "id1\tSecond"},
		{"derived_label", "id, name_en", "id1\tFirst", "id1\tSecond"},
		{"derived_series", "id, name_en", "id1\tFirst", "id1\tSecond"},
		{"derived_director", "id, name_romaji", "id1\tFirst", "id1\tSecond"},
		{"derived_category", "id, name_en", "id1\tFirst", "id1\tSecond"},
		{trailerTable, "content_id, url", "duplicate00001\tfirst.mp4", "duplicate00001\tsecond.mp4"},
		{derivedVideoActressTable, "content_id, actress_id, ordinality", "duplicate00001\tid1\t1", "duplicate00001\tid1\t2"},
		{derivedVideoCategoryTable, "content_id, category_id, release_date", "duplicate00001\tid1\t2026-01-01", "duplicate00001\tid1\t2026-02-01"},
		{derivedVideoDirectorTable, "content_id, director_id", "duplicate00001\tid1", "duplicate00001\tid1"},
	} {
		for _, shape := range []string{"identical", "conflicting", "across batches"} {
			t.Run(tc.table+"/"+shape, func(t *testing.T) {
				var rows strings.Builder
				rows.WriteString(tc.first + "\n")
				if shape == "across batches" {
					// Fill and flush this table's own batch, then put the duplicate
					// in another COPY block. Both within- and across-batch failures
					// must roll back the entire import.
					for i := 1; i < importBatchSize; i++ {
						_, tail, _ := strings.Cut(tc.first, "\t")
						fmt.Fprintf(&rows, "filler%05d\t%s\n", i, tail)
					}
					fmt.Fprintf(&rows, "\\.\nCOPY public.%s (%s) FROM stdin;\n", tc.table, tc.columns)
				}
				second := tc.second
				if shape == "identical" {
					second = tc.first
				}
				rows.WriteString(second + "\n")
				dump := fmt.Sprintf("COPY public.%s (%s) FROM stdin;\n%s\\.\n", tc.table, tc.columns, rows.String())
				assertConstraintImportPreservesDump(t, dump, tc.table)
			})
		}
	}
}

func assertConstraintImportPreservesDump(t *testing.T, invalid, table string) {
	t.Helper()
	path := seedDump(t, "old00001\tOLD-001")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	old, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = old.Close() })

	var dump strings.Builder
	dump.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < importBatchSize; i++ {
		fmt.Fprintf(&dump, "new%05d\tNEW-%05d\n", i, i)
	}
	dump.WriteString("\\.\n" + invalid)
	beforeSwap, afterSwap := false, false
	_, err = Import(context.Background(), strings.NewReader(dump.String()), path, ImportOptions{
		RequireAllRows: true,
		BeforeSwap:     func() error { beforeSwap = true; return nil },
		AfterSwap:      func() { afterSwap = true },
	})
	assert.ErrorIs(t, err, ErrDumpInvalid)
	if err != nil {
		assert.Contains(t, err.Error(), table)
	}
	assert.False(t, beforeSwap, "invalid rows must fail before detaching the working handle")
	assert.False(t, afterSwap)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "working dump bytes must remain unchanged")
	dvdID, lookupErr := old.LookupByContentID(context.Background(), "old00001")
	assert.NoError(t, lookupErr)
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
}

func TestImport_DefaultStillKeepsFirstDuplicateAcrossBatches(t *testing.T) {
	var dump strings.Builder
	dump.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < importBatchSize; i++ {
		fmt.Fprintf(&dump, "new%05d\tFIRST-%05d\n", i, i)
	}
	dump.WriteString("\\.\nCOPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00000\tSECOND-001\n\\.\n")
	path := filepath.Join(t.TempDir(), "dump.db")
	result, err := Import(context.Background(), strings.NewReader(dump.String()), path, ImportOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, importBatchSize, result.Rows)
	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	dvdID, err := store.LookupByContentID(context.Background(), "new00000")
	require.NoError(t, err)
	assert.Equal(t, "FIRST-00000", dvdID)
}

func TestImport_RequireAllRowsPreservesNullableAndOptionalData(t *testing.T) {
	const videos = "COPY public.derived_video (content_id, dvd_id, runtime_mins, maker_id, label_id, series_id, site_id, service_code) FROM stdin;\n" +
		"new00001\tSAME-001\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\nnew00002\tSAME-001\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\n\\.\n"
	const unknown = "COPY public.unstored (id) FROM stdin;\n\\N\nrepeated\nrepeated\n\\.\n"
	const associations = "COPY public.derived_video_actress (content_id, actress_id, ordinality, release_date) FROM stdin;\n" +
		"new00001\t\t\\N\t\\N\nnew00001\tID\t\\N\t\\N\nnew00001\tid\t\\N\t\\N\nnew00002\tid\t\\N\t\\N\n\\.\n" +
		"COPY public.derived_video_category (content_id, category_id, release_date) FROM stdin;\nnew00001\tmissing1\t\\N\nnew00001\tmissing2\t\\N\n\\.\n" +
		"COPY public.derived_video_director (content_id, director_id) FROM stdin;\nnew00001\tmissing1\nnew00001\tmissing2\n\\.\n" +
		"COPY public.source_dmm_trailer (content_id, url) FROM stdin;\nnew00001\t\\N\n\\.\n"
	for _, includeAssociations := range []bool{false, true} {
		t.Run(fmt.Sprintf("associations=%t", includeAssociations), func(t *testing.T) {
			dump := videos + unknown
			if includeAssociations {
				dump += associations + "COPY public.derived_actress (id, name_romaji) FROM stdin;\n\t\\N\nID\t\\N\nid\t\\N\n\\.\n"
				for _, table := range []string{"derived_maker", "derived_label", "derived_series", "derived_director", "derived_category"} {
					// Omitting all optional columns must still preserve the row.
					dump += fmt.Sprintf("COPY public.%s (id) FROM stdin;\nunreferenced\n\\.\n", table)
				}
			}
			path := filepath.Join(t.TempDir(), "dump.db")
			result, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{RequireAllRows: true})
			require.NoError(t, err)
			assert.EqualValues(t, 2, result.Rows, "matching DVD IDs are not duplicate logical keys")
			store, err := Open(path)
			require.NoError(t, err)
			defer func() { _ = store.Close() }()
			var count int
			require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM videos WHERE runtime_mins IS NULL AND maker_id IS NULL AND label_id IS NULL AND series_id IS NULL AND site_id IS NULL AND service_code IS NULL").Scan(&count))
			assert.Equal(t, 2, count)
			if includeAssociations {
				for table, want := range map[string]int{"video_actresses": 4, "video_categories": 2, "video_directors": 2, "trailers": 1, "actresses": 3, "makers": 1, "labels": 1, "series": 1, "directors": 1, "categories": 1} {
					require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
					assert.Equal(t, want, count, table)
				}
				for _, query := range []string{
					"SELECT COUNT(*) FROM video_actresses WHERE ordinality IS NULL AND release_date IS NULL",
					"SELECT COUNT(*) FROM actresses WHERE name_romaji IS NULL",
				} {
					require.NoError(t, store.db.QueryRow(query).Scan(&count))
					assert.Positive(t, count)
				}
			}
		})
	}
}

func TestImport_RequireAllRowsStillRejectsNoVideos(t *testing.T) {
	path := seedDump(t, "old00001\tOLD-001")
	_, err := Import(context.Background(), strings.NewReader("COPY public.derived_maker (id) FROM stdin;\nid1\n\\.\n"), path, ImportOptions{RequireAllRows: true})
	assert.ErrorIs(t, err, ErrDumpNoRows)
}

var errRowsAffected = errors.New("synthetic RowsAffected failure")

type failedRowsAffected struct{}

func (failedRowsAffected) LastInsertId() (int64, error) { return 0, nil }
func (failedRowsAffected) RowsAffected() (int64, error) { return 1, errRowsAffected }

func TestImport_RejectsRowsAffectedErrorBeforeSwap(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("requireAllRows=%t", strict), func(t *testing.T) {
			d := newImportLifecycleDriver()
			d.insertResult = failedRowsAffected{}
			close(d.releaseRollback)
			path := seedDump(t, "old00001\tOLD-001")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			beforeSwap := false
			_, err = importWithOpenDB(context.Background(), strings.NewReader("COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tNEW-001\n\\.\n"), path, ImportOptions{
				RequireAllRows: strict,
				BeforeSwap:     func() error { beforeSwap = true; return nil },
			}, d.open)
			assert.ErrorIs(t, err, errRowsAffected, "an untrusted count cannot authorize installation even if it equals the batch size")
			assert.NotErrorIs(t, err, ErrDumpInvalid, "driver count failures are import failures, not invalid input")
			assert.False(t, beforeSwap)
			assert.False(t, d.committed)
			waitLifecycle(t, d.rollbackStarted)
			waitLifecycle(t, d.closed)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				assert.NoFileExists(t, path+suffix)
			}
		})
	}
}
