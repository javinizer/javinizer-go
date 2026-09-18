package scrape

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildCreditsFromScrapePatchBranches(t *testing.T) {
	BuildCreditsFromScrape(nil, nil, nil)

	empty := &models.Movie{}
	BuildCreditsFromScrape(empty, nil, nil)
	assert.NotNil(t, empty.Credits)
	assert.Empty(t, empty.Credits)

	movie := &models.Movie{Actresses: []models.Actress{
		{},
		{DMMID: 7, FirstName: "Canonical", ThumbURL: "canonical.jpg"},
		{DMMID: 7, FirstName: "Duplicate"},
	}}
	results := []*models.ScraperResult{
		nil,
		{Source: "   "},
		{
			Source: " source ",
			Actresses: []models.ActressInfo{
				{DMMID: 7, FirstName: "Reported", ThumbURL: "reported.jpg"},
			},
		},
	}
	BuildCreditsFromScrape(movie, map[string]string{"dmmid:7": " source "}, results)

	assert.Len(t, movie.Credits, 1)
	assert.Equal(t, "Reported", movie.Credits[0].CreditedName)
	assert.Equal(t, "reported.jpg", movie.Credits[0].ReportedThumbURL)
	assert.Equal(t, "source", movie.Credits[0].Source)
}

func TestBuildCreditsFromScrapeUsesAlternateProvenanceKey(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Canonical", LastName: "Person", JapaneseName: "日本名"}}}
	results := []*models.ScraperResult{{
		Source:    "source",
		Actresses: []models.ActressInfo{{FirstName: "Canonical", LastName: "Person", ThumbURL: "reported.jpg"}},
	}}

	BuildCreditsFromScrape(movie, map[string]string{"name:person canonical": "source"}, results)

	assert.Len(t, movie.Credits, 1)
	assert.Equal(t, "source", movie.Credits[0].Source)
	assert.Equal(t, "Person Canonical", movie.Credits[0].CreditedName)
	assert.Equal(t, "reported.jpg", movie.Credits[0].ReportedThumbURL)
}

func TestAttachCreditPolicyPatchBranches(t *testing.T) {
	AttachCreditPolicy(nil, &Config{})
	AttachCreditPolicy(&models.Movie{}, nil)

	movie := &models.Movie{}
	cfg := &Config{
		CollisionPolicy:         "trusted_sources",
		TrustedCollisionSources: []string{"dmm"},
	}
	AttachCreditPolicy(movie, cfg)
	assert.Equal(t, "trusted_sources", movie.CreditPolicy)
	assert.Equal(t, []string{"dmm"}, movie.TrustedCollisionSources)
}

func TestEnrichActressesSkipsCandidatePatchBranch(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 11}}}
	repo := &mockActressRepoForUncovered{findByDMMIDVal: &models.Actress{DMMID: 11, Verified: false}}
	assert.Zero(t, enrichActressesFromDB(t.Context(), movie, repo, &Config{ActressDBEnabled: true}))
}

func TestPostProcessScrapedBuildsCreditsPatchBranch(t *testing.T) {
	movie := &models.Movie{ID: "PATCH-1", Actresses: []models.Actress{{FirstName: "Actor"}}}
	result, err := postProcessScraped(t.Context(), movie, nil, nil, &Config{ScrapeActress: true, CollisionPolicy: "block"}, nil, nil, ScrapeCmd{MovieID: "PATCH-1"}, time.Now())
	assert.NoError(t, err)
	assert.Len(t, result.Movie.Credits, 1)
	assert.Equal(t, "block", result.Movie.CreditPolicy)
}

func TestInfoFullNamePatchBranches(t *testing.T) {
	tests := []struct {
		name string
		info models.ActressInfo
		want string
	}{
		{name: "Japanese only", info: models.ActressInfo{JapaneseName: "  葵  "}, want: "葵"},
		{name: "both names", info: models.ActressInfo{FirstName: " Yui ", LastName: " Hatano "}, want: "Hatano   Yui"},
		{name: "first only", info: models.ActressInfo{FirstName: "  Aoi  "}, want: "Aoi"},
		{name: "last only", info: models.ActressInfo{LastName: "  Aoi  "}, want: "Aoi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, infoFullName(tt.info))
		})
	}
	assert.False(t, actressInfoMatchesKey(models.ActressInfo{FirstName: "Other"}, "missing"))
}
