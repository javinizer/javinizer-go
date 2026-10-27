package r18dev

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
)

func TestOnlyDumpAdapterSuppliesExactDerivedActressReferences(t *testing.T) {
	s := &scraper{}
	result := &models.ScraperResult{Source: "r18dev"}
	s.resolveDumpActresses(&models.DumpMovie{Actresses: []models.DumpActress{{ID: "7"}, {ID: "007"}, {ID: "opaque-A"}}}, result)
	require.Equal(t, []string{"7", "007", "opaque-A"}, result.R18DumpActorReferences())
	require.Equal(t, 7, result.Actresses[0].DMMID)
	require.Equal(t, 7, result.Actresses[1].DMMID)
	require.Zero(t, result.Actresses[2].DMMID)
	var live r18Response
	require.NoError(t, json.Unmarshal([]byte(`{"actresses":[{"id":7,"name_romaji":"Whole Seven"}]}`), &live))
	fromHTTP := &models.ScraperResult{Source: "r18dev"}
	resolveActresses(&live, fromHTTP)
	require.Nil(t, fromHTTP.R18DumpActorReferences(), "no reviewed HTTP-to-derived_actress contract")
	require.Equal(t, 7, fromHTTP.Actresses[0].DMMID, "legacy behavior remains unchanged")
	empty := &models.ScraperResult{}
	s.resolveDumpActresses(&models.DumpMovie{}, empty)
	require.NotNil(t, empty.R18DumpActorReferences())
}

func TestRealDumpImportLookupAndScraperPreserveOpaqueActorIDs(t *testing.T) {
	dump := "COPY public.derived_video (content_id, dvd_id, title_en) FROM stdin;\nsynthetic00001\tABC-001\tSynthetic movie\n\\.\n" +
		"COPY public.derived_actress (id, name_romaji, name_kanji) FROM stdin;\n7\tWhole Seven\t七\n007\tWhole Zero Seven\t零七\nopaque-A\tWhole Opaque\t不透明\n\\.\n" +
		"COPY public.derived_video_actress (content_id, actress_id, ordinality) FROM stdin;\nsynthetic00001\t7\t1\nsynthetic00001\t007\t2\nsynthetic00001\topaque-A\t3\n\\.\n"
	path := filepath.Join(t.TempDir(), "synthetic-dump.db")
	_, err := r18devdump.Import(t.Context(), strings.NewReader(dump), path, r18devdump.ImportOptions{RequireAllRows: true})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	s, transport := newScraperWithBlockedHTTP(t, store)
	result, err := s.Search(t.Context(), "ABC-001")
	require.NoError(t, err)
	require.Equal(t, []string{"7", "007", "opaque-A"}, result.R18DumpActorReferences())
	require.Zero(t, transport.count(), "source references never require HTTP")
}

func TestDumpReferenceBudgetDoesNotChangeLegacyCast(t *testing.T) {
	cast := make([]models.DumpActress, 100)
	for i := range cast {
		cast[i].ID = "7"
	}
	r := &models.ScraperResult{}
	(&scraper{}).resolveDumpActresses(&models.DumpMovie{Actresses: cast}, r)
	require.Len(t, r.Actresses, 100)
	require.Len(t, r.R18DumpActorReferences(), 65)
}
