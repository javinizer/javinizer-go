package nfo

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func TestGeneratedCreditLinkSelectionsAndMembership(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		credited, override, force, emptyCanonical bool
		want                                      string
	}{
		{name: "canonical", want: "canonical"}, {name: "override", override: true, want: "override"},
		{name: "credited", credited: true, want: "credited"}, {name: "force", credited: true, force: true, want: "canonical"},
		{name: "fallback", emptyCanonical: true, want: "credited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.Actress{FirstName: "Canonical", Verified: true}
			if tc.emptyCanonical {
				a = nil
			}
			c := models.MovieCredit{ID: 1, MovieContentID: "movie", Actress: a, CreditedName: "Reported", UserOverride: tc.override, OverrideName: "Selected", DisplayForceCanonical: tc.force}
			g := NewGenerator(nil, &Config{FirstNameOrder: true, UseCreditedName: tc.credited})
			rows := g.buildActorsForMovie(&models.Movie{ContentID: "movie", Credits: []models.MovieCredit{c}})
			require.Len(t, rows, 1)
			require.Len(t, rows[0].PresentationLinks, 1)
			require.Equal(t, tc.want, rows[0].PresentationLinks[0].Selection)
			c.MovieContentID = "other"
			rows = g.buildActorsForMovie(&models.Movie{ContentID: "movie", Credits: []models.MovieCredit{c}})
			require.Empty(t, rows[0].PresentationLinks)
		})
	}
}

func TestLinkedGeneratedSelectionsDoNotBroadenCanonicalProtection(t *testing.T) {
	for _, selection := range []string{"canonical", "override", "credited"} {
		m := challengeLinkedSource()
		m.Credits[1].Origin = "user" // An order edit alone must not defeat PreferNFO.
		parsed, err := challengeParseActors(t, `<actor><name>NFO Name</name><role>乙</role><javinizer-credit version="1" movie="check001" credit="42" selection="`+selection+`"/></actor>`)
		require.NoError(t, err)
		merged, err := MergeMovieMetadataWithOptions(m, parsed, PreferNFO, false)
		require.NoError(t, err)
		rendered, _ := challengeRenderCast(t, merged.Merged, false)
		require.Len(t, rendered.Actors, 1)
		want := "Beta Person"
		if selection == "canonical" {
			want = "NFO Name"
		}
		require.Equal(t, want, rendered.Actors[0].Name)
	}
}

func TestLegacyCastGateSelectionAndStrategy(t *testing.T) {
	for _, selection := range []string{"none", "origin", "override", "suppressed", "forced", "configured credited"} {
		for _, replace := range []bool{false, true} {
			t.Run(selection+map[bool]string{false: "/merge", true: "/scraper replace"}[replace], func(t *testing.T) {
				a := models.Actress{FirstName: "Canonical", Verified: true}
				c := models.MovieCredit{Actress: &a, CreditedName: "Reported"}
				switch selection {
				case "origin":
					c.Origin = "user"
				case "override":
					c.UserOverride = true
					c.OverrideName = "Selected"
				case "suppressed":
					c.Suppressed = true
				case "forced":
					c.DisplayForceCanonical = true
				}
				m := &models.Movie{ID: "TEST-001", ContentID: "test001", Credits: []models.MovieCredit{c}, Actresses: []models.Actress{a}}
				parsed, err := challengeParseActors(t, `<actor><name>Unclassified Face</name></actor>`)
				require.NoError(t, err)
				_, err = mergeMovieMetadataWithRenderPolicy(m, parsed, PreferScraper, !replace, selection == "configured credited")
				if selection != "none" && !replace {
					var blocked *CastAmbiguityError
					require.ErrorAs(t, err, &blocked)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
	// Public setter misuse cannot turn unmatched metadata into valid references.
	source := challengeLinkedSource()
	err := bindPresentationLinks(source, nil, [][]models.ActorPresentationLink{{{Version: "1"}}}, nil)
	var blocked *CastAmbiguityError
	require.ErrorAs(t, err, &blocked)
}

func TestNFODurableCreditOwnershipSurvivesLegacyContentIDFallback(t *testing.T) {
	for _, durable := range []bool{false, true} {
		m := challengeLinkedSource()
		if !durable {
			for i := range m.Credits {
				m.Credits[i].ID = 0
			}
		}
		parsed, err := challengeParseActors(t, `<actor><name>Beta Person</name></actor>`)
		require.NoError(t, err)
		require.Equal(t, "CHECK-001", parsed.ContentID)
		merged, err := MergeMovieMetadataWithOptions(m, parsed, PreferNFO, true)
		require.NoError(t, err)
		want, source := "CHECK-001", "nfo"
		if durable {
			want, source = m.ContentID, "scraper"
		}
		require.Equal(t, want, merged.Merged.ContentID)
		require.Equal(t, source, merged.Provenance["ContentID"].Source)
		rows := NewGenerator(nil, &Config{FirstNameOrder: true}).buildActorsForMovie(merged.Merged)
		if durable {
			require.Equal(t, m.ContentID, rows[0].PresentationLinks[0].MovieContentID)
		} else {
			require.Empty(t, rows[0].PresentationLinks)
		}
	}
	parsed, err := challengeParseActors(t, `<actor><name>Beta Person</name><javinizer-credit version="1" selection="canonical" movie="check001" credit="42"/></actor>`)
	require.NoError(t, err)
	_, err = MergeMovieMetadataWithOptions(nil, parsed, PreferNFO, true)
	var ambiguity *CastAmbiguityError
	require.ErrorAs(t, err, &ambiguity)
}

func TestGeneratedCreditLinksRemainMovieScopedAcrossParts(t *testing.T) {
	source := challengeLinkedSource()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/output", 0755))
	cfg := &Config{FilenameTemplate: "<ID>.nfo", PerFile: true, FirstNameOrder: true}
	gen := NewGenerator(fs, cfg)
	for part := 1; part <= 2; part++ {
		path, err := gen.ResolveAndGenerate(t.Context(), source, "/output", cfg.ToNFONameConfig(true, "-pt"+strconv.Itoa(part), part), "", nil)
		require.NoError(t, err)
		parsed, err := ParseNFO(fs, path)
		require.NoError(t, err)
		refs, present := parsed.Movie.NFOActorLinks()
		require.True(t, present)
		require.Len(t, refs, 1)
		require.Equal(t, source.ContentID, refs[0][0].MovieContentID)
		require.Equal(t, strconv.FormatUint(uint64(source.Credits[1].ID), 10), refs[0][0].CreditID)
		updated := source.Clone()
		updated.Credits[1].UserOverride = true
		updated.Credits[1].OverrideName = "Current Alias"
		merged, err := MergeMovieMetadataWithOptions(updated, parsed.Movie, PreferNFO, true)
		require.NoError(t, err)
		rendered, _ := challengeRenderCast(t, merged.Merged, false)
		require.Equal(t, "Current Alias", rendered.Actors[0].Name)
	}
}

func TestGeneratedLinkRequiresMembershipInDurableSourceNotJustProjectionID(t *testing.T) {
	a := models.Actress{FirstName: "Canonical", Verified: true}
	source := models.Movie{ContentID: "movie", Credits: []models.MovieCredit{{ID: 1, MovieContentID: "movie", Actress: &a}}}
	source.SetRenderCredits([]models.MovieCredit{{ID: 2, MovieContentID: "movie", Actress: &a}})
	rows := NewGenerator(nil, &Config{FirstNameOrder: true}).buildActorsForMovie(&source)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].PresentationLinks)
}

func TestNFOCastPreflightCachesReadOnlyResult(t *testing.T) {
	movie := challengeLinkedSource()
	service := NewNFOImplementor(afero.NewMemMapFs(), &Config{}, nil)
	result := service.(CastMergePreflighter).PreflightCreditMerge(movie, MergeWithExistingOptions{ForceOverwrite: true})
	require.NoError(t, result.Err)
	require.Same(t, movie, result.Movie)
	require.False(t, result.Merged)
}
