package nfo

import (
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func challengeLinkedSource() *models.Movie {
	m := challengeSourceMovie()
	for i := range m.Credits {
		m.Credits[i].MovieContentID = m.ContentID
	}
	return m
}

func challengeParseActors(t *testing.T, actorXML string) (*models.Movie, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/legacy.nfo", []byte(`<movie><title>Synthetic</title><id>CHECK-001</id>`+actorXML+`</movie>`), 0644))
	parsed, err := ParseNFO(fs, "/legacy.nfo")
	if err != nil {
		return nil, err
	}
	return parsed.Movie, nil
}

func TestChallengeNFOLinkRejectsUntrustedClaims(t *testing.T) {
	for _, tc := range []struct{ name, marker, nameText string }{
		{"cross movie", `<javinizer-credit version="1" selection="canonical" movie="other" credit="42"/>`, "Old Alias"},
		{"unknown credit", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="999"/>`, "Old Alias"},
		{"empty movie", `<javinizer-credit version="1" selection="canonical" movie="" credit="42"/>`, "Old Alias"},
		{"missing movie", `<javinizer-credit version="1" selection="canonical" credit="42"/>`, "Old Alias"},
		{"zero credit", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="0"/>`, "Old Alias"},
		{"negative credit", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="-1"/>`, "Old Alias"},
		{"nonnumeric credit", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="bad"/>`, "Old Alias"},
		{"overflow credit", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="184467440737095516160"/>`, "Old Alias"},
		{"missing credit", `<javinizer-credit version="1" selection="canonical" movie="check001"/>`, "Old Alias"},
		{"unknown version", `<javinizer-credit version="2" selection="canonical" movie="check001" credit="42"/>`, "Old Alias"},
		{"missing version", `<javinizer-credit movie="check001" credit="42"/>`, "Old Alias"},
		{"missing selection", `<javinizer-credit version="1" movie="check001" credit="42"/>`, "Old Alias"},
		{"unknown selection", `<javinizer-credit version="1" selection="authoritative" movie="check001" credit="42"/>`, "Old Alias"},
		{"duplicate link", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="42"/><javinizer-credit version="1" selection="canonical" movie="check001" credit="42"/>`, "Old Alias"},
		{"conflicting duplicate link", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="42"/><javinizer-credit version="1" selection="canonical" movie="check001" credit="41"/>`, "Old Alias"},
		{"link disagrees with name owner", `<javinizer-credit version="1" selection="canonical" movie="check001" credit="41"/>`, "Beta Person"},
		{"malformed blank actor", `<javinizer-credit version="bad" selection="canonical" movie="check001" credit="42"/>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := challengeParseActors(t, `<actor><name>`+tc.nameText+`</name>`+tc.marker+`</actor>`)
			if err == nil {
				_, err = MergeMovieMetadataWithOptions(challengeLinkedSource(), parsed, PreferNFO, true)
			}
			require.Error(t, err)
			var blocked *CastAmbiguityError
			require.True(t, errors.As(err, &blocked), "invalid marker must stay a typed actionable block, not an ordinary swallowed parse error: %v", err)
		})
	}
}

func TestChallengeNFOLinkClassifiesHistoricalNamesWithoutRevivingHiddenCredits(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprintf("suppressed=%v", hidden), func(t *testing.T) {
			m := challengeLinkedSource()
			m.Credits[1].OverrideName, m.Credits[1].UserOverride = "Current Alias", true
			m.Credits[1].Suppressed = hidden
			parsed, err := challengeParseActors(t, `<actor><name>Historical Alias</name><javinizer-credit version="1" selection="override" movie="check001" credit="42"/></actor>`)
			require.NoError(t, err)
			merged, err := MergeMovieMetadataWithOptions(m, parsed, PreferNFO, true)
			require.NoError(t, err)
			rendered, names := challengeRenderCast(t, merged.Merged, false)
			if hidden {
				require.Empty(t, rendered.Actors)
				require.Empty(t, names)
			} else {
				require.Len(t, rendered.Actors, 1)
				require.Equal(t, "Current Alias", rendered.Actors[0].Name)
			}
			require.Equal(t, m.Credits, merged.Merged.Credits)
		})
	}
}

func TestChallengeNFOLinkComponentConflictsCannotDependOnRowOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		rows := []string{`<actor><name>Shared Historical</name><javinizer-credit version="1" selection="canonical" movie="check001" credit="41"/></actor>`, `<actor><name>Shared Historical</name><javinizer-credit version="1" selection="canonical" movie="check001" credit="42"/></actor>`}
		if reverse {
			rows[0], rows[1] = rows[1], rows[0]
		}
		parsed, err := challengeParseActors(t, rows[0]+rows[1])
		require.NoError(t, err)
		_, err = MergeMovieMetadataWithOptions(challengeLinkedSource(), parsed, PreferNFO, true)
		var blocked *CastAmbiguityError
		require.True(t, errors.As(err, &blocked))
	}
}

func TestChallengeLegacyGateUsesParserProvenanceNotSourceLabel(t *testing.T) {
	m := challengeLinkedSource()
	m.Credits[1].Origin = "user" // Includes a cleared override.
	parsed, err := challengeParseActors(t, `<actor><name>Unclassified Historical</name></actor>`)
	require.NoError(t, err)
	_, err = MergeMovieMetadataWithOptions(m, parsed, PreferNFO, true)
	var blocked *CastAmbiguityError
	require.True(t, errors.As(err, &blocked))
	// An existing database/review movie is not a parsed NFO just because its
	// ordinary source metadata retained an earlier NFO merge's label.
	existing := &models.Movie{ID: m.ID, ContentID: m.ContentID, SourceName: "nfo", Actresses: []models.Actress{{FirstName: "Other", LastName: "Actor"}}}
	_, err = MergeMovieMetadataWithOptions(m, existing, PreferNFO, true)
	require.NoError(t, err)
}

func TestChallengeNFOLinkRequiresActualCreditMembership(t *testing.T) {
	for _, mode := range []string{"no credits", "wrong credit movie", "deleted credit"} {
		t.Run(mode, func(t *testing.T) {
			m := challengeLinkedSource()
			switch mode {
			case "no credits":
				m.Credits = nil
			case "wrong credit movie":
				m.Credits[1].MovieContentID = "other"
			case "deleted credit":
				m.Credits = m.Credits[:1]
			}
			parsed, err := challengeParseActors(t, `<actor><name>Historical Alias</name><javinizer-credit version="1" selection="override" movie="check001" credit="42"/></actor>`)
			require.NoError(t, err)
			_, err = MergeMovieMetadataWithOptions(m, parsed, PreferNFO, true)
			var blocked *CastAmbiguityError
			require.True(t, errors.As(err, &blocked), "same number without actual source-movie membership cannot become a rendering link")
		})
	}
}

func TestChallengeExistingNFOMergeDoesNotSwallowCastBlocks(t *testing.T) {
	for _, actorXML := range []string{
		`<actor><name>Unclassified Previous Alias</name></actor>`,
		`<actor><name>Historical Alias</name><javinizer-credit version="1" selection="override" movie="check001" credit="bad"/></actor>`,
	} {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/incoming", 0755))
		body := []byte(`<movie><title>Synthetic</title><id>CHECK-001</id>` + actorXML + `</movie>`)
		require.NoError(t, afero.WriteFile(fs, "/incoming/CHECK-001.nfo", body, 0644))
		impl := NewNFOImplementor(fs, &Config{FilenameTemplate: "<ID>.nfo"}, template.NewEngine())
		opts := MergeWithExistingOptions{Match: models.FileMatchInfo{Path: "/incoming/CHECK-001.mp4"}, ScalarStrategy: PreferNFO, ArrayStrategy: true}
		result := impl.MergeWithExistingNFO(challengeLinkedSource(), opts)
		var blocked *CastAmbiguityError
		require.True(t, errors.As(result.Err, &blocked), "typed block cannot downgrade to scraper-only success")
		require.Contains(t, result.Err.Error(), "CHECK-001.nfo")
		after, err := afero.ReadFile(fs, "/incoming/CHECK-001.nfo")
		require.NoError(t, err)
		require.Equal(t, body, after)
		opts.ForceOverwrite = true
		result = impl.MergeWithExistingNFO(challengeLinkedSource(), opts)
		require.NoError(t, result.Err, "explicitly ignoring old NFO is a supported recovery")
	}
}

func TestChallengeGeneratedSharedPresentationRoundTrip(t *testing.T) {
	for _, mode := range []string{"same selected names", "override versus another canonical", "Japanese role bridge"} {
		t.Run(mode, func(t *testing.T) {
			source := challengeLinkedSource()
			source.Credits[0].Suppressed = false
			source.Credits[0].UserOverride = true
			source.Credits[0].OverrideName = "Shared Name"
			source.Credits[1].UserOverride = true
			source.Credits[1].OverrideName = "Shared Name"
			wantRows := 1
			if mode == "override versus another canonical" {
				source.Credits[0].OverrideName = "Beta Person"
				source.Credits[1].OverrideName = "Different Name"
				wantRows = 2
			}
			if mode == "Japanese role bridge" {
				source.Credits[0].OverrideName = "Alpha Selected"
				source.Credits[1].OverrideName = "Beta Selected"
				source.Credits[0].Actress.JapaneseName = source.Credits[1].Actress.JapaneseName
				wantRows = 2
			}
			fs := afero.NewMemMapFs()
			generator := NewGenerator(fs, &Config{FirstNameOrder: true, AltNameRole: mode == "Japanese role bridge"})
			require.NoError(t, generator.GenerateAtPath(t.Context(), source, "/shared.nfo", "", nil))
			parsed, err := ParseNFO(fs, "/shared.nfo")
			require.NoError(t, err)
			require.Len(t, parsed.Movie.Actresses, wantRows)
			_, err = MergeMovieMetadataWithOptions(source, parsed.Movie, PreferNFO, true)
			require.NoError(t, err, "unchanged generated NFO should remain mergeable")
		})
	}
}

func TestChallengeCanonicalNFOPresentationSurvivesGeneratedRoundTrip(t *testing.T) {
	source := challengeLinkedSource()
	// This is an unedited source credit; PreferNFO legitimately imports a name
	// through its known Japanese counterpart. Default output omits alt roles.
	source.Credits = source.Credits[1:]
	source.Actresses = []models.Actress{*source.Credits[0].Actress}
	original, err := challengeParseActors(t, `<actor><name>Imported Display</name><role>乙</role></actor>`)
	require.NoError(t, err)
	merged, err := MergeMovieMetadataWithOptions(source, original, PreferNFO, false)
	require.NoError(t, err)
	fs := afero.NewMemMapFs()
	generator := NewGenerator(fs, &Config{FirstNameOrder: true})
	require.NoError(t, generator.GenerateAtPath(t.Context(), merged.Merged, "/roundtrip.nfo", "", nil))
	generated, err := ParseNFO(fs, "/roundtrip.nfo")
	require.NoError(t, err)
	require.Equal(t, "Imported", generated.Movie.Actresses[0].FirstName)
	next, err := MergeMovieMetadataWithOptions(source, generated.Movie, PreferNFO, false)
	require.NoError(t, err)
	rendered, _ := challengeRenderCast(t, next.Merged, false)
	require.Equal(t, "Imported Display", rendered.Actors[0].Name, "ordinary NFO-selected presentation must remain stable across repeated Apply")
}
