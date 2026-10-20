package r18devdump

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mixedCaseContentID = "1223bbspgS01_m"

// Keep the source identity in every key and media path: case-folding an import
// must not silently break joins, alter upstream URLs, or collapse source rows.
func TestContentIDCasing_ImportPreservesSourceAndJoinedMovie(t *testing.T) {
	fixture := strings.NewReplacer("118abw00013", mixedCaseContentID, "118abw013", mixedCaseContentID).Replace(fullDump)
	const sourceURL = "https://example.invalid/Dumps/SourceCase.sql.gz?Version=AbC"
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict_%t", strict), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "dump.db")
			result, err := Import(ctx, strings.NewReader(fixture), path, ImportOptions{
				SourceURL: sourceURL, SourceDate: "2026-10-20", RequireAllRows: strict,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, result.Rows)
			store, err := ValidateSidecar(ctx, path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })

			for _, table := range []string{"videos", "video_actresses", "video_categories", "video_directors", "trailers"} {
				var ids string
				require.NoError(t, store.db.QueryRowContext(ctx, "SELECT group_concat(DISTINCT content_id) FROM "+table).Scan(&ids))
				assert.Equal(t, mixedCaseContentID, ids, table+" must preserve the exact source key")
			}
			for _, field := range []struct{ name, want string }{
				{"sample_url", "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/" + mixedCaseContentID + "/" + mixedCaseContentID + "_dmb_w.mp4"},
				{"jacket_full_url", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "pl"},
				{"jacket_thumb_url", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "ps"},
				{"gallery_full_first", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "jp-1"},
				{"gallery_full_last", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "jp-12"},
				{"gallery_thumb_first", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "-1"},
				{"gallery_thumb_last", "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "-12"},
			} {
				var got string
				require.NoError(t, store.db.QueryRowContext(ctx, "SELECT "+field.name+" FROM videos").Scan(&got))
				assert.Equal(t, field.want, got, field.name)
			}
			stats, err := store.Stats(ctx)
			require.NoError(t, err)
			assert.Equal(t, sourceURL, stats.SourceURL)
			assert.Equal(t, "2026-10-20", stats.SourceDate)

			for _, input := range []string{mixedCaseContentID, strings.ToLower(mixedCaseContentID), strings.ToUpper(mixedCaseContentID)} {
				dvdID, err := store.LookupByContentID(ctx, input)
				require.NoError(t, err, input)
				assert.Equal(t, "ABW-013", dvdID)
				matches, err := store.MatchByDisplayID(ctx, input)
				require.NoError(t, err, input)
				require.Len(t, matches, 1)
				assert.Equal(t, mixedCaseContentID, matches[0].ContentID)
			}
			contentID, err := store.LookupByDVDID(ctx, "ABW-013")
			require.NoError(t, err)
			assert.Equal(t, mixedCaseContentID, contentID)
			movie, err := store.LookupMovie(ctx, "ABW-013")
			require.NoError(t, err)
			assert.Equal(t, &models.DumpMovie{
				ContentID: mixedCaseContentID, DVDID: "ABW-013",
				TitleEn: "You Can't Make A Sound", TitleJa: "Airi Suzumura", Runtime: 182, ReleaseDate: "2020-10-02",
				SampleURL:      "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/" + mixedCaseContentID + "/" + mixedCaseContentID + "_dmb_w.mp4",
				JacketFullURL:  "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "pl",
				JacketThumbURL: "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "ps",
				GalleryFirst:   "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "jp-1",
				GalleryLast:    "digital/video/" + mixedCaseContentID + "/" + mixedCaseContentID + "jp-12",
				SiteID:         "2", ServiceCode: "digital",
				Maker:      &models.DumpNamedEntity{NameEn: "Prestige", NameJa: "プレステージ"},
				Label:      &models.DumpNamedEntity{NameEn: "ABSOLUTELY WONDERFUL", NameJa: "ABSOLUTELY WONDERFUL"},
				Series:     &models.DumpNamedEntity{NameEn: "When You Can't Scream Out...", NameJa: "声が出せない状況で…"},
				Director:   &models.DumpDirector{NameKanji: "監督名", NameKana: "かんとくめい", NameRomaji: "Kantoku Mei"},
				Actresses:  []models.DumpActress{{ID: "1019076", NameRomaji: "Airi Suzumura", ImageURL: "suzumura_airi.jpg", NameKanji: "鈴村あいり", NameKana: "すずむらあいり"}},
				Categories: []models.DumpNamedEntity{{NameEn: "Cheating Wife", NameJa: "寝取り・寝取られ・NTR"}, {NameEn: "Squirting", NameJa: "潮吹き"}},
				TrailerURL: "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/" + mixedCaseContentID + "/" + mixedCaseContentID + "_trailer.mp4",
			}, movie)
		})
	}
}

func TestContentIDCasing_CaseDistinctKeysSurviveAllContentTables(t *testing.T) {
	ids := []string{mixedCaseContentID, strings.ToLower(mixedCaseContentID)}
	var fixture strings.Builder
	for _, table := range []struct{ name, columns, values string }{
		{derivedVideoTable, "dvd_id", "\\N"},
		{derivedVideoActressTable, "actress_id, ordinality", "same-actress\t1"},
		{derivedVideoCategoryTable, "category_id", "same-category"},
		{derivedVideoDirectorTable, "director_id", "same-director"},
		{trailerTable, "url", "https://example.invalid/CasePreserved.mp4"},
	} {
		fmt.Fprintf(&fixture, "COPY public.%s (content_id, %s) FROM stdin;\n", table.name, table.columns)
		for _, id := range ids {
			fmt.Fprintf(&fixture, "%s\t%s\n", id, table.values)
		}
		fixture.WriteString("\\.\n")
	}
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict_%t", strict), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dump.db")
			result, err := Import(context.Background(), strings.NewReader(fixture.String()), path, ImportOptions{
				SourceURL: "https://example.invalid/dump.sql", RequireAllRows: strict,
			})
			require.NoError(t, err)
			require.EqualValues(t, 2, result.Rows)
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			for _, table := range []string{"videos", "video_actresses", "video_categories", "video_directors", "trailers"} {
				for _, id := range ids {
					var count int
					require.NoError(t, store.db.QueryRow("SELECT count(*) FROM "+table+" WHERE content_id COLLATE BINARY = ?", id).Scan(&count))
					assert.Equal(t, 1, count, "%s: %s", table, id)
				}
			}
		})
	}
}

func casingStore(t *testing.T, rows []string, reverse bool) *Store {
	t.Helper()
	rows = append([]string(nil), rows...)
	if reverse {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	path := filepath.Join(t.TempDir(), "dump.db")
	fixture := "COPY public.derived_video (content_id, dvd_id, release_date, service_code) FROM stdin;\n" + strings.Join(rows, "\n") + "\n\\.\n"
	result, err := Import(context.Background(), strings.NewReader(fixture), path, ImportOptions{SourceURL: "https://example.invalid/dump.sql", RequireAllRows: true})
	require.NoError(t, err)
	require.EqualValues(t, len(rows), result.Rows)
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func TestContentIDCasing_ExactRowsWinAndAmbiguousAliasesMiss(t *testing.T) {
	for _, dvds := range []struct{ name, lower, upper string }{
		{"different_DVDs", "FIRST-001", "SECOND-002"},
		{"lower_NULL", "\\N", "LULU-441"},
		{"upper_NULL", "LULU-441", "\\N"},
		{"both_NULL", "\\N", "\\N"},
		{"same_DVD", "LULU-441", "LULU-441"},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", dvds.name, reverse), func(t *testing.T) {
				store := casingStore(t, []string{
					"lulu00441\t" + dvds.lower + "\t2026-01-01\tdigital",
					"LULU00441\t" + dvds.upper + "\t2026-02-02\tmono",
				}, reverse)
				ctx := context.Background()
				for _, row := range []models.DumpMatch{
					{ContentID: "lulu00441", DVDID: dvds.lower, ReleaseDate: "2026-01-01", ServiceCode: "digital"},
					{ContentID: "LULU00441", DVDID: dvds.upper, ReleaseDate: "2026-02-02", ServiceCode: "mono"},
				} {
					dvdID, err := store.LookupByContentID(ctx, row.ContentID)
					if row.DVDID == `\N` {
						require.ErrorIs(t, err, models.ErrDumpNoDVDID, "exact NULL row must not borrow its sibling's DVD ID")
						require.Empty(t, dvdID)
						row.DVDID = ""
					} else {
						require.NoError(t, err)
						assert.Equal(t, row.DVDID, dvdID)
					}
					matches, err := store.MatchByDisplayID(ctx, row.ContentID)
					require.NoError(t, err)
					assert.Equal(t, []models.DumpMatch{row}, matches)
				}
				// Neither raw spelling is this third casing. A generated lowercase
				// candidate must not turn an ambiguous alias into an exact hit.
				dvdID, err := store.LookupByContentID(ctx, "LuLu00441")
				require.ErrorIs(t, err, models.ErrDumpMiss)
				assert.NotErrorIs(t, err, models.ErrDumpNoDVDID)
				assert.Empty(t, dvdID)
				matches, err := store.MatchByDisplayID(ctx, "LuLu00441")
				require.ErrorIs(t, err, models.ErrDumpMiss)
				assert.Empty(t, matches)
			})
		}
	}
}

func TestContentIDCasing_SameDVDRequiresExactRawIdentity(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse_%t", reverse), func(t *testing.T) {
			store := casingStore(t, []string{"abc001\tABC-001\t2026-01-01\tdigital", "AbC001\tABC-001\t2026-02-02\tmono"}, reverse)
			ctx := context.Background()
			for _, input := range []string{"ABC-001", "ABC001", "ABC-00001"} {
				id, err := store.LookupByDVDID(ctx, input)
				require.ErrorIs(t, err, models.ErrDumpMiss, input)
				assert.Empty(t, id)
				movie, err := store.LookupMovie(ctx, input)
				require.ErrorIs(t, err, models.ErrDumpMiss, input)
				assert.Nil(t, movie)
				matches, err := store.MatchByDisplayID(ctx, input)
				require.ErrorIs(t, err, models.ErrDumpMiss, input)
				assert.Empty(t, matches)
			}
			for _, raw := range []string{"abc001", "AbC001"} {
				id, err := store.LookupByDVDID(ctx, raw)
				require.NoError(t, err)
				assert.Equal(t, raw, id)
				movie, err := store.LookupMovie(ctx, raw)
				require.NoError(t, err)
				assert.Equal(t, raw, movie.ContentID)
			}
		})
	}
}

func TestContentIDCasing_DistinctDVDIDsRemainAuthoritative(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse_%t", reverse), func(t *testing.T) {
			store := casingStore(t, []string{"lulu00441\tFIRST-001\t2026-01-01\tdigital", "LULU00441\tSECOND-002\t2026-02-02\tmono"}, reverse)
			ctx := context.Background()
			for _, row := range []models.DumpMatch{
				{ContentID: "lulu00441", DVDID: "FIRST-001", ReleaseDate: "2026-01-01", ServiceCode: "digital"},
				{ContentID: "LULU00441", DVDID: "SECOND-002", ReleaseDate: "2026-02-02", ServiceCode: "mono"},
			} {
				id, err := store.LookupByDVDID(ctx, row.DVDID)
				require.NoError(t, err)
				assert.Equal(t, row.ContentID, id)
				movie, err := store.LookupMovie(ctx, row.DVDID)
				require.NoError(t, err)
				assert.Equal(t, row.ContentID, movie.ContentID)
				assert.Equal(t, row.DVDID, movie.DVDID)
				matches, err := store.MatchByDisplayID(ctx, row.DVDID)
				require.NoError(t, err)
				assert.Equal(t, []models.DumpMatch{row}, matches)
			}
		})
	}
}

func TestContentIDCasing_PublishedDVDWinsOverAmbiguousLiteralAlias(t *testing.T) {
	for _, owner := range []string{"abc001", "ABC001"} {
		other := "ABC001"
		if owner == other {
			other = "abc001"
		}
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", owner, reverse), func(t *testing.T) {
				store := casingStore(t, []string{owner + "\tABC-001\t2026-01-01\tdigital", other + "\tOTHER-002\t2026-02-02\tmono"}, reverse)
				ctx := context.Background()
				const display = "AbC001"
				// A content-ID-only lookup cannot choose between these raw identities.
				_, err := store.LookupByContentID(ctx, display)
				require.ErrorIs(t, err, models.ErrDumpMiss)
				// A display lookup has independent, authoritative published DVD evidence.
				id, err := store.LookupByDVDID(ctx, display)
				require.NoError(t, err)
				assert.Equal(t, owner, id)
				movie, err := store.LookupMovie(ctx, display)
				require.NoError(t, err)
				assert.Equal(t, owner, movie.ContentID)
				assert.Equal(t, "ABC-001", movie.DVDID)
				matches, err := store.MatchByDisplayID(ctx, display)
				require.NoError(t, err)
				assert.Equal(t, []models.DumpMatch{{ContentID: owner, DVDID: "ABC-001", ReleaseDate: "2026-01-01", ServiceCode: "digital"}}, matches)
				// A literal raw identity still wins over the conflicting DVD spelling.
				matches, err = store.MatchByDisplayID(ctx, other)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				assert.Equal(t, "OTHER-002", matches[0].DVDID)
				movie, err = store.LookupMovie(ctx, other)
				require.NoError(t, err)
				assert.Equal(t, other, movie.ContentID)
				assert.Equal(t, "OTHER-002", movie.DVDID)
			})
		}
	}
}

func TestContentIDCasing_UniqueGeneratedAliasReturnsRawID(t *testing.T) {
	store := casingStore(t, []string{"LuLu00441\t\\N\t2026-01-01\tdigital"}, false)
	matches, err := store.MatchByDisplayID(context.Background(), "LULU-441")
	require.NoError(t, err)
	assert.Equal(t, []models.DumpMatch{{ContentID: "LuLu00441", ReleaseDate: "2026-01-01", ServiceCode: "digital"}}, matches)
	_, err = store.LookupByContentID(context.Background(), "lulu00441")
	require.ErrorIs(t, err, models.ErrDumpNoDVDID)
	_, err = store.LookupMovie(context.Background(), "LuLu00441")
	require.ErrorIs(t, err, models.ErrDumpMiss, "an exact null-DVD row is still only candidate evidence")
}

func TestContentIDCasing_FoldingMatchesSQLiteASCIIOnly(t *testing.T) {
	assert.Equal(t, "abcÄÖİß_001", foldContentID("AbCÄÖİß_001"))
	store := casingStore(t, []string{"ÄBC001\tFIRST-001\t2026-01-01\tdigital", "äbc001\tSECOND-002\t2026-02-02\tmono"}, false)
	for _, tc := range []struct{ input, dvd string }{{"Äbc001", "FIRST-001"}, {"äBC001", "SECOND-002"}} {
		dvdID, err := store.LookupByContentID(context.Background(), tc.input)
		require.NoError(t, err)
		assert.Equal(t, tc.dvd, dvdID)
	}
}

func TestContentIDCasing_CopiedSidecarWithoutFoldIndexRemainsCompatible(t *testing.T) {
	path := seedDump(t, mixedCaseContentID+"\tSOURCE-001")
	alterFixture(t, path, "DROP INDEX idx_videos_content_id_fold")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	copied := filepath.Join(t.TempDir(), "copied-old-sidecar.db")
	require.NoError(t, os.WriteFile(copied, data, 0o600))
	store, err := ValidateSidecar(context.Background(), copied)
	require.NoError(t, err, "the folded index is additive, not required for imported/copied sidecars")
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	var count int
	require.NoError(t, store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'idx_videos_content_id_fold'").Scan(&count))
	assert.Zero(t, count, "read-only validation must not silently migrate the file")
	dvdID, err := store.LookupByContentID(context.Background(), strings.ToLower(mixedCaseContentID))
	require.NoError(t, err)
	assert.Equal(t, "SOURCE-001", dvdID)
	matches, err := store.MatchByDisplayID(context.Background(), strings.ToUpper(mixedCaseContentID))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, mixedCaseContentID, matches[0].ContentID)
}

func TestContentIDCasing_ImportedLookupPlansUseIndexes(t *testing.T) {
	store := casingStore(t, []string{"lulu00441\tFIRST-001\t2026-01-01\tdigital", "LULU00441\tSECOND-002\t2026-02-02\tmono"}, false)
	var unique int
	require.NoError(t, store.db.QueryRow(`SELECT "unique" FROM pragma_index_list('videos') WHERE name = 'idx_videos_content_id_fold'`).Scan(&unique))
	assert.Zero(t, unique, "folded aliases must never impose a new source-identity uniqueness constraint")
	for _, tc := range []struct{ name, query, index string }{
		{"exact", "SELECT content_id, dvd_id, release_date, service_code FROM videos WHERE content_id COLLATE BINARY IN (?, ?)", "sqlite_autoindex_videos_1"},
		{"folded", "SELECT content_id, dvd_id, release_date, service_code FROM videos WHERE content_id COLLATE NOCASE IN (?, ?)", "idx_videos_content_id_fold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := store.db.Query("EXPLAIN QUERY PLAN "+tc.query, "LuLu00441", "absent001")
			require.NoError(t, err)
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
				details = append(details, detail)
			}
			require.NoError(t, rows.Err())
			plan := strings.Join(details, "\n")
			assert.Contains(t, plan, "SEARCH videos USING INDEX "+tc.index)
			assert.NotContains(t, strings.ToUpper(plan), "SCAN VIDEOS", "an imported multi-million-row sidecar must not scan for a folded lookup")
		})
	}
}
