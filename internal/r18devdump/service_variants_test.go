package r18devdump

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// The 2026-10-20 upstream dump declares PRIMARY KEY (content_id, service_code).
// Public source: https://r18dotdev.s3.eu-west-1.wasabisys.com/dumps/r18dotdev_dump_2026-10-20.sql.gz
// These synthetic rows preserve that identity shape, not source titles.
func TestServiceVariants_PreserveAllRowsWithoutSelectingMetadata(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverse=%t/strict=%t", reverse, strict), func(t *testing.T) {
				rows := []string{"variant00001\tFIRST-001\tdigital\tFirst", "variant00001\tSECOND-001\trental\tSecond", "variant00002\tSAFE-002\tdigital\tSafe"}
				if reverse {
					rows[0], rows[1] = rows[1], rows[0]
				}
				dump := "COPY public.derived_video (content_id, dvd_id, service_code, title_en) FROM stdin;\n" + strings.Join(rows, "\n") + "\n\\.\n" +
					"COPY public.derived_actress (id, name_romaji) FROM stdin;\n1\tShared\n\\.\n" +
					"COPY public.derived_video_actress (content_id, actress_id) FROM stdin;\nvariant00001\t1\n\\.\n"
				path := filepath.Join(t.TempDir(), "variants.db")
				result, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{SourceURL: "https://example.invalid/dump", RequireAllRows: strict})
				require.NoError(t, err)
				require.EqualValues(t, 3, result.Rows)
				store, err := ValidateSidecar(context.Background(), path)
				require.NoError(t, err)
				defer store.Close()
				for _, id := range []string{"variant00001", "VARIANT00001", "FIRST-001", "SECOND-001"} {
					_, err := store.LookupMovie(context.Background(), id)
					require.ErrorIs(t, err, models.ErrDumpMiss, id)
					_, err = store.MatchByDisplayID(context.Background(), id)
					require.ErrorIs(t, err, models.ErrDumpMiss, id)
					_, err = store.LookupByDVDID(context.Background(), id)
					require.ErrorIs(t, err, models.ErrDumpMiss, id)
				}
				_, err = store.LookupByContentID(context.Background(), "variant00001")
				require.ErrorIs(t, err, models.ErrDumpMiss)
				movie, err := store.LookupMovie(context.Background(), "SAFE-002")
				require.NoError(t, err)
				require.Equal(t, "Safe", movie.TitleEn)
			})
		}
	}
}

func TestServiceVariants_ReportedFortyRowsAreAllStored(t *testing.T) {
	var dump strings.Builder
	dump.WriteString("COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\n")
	for i := 0; i < 33; i++ {
		fmt.Fprintf(&dump, "variant%03d\tTEST-%03d\tdigital\n", i, i)
	}
	for i := 0; i < 7; i++ {
		fmt.Fprintf(&dump, "variant%03d\tOTHER-%03d\trental\n", i, i)
	}
	dump.WriteString("\\.\n")
	result, err := Import(context.Background(), strings.NewReader(dump.String()), filepath.Join(t.TempDir(), "all.db"), ImportOptions{RequireAllRows: true})
	require.NoError(t, err)
	require.EqualValues(t, 40, result.Rows)
}

func TestServiceVariants_CaseAliasesDoNotHideOtherServices(t *testing.T) {
	const dump = "COPY public.derived_video (content_id, dvd_id, service_code, title_en) FROM stdin;\n" +
		"case00001\tCASE-001\tdigital\tlower\ncase00001\tOTHER-001\trental\tother service\nCase00001\tCASE-001\tdigital\tupper\n\\.\n"
	path := filepath.Join(t.TempDir(), "case.db")
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{RequireAllRows: true})
	require.NoError(t, err)
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	for _, id := range []string{"case00001", "CASE00001", "CASE-001", "OTHER-001"} {
		_, err = store.LookupMovie(context.Background(), id)
		require.ErrorIs(t, err, models.ErrDumpMiss, id)
		_, err = store.MatchByDisplayID(context.Background(), id)
		require.ErrorIs(t, err, models.ErrDumpMiss, id)
	}
	movie, err := store.LookupMovie(context.Background(), "Case00001")
	require.NoError(t, err)
	require.Equal(t, "upper", movie.TitleEn, "explicit case-distinct unique identity still works")
}

func TestServiceVariants_StrictCompositeDuplicatesPreserveOldDump(t *testing.T) {
	for _, service := range []string{"digital", `\N`, ""} {
		for _, tail := range []string{"FIRST-001", "SECOND-002"} {
			dump := "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\n" +
				"repeat00001\tFIRST-001\t" + service + "\nrepeat00001\t" + tail + "\t" + service + "\n\\.\n"
			assertConstraintImportPreservesDump(t, dump, derivedVideoTable)
		}
	}
}

func TestServiceVariants_RealSourceProjectionPreservesNineteenRows(t *testing.T) {
	data, err := os.ReadFile("testdata/service_variants_2026-10-20.sql")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "upstream.db")
	result, err := Import(context.Background(), strings.NewReader(string(data)), path, ImportOptions{RequireAllRows: true, SourceURL: "https://r18.dev/dumps"})
	require.NoError(t, err)
	require.EqualValues(t, 19, result.Rows)
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	defer store.Close()
	for service, runtime := range map[string]int{"digital": 60, "rental": 120} {
		var got int
		require.NoError(t, store.db.QueryRow("SELECT runtime_mins FROM videos WHERE content_id=? AND service_code=?", "104anpd001", service).Scan(&got))
		require.Equal(t, runtime, got)
	}
	for _, id := range []string{"104anpd001", "ANPD-01", "ANPD001", "104bakd01", "BAKD01"} {
		_, err := store.LookupMovie(context.Background(), id)
		require.ErrorIs(t, err, models.ErrDumpMiss)
	}
}

func TestServiceVariants_ExactAmbiguityCannotFallThroughToOtherDVD(t *testing.T) {
	const dump = "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\n" +
		"exact00001\tFIRST-001\tdigital\nexact00001\tSECOND-001\trental\nother00001\texact00001\tdigital\n\\.\n"
	path := filepath.Join(t.TempDir(), "exact.db")
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{RequireAllRows: true})
	require.NoError(t, err)
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	_, err = store.LookupByDVDID(context.Background(), "exact00001")
	require.ErrorIs(t, err, models.ErrDumpMiss)
	_, err = store.MatchByDisplayID(context.Background(), "exact00001")
	require.ErrorIs(t, err, models.ErrDumpMiss)
	_, err = store.LookupMovie(context.Background(), "exact00001")
	require.ErrorIs(t, err, models.ErrDumpMiss)
}

func TestServiceVariants_CrossBatchAndCopyIdentity(t *testing.T) {
	const header = "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\n"
	for _, secondService := range []string{"rental", "digital"} {
		var dump strings.Builder
		dump.WriteString(header)
		for i := 0; i < importBatchSize; i++ {
			fmt.Fprintf(&dump, "cross%03d\tTEST-%03d\tdigital\n", i, i)
		}
		dump.WriteString("\\.\n" + header + "cross000\tOTHER-001\t" + secondService + "\n\\.\n")
		if secondService == "digital" {
			assertConstraintImportPreservesDump(t, dump.String(), derivedVideoTable)
			continue
		}
		result, err := Import(context.Background(), strings.NewReader(dump.String()), filepath.Join(t.TempDir(), "cross.db"), ImportOptions{RequireAllRows: true})
		require.NoError(t, err)
		require.EqualValues(t, importBatchSize+1, result.Rows)
	}
}
