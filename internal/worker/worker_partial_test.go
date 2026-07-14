package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- scrapeResultToMovieResult: nil result (line 75) ---

func TestScrapeResultToMovieResult_NilResult_Partial(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "test.mp4", MovieID: "TEST-001"}
	mr, prov := scrapeResultToMovieResult(fmi, nil, nil, false)
	assert.Nil(t, mr)
	assert.Nil(t, prov)
}

// --- scrapeResultToMovieResult: with OrchestrationMeta (line 91) ---

func TestScrapeResultToMovieResult_WithMeta_Partial(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "test.mp4", MovieID: "TEST-001"}
	result := &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "TEST-001"},
	}
	meta := &workflow.OrchestrationMeta{}
	meta.DisplayTitleApplied = true
	mr, prov := scrapeResultToMovieResult(fmi, result, meta, false)
	require.NotNil(t, mr)
	assert.Equal(t, "TEST-001", mr.FileMatchInfo.MovieID)
	assert.True(t, mr.DisplayTitleApplied)
	assert.Nil(t, prov) // no field/actress sources
}

// --- scrapeResultToMovieResult: with provenance (line 91+) ---

func TestScrapeResultToMovieResult_WithProvenance_Partial(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "test.mp4", MovieID: "TEST-001"}
	result := &scrape.ScrapeResult{
		Movie:          &models.Movie{ID: "TEST-001"},
		FieldSources:   map[string]string{"title": "dmm"},
		ActressSources: map[string]string{"actress1": "r18dev"},
	}
	mr, prov := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	require.NotNil(t, prov)
	assert.Equal(t, "dmm", prov.FieldSources["title"])
	assert.Equal(t, "r18dev", prov.ActressSources["actress1"])
}

// --- scrapeResultToMovieResult: OriginalFileName populated from FileMatchInfo
// (regression: the NFO <FILENAME> tag was empty after the workflow-seam refactor
// dropped movie.OriginalFileName = filepath.Base(filePath)) ---

func TestScrapeResultToMovieResult_OriginalFileNameFromFMI(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "/media/IPX-535.mp4", Name: "IPX-535.mp4"}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "IPX-535"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "IPX-535.mp4", mr.Movie.OriginalFileName)
}

func TestScrapeResultToMovieResult_PreservesExistingOriginalFileName(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "/media/IPX-535.mp4", Name: "IPX-535.mp4"}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "IPX-535", OriginalFileName: "custom.mp4"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	assert.Equal(t, "custom.mp4", mr.Movie.OriginalFileName)
}

func TestScrapeResultToMovieResult_NoOriginalFileNameWhenFMINameEmpty(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "test.mp4"} // Name empty
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "TEST-001"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	assert.Empty(t, mr.Movie.OriginalFileName)
}

// --- end-to-end: <FILENAME> template tag resolves from scrapeResultToMovieResult
// (connects the worker conversion to the template engine so a future refactor
// can't silently re-break the NFO <FILENAME> tag, as PR #35 did) ---

func TestScrapeResultToMovieResult_FilenameTemplateEndToEnd(t *testing.T) {
	fmi := models.FileMatchInfo{Path: "/media/IPX-535.mp4", Name: "IPX-535.mp4"}
	result := &scrape.ScrapeResult{Movie: &models.Movie{ID: "IPX-535"}}
	mr, _ := scrapeResultToMovieResult(fmi, result, nil, false)
	require.NotNil(t, mr)
	require.NotNil(t, mr.Movie)

	eng := template.NewEngine()
	ctx := template.NewContextFromMovie(mr.Movie)

	got, err := eng.Execute("<FILENAME>.nfo", ctx)
	require.NoError(t, err)
	assert.Equal(t, "IPX-535.nfo", got) // <FILENAME> strips the extension

	gotExt, err := eng.Execute("<FILENAME_EXT>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "IPX-535.mp4", gotExt) // <FILENAME_EXT> keeps the extension
}

// --- Clone: nil MovieResult (line 146) ---

func TestMovieResult_Clone_Nil_Partial(t *testing.T) {
	var mr *MovieResult
	cloned := mr.Clone()
	assert.Nil(t, cloned)
}

// --- Clone: with EndedAt ---

func TestMovieResult_Clone_WithEndedAt_Partial(t *testing.T) {
	now := time.Now()
	mr := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "test.mp4"},
		Status:        models.JobStatusCompleted,
		EndedAt:       &now,
	}
	cloned := mr.Clone()
	require.NotNil(t, cloned)
	require.NotNil(t, cloned.EndedAt)
	assert.Equal(t, now, *cloned.EndedAt)
}

// --- Clone: with Movie ---

func TestMovieResult_Clone_WithMovie_Partial(t *testing.T) {
	mr := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "test.mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001"},
	}
	cloned := mr.Clone()
	require.NotNil(t, cloned)
	require.NotNil(t, cloned.Movie)
	assert.Equal(t, "TEST-001", cloned.Movie.ID)
}

// --- FSCaseCache: isCaseInsensitiveFS with afero mem ---

func TestFSCaseCache_IsCaseInsensitiveFS_MemMapFs_Partial(t *testing.T) {
	// Using an afero MemMapFs, which IS case-sensitive
	cache := NewFSCaseCache(nil)
	// This will use OS filesystem, which on macOS is case-insensitive
	// and on Linux is case-sensitive. Just exercise the path.
	result := cache.IsCaseInsensitive("/tmp")
	_ = result // result varies by OS
}
