package nfo

import (
	"context"
	"encoding/xml"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func mergeCreditFixture() *models.Movie {
	first := models.Actress{ID: 1, FirstName: "Canonical", LastName: "First", ThumbURL: "https://first.invalid/thumb.jpg", Verified: true, Translations: []models.ActressTranslation{{Language: "en", FirstName: "Translation"}}}
	second := models.Actress{ID: 2, FirstName: "Canonical", LastName: "Second", ThumbURL: "https://second.invalid/thumb.jpg", Verified: true}
	hidden := models.Actress{ID: 3, FirstName: "Suppressed", Verified: true}
	quarantined := models.Actress{ID: 4, FirstName: "Quarantined", AmbiguityQuarantined: true}
	return &models.Movie{
		ID: "CREDIT-001", ContentID: "credit001", Title: "Scraper title",
		// The credit order deliberately differs from the legacy projection.
		Actresses: []models.Actress{first, second, hidden, quarantined},
		Credits: []models.MovieCredit{
			{ID: 20, MovieContentID: "credit001", ActressID: second.ID, Actress: &second, CreditedName: "Reported Second", OrderIndex: 0, OrderPinned: true, DisplayForceCanonical: true},
			{ID: 10, MovieContentID: "credit001", ActressID: first.ID, Actress: &first, CreditedName: "Reported First", OverrideName: "Selected First", UserOverride: true, OrderIndex: 1, Origin: "user", Scraped: first},
			{ID: 30, MovieContentID: "credit001", ActressID: hidden.ID, Actress: &hidden, CreditedName: "Suppressed", Suppressed: true, OrderIndex: 2},
			{ID: 40, MovieContentID: "credit001", ActressID: quarantined.ID, Actress: &quarantined, CreditedName: "Quarantined", OrderIndex: 3},
		},
	}
}

func TestMergePreservesCreditsWhenNFOCastDoesNotContribute(t *testing.T) {
	for _, strategy := range []MergeStrategy{PreferScraper, PreferNFO, PreserveExisting, FillMissingOnly, MergeArrays} {
		for _, mergeArrays := range []bool{false, true} {
			t.Run(string(strategy)+map[bool]string{false: "/replace", true: "/merge"}[mergeArrays], func(t *testing.T) {
				scraped := mergeCreditFixture()
				result, err := MergeMovieMetadataWithOptions(scraped, &models.Movie{ID: scraped.ID, Title: "NFO title"}, strategy, mergeArrays)
				require.NoError(t, err)
				require.Equal(t, scraped.Credits, result.Merged.Credits)
				require.Equal(t, scraped.Actresses, result.Merged.Actresses)
				assertMergedCreditOutput(t, result.Merged)
			})
		}
	}
}

func TestMergePreferScraperCastKeepsFullCreditState(t *testing.T) {
	scraped := mergeCreditFixture()
	nfoMovie := &models.Movie{ID: scraped.ID, Title: "NFO title", Actresses: []models.Actress{{FirstName: "NFO", LastName: "Actor", ThumbURL: "https://nfo.invalid/thumb.jpg"}}}
	before := scraped.Clone()
	result, err := MergeMovieMetadataWithOptions(scraped, nfoMovie, PreferScraper, false)
	require.NoError(t, err)
	require.Equal(t, scraped.Credits, result.Merged.Credits)
	require.Equal(t, scraped.Actresses, result.Merged.Actresses)
	require.Equal(t, "scraper", result.Provenance["Actresses"].Source)
	assertMergedCreditOutput(t, result.Merged)

	// The merge result must not alias credit records, identities, or nested slices.
	result.Merged.Credits[1].OverrideName = "Mutated"
	result.Merged.Credits[1].Actress.FirstName = "Mutated"
	result.Merged.Credits[1].Actress.Translations[0].FirstName = "Mutated"
	result.Merged.Credits[1].Scraped.Translations[0].FirstName = "Mutated"
	require.Equal(t, before, scraped)
}

func assertMergedCreditOutput(t *testing.T, movie *models.Movie) {
	t.Helper()
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{FirstNameOrder: true, UseCreditedName: true, IncludeActressImages: true, Tagline: "<ACTORS>"})
	require.NoError(t, g.GenerateAtPath(context.Background(), movie, "/result.nfo", "", nil))
	data, err := afero.ReadFile(fs, "/result.nfo")
	require.NoError(t, err)
	var rendered Movie
	require.NoError(t, xml.Unmarshal(data, &rendered))
	require.Len(t, rendered.Actors, 2)
	require.Equal(t, "Canonical Second", rendered.Actors[0].Name)
	require.Equal(t, "Selected First", rendered.Actors[1].Name)
	require.Equal(t, 0, rendered.Actors[0].Order)
	require.Equal(t, 1, rendered.Actors[1].Order)
	require.Equal(t, "https://second.invalid/thumb.jpg", rendered.Actors[0].Thumb)
	require.Equal(t, "https://first.invalid/thumb.jpg", rendered.Actors[1].Thumb)
	require.Contains(t, rendered.Tagline, "Canonical Second")
	require.Contains(t, rendered.Tagline, "Selected First")
	require.NotContains(t, rendered.Tagline, "Suppressed")
	require.NotContains(t, rendered.Tagline, "Quarantined")
}

func TestMergePreservesNilEmptyAndHiddenCreditStateWithoutNFOCast(t *testing.T) {
	for _, tc := range []struct {
		name    string
		credits []models.MovieCredit
	}{
		{name: "nil"},
		{name: "empty", credits: []models.MovieCredit{}},
		{name: "all suppressed", credits: []models.MovieCredit{{Suppressed: true, CreditedName: "Hidden"}}},
		{name: "unresolved", credits: []models.MovieCredit{{CreditedName: "Unresolved"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scraped := &models.Movie{ID: "CREDIT-001", Title: "Title", Credits: tc.credits}
			result, err := MergeMovieMetadataWithOptions(scraped, &models.Movie{ID: scraped.ID}, PreferNFO, true)
			require.NoError(t, err)
			require.Equal(t, tc.credits, result.Merged.Credits)
			require.Empty(t, result.Merged.Actresses)
		})
	}
}

func TestMergeCreditPreservationDoesNotHideNFOCastPolicy(t *testing.T) {
	for _, tc := range []struct {
		name          string
		strategy      MergeStrategy
		mergeArrays   bool
		noScraperCast bool
	}{
		{name: "prefer NFO", strategy: PreferNFO},
		{name: "preserve existing", strategy: PreserveExisting},
		{name: "fill missing", strategy: FillMissingOnly},
		{name: "merge arrays", strategy: PreferScraper, mergeArrays: true},
		{name: "NFO fallback for empty scraper cast", strategy: PreferScraper, noScraperCast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scraped := mergeCreditFixture()
			if tc.noScraperCast {
				scraped.Actresses = nil
			}
			nfoMovie := &models.Movie{ID: scraped.ID, Title: "NFO title", Actresses: []models.Actress{{FirstName: "NFO", LastName: "Actor", ThumbURL: "https://nfo.invalid/thumb.jpg"}}}
			legacy := scraped.Clone()
			legacy.Credits = nil
			want, err := MergeMovieMetadataWithOptions(legacy, nfoMovie, tc.strategy, tc.mergeArrays)
			require.NoError(t, err)
			got, err := MergeMovieMetadataWithOptions(scraped, nfoMovie, tc.strategy, tc.mergeArrays)
			require.NoError(t, err)
			require.Equal(t, want.Merged.Actresses, got.Merged.Actresses)
			// The durable ledger remains unchanged; a separate projection includes NFO additions.
			require.Equal(t, scraped.Credits, got.Merged.Credits)
			rendered, present := got.Merged.RenderingCredits()
			require.True(t, present)
			require.Len(t, rendered, 3)
			require.Equal(t, "NFO", rendered[2].Actress.FirstName)
			require.Contains(t, got.Merged.Actresses, nfoMovie.Actresses[0])
		})
	}
}

func TestExistingNFOMergePreservesCreditNameSelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		credited bool
		override string
		force    bool
		want     string
	}{
		{name: "override", override: "Selected Alias", want: "Selected Alias"},
		{name: "credited", credited: true, want: "Reported Alias"},
		{name: "canonical", want: "Canonical Person"},
		{name: "forced canonical", credited: true, force: true, want: "Canonical Person"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/incoming", 0755))
			require.NoError(t, afero.WriteFile(fs, "/incoming/CREDIT-001.nfo", []byte(`<movie><title>Existing</title><id>CREDIT-001</id></movie>`), 0644))
			a := models.Actress{ID: 1, FirstName: "Canonical", LastName: "Person", Verified: true}
			movie := &models.Movie{ID: "CREDIT-001", ContentID: "credit001", Title: "Synthetic", Actresses: []models.Actress{a}, Credits: []models.MovieCredit{{ID: 1, ActressID: 1, Actress: &a, CreditedName: "Reported Alias", OverrideName: tc.override, UserOverride: tc.override != "", DisplayForceCanonical: tc.force}}}
			cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true, UseCreditedName: tc.credited, Tagline: "<ACTORS>"}
			merged := NewNFOImplementor(fs, cfg, template.NewEngine()).MergeWithExistingNFO(movie, MergeWithExistingOptions{Match: models.FileMatchInfo{Path: "/incoming/CREDIT-001.mp4"}, ScalarStrategy: PreferScraper})
			require.True(t, merged.Merged)
			require.NoError(t, NewGenerator(fs, cfg).GenerateAtPath(context.Background(), merged.Movie, "/after.nfo", "", nil))
			data, err := afero.ReadFile(fs, "/after.nfo")
			require.NoError(t, err)
			var rendered Movie
			require.NoError(t, xml.Unmarshal(data, &rendered))
			require.Len(t, rendered.Actors, 1)
			require.Equal(t, tc.want, rendered.Actors[0].Name)
			require.Equal(t, tc.want, rendered.Tagline)
		})
	}
}

func TestExistingNFOMergeKeepsSuppressedCreditHidden(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/incoming", 0755))
	require.NoError(t, afero.WriteFile(fs, "/incoming/AUDIT-002.nfo", []byte(`<movie><title>Existing</title><id>AUDIT-002</id><actor><name>Suppressed Person</name></actor></movie>`), 0644))
	a := models.Actress{ID: 2, FirstName: "Suppressed", LastName: "Person", Verified: true}
	movie := &models.Movie{ID: "AUDIT-002", ContentID: "audit002", Title: "Synthetic", Actresses: []models.Actress{}, Credits: []models.MovieCredit{{ID: 2, ActressID: 2, Actress: &a, CreditedName: "Suppressed Person", Suppressed: true}}}
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	generator := NewGenerator(fs, cfg)
	before, err := generator.movieToNFO(context.Background(), movie, "", "", 0, false, nil)
	require.NoError(t, err)
	require.Empty(t, before.Actors)
	merged := NewNFOImplementor(fs, cfg, template.NewEngine()).MergeWithExistingNFO(movie, MergeWithExistingOptions{Match: models.FileMatchInfo{Path: "/incoming/AUDIT-002.mp4"}, ScalarStrategy: PreferScraper})
	require.True(t, merged.Merged)
	require.NoError(t, generator.GenerateAtPath(context.Background(), merged.Movie, "/after.nfo", "", nil))
	data, err := afero.ReadFile(fs, "/after.nfo")
	require.NoError(t, err)
	var after Movie
	require.NoError(t, xml.Unmarshal(data, &after))
	t.Logf("credits_after=%d actors_after=%+v", len(merged.Movie.Credits), after.Actors)
	require.Empty(t, after.Actors, "stale NFO cannot revive explicitly suppressed credit")
}

func TestMergePreferScraperEmptyProjectionRequiresExplicitSuppression(t *testing.T) {
	for _, tc := range []struct {
		name    string
		credits []models.MovieCredit
		wantNFO bool
	}{
		{name: "absent", wantNFO: true},
		{name: "empty", credits: []models.MovieCredit{}, wantNFO: true},
		{name: "raw unresolved", credits: []models.MovieCredit{{CreditedName: "Reported"}}, wantNFO: true},
		{name: "quarantined", credits: []models.MovieCredit{{Actress: &models.Actress{AmbiguityQuarantined: true}}}, wantNFO: true},
		{name: "suppressed and quarantined", credits: []models.MovieCredit{{Suppressed: true}, {Actress: &models.Actress{AmbiguityQuarantined: true}}}, wantNFO: true},
		{name: "suppressed and unresolved", credits: []models.MovieCredit{{Suppressed: true}, {CreditedName: "Reported"}}, wantNFO: true},
		{name: "all suppressed", credits: []models.MovieCredit{{Suppressed: true}, {Suppressed: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scraped := &models.Movie{ID: "EMPTY-001", Title: "Title", Credits: tc.credits}
			nfoMovie := &models.Movie{ID: scraped.ID, Actresses: []models.Actress{{FirstName: "NFO Actor"}}}
			result, err := MergeMovieMetadataWithOptions(scraped, nfoMovie, PreferScraper, false)
			require.NoError(t, err)
			if tc.wantNFO {
				require.Equal(t, nfoMovie.Actresses, result.Merged.Actresses)
				require.Equal(t, "nfo", result.Provenance["Actresses"].Source)
				require.Equal(t, scraped.Credits, result.Merged.Credits)
			} else {
				require.Empty(t, result.Merged.Actresses)
				require.Equal(t, "empty", result.Provenance["Actresses"].Source)
				require.Equal(t, scraped.Credits, result.Merged.Credits)
			}
			require.Len(t, nfoMovie.Actresses, 1, "source NFO must not be mutated")
		})
	}
}

// Explicit suppression must not silently disable genuinely new NFO cast
// additions in modes that allow importing NFO actors.
func TestMergeSuppressedCastDoesNotChangeNFOPreferredOrArrayPolicies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		strategy    MergeStrategy
		mergeArrays bool
	}{
		{name: "NFO preferred", strategy: PreferNFO},
		{name: "array merge", strategy: PreferScraper, mergeArrays: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scraped := &models.Movie{ID: "EMPTY-001", Title: "Title", Credits: []models.MovieCredit{{Suppressed: true}}}
			nfoMovie := &models.Movie{ID: scraped.ID, Actresses: []models.Actress{{FirstName: "NFO Actor"}}}
			result, err := MergeMovieMetadataWithOptions(scraped, nfoMovie, tc.strategy, tc.mergeArrays)
			require.NoError(t, err)
			require.Equal(t, nfoMovie.Actresses, result.Merged.Actresses)
			require.Equal(t, "nfo", result.Provenance["Actresses"].Source)
			require.Equal(t, scraped.Credits, result.Merged.Credits)
		})
	}
}
