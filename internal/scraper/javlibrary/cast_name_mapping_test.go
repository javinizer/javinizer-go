package javlibrary

import (
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// The fixture intentionally mixes scripts: the UI language does not establish
// whether an individual cast name is romanized.
func TestCastNameScriptMapping(t *testing.T) {
	html, err := os.ReadFile("testdata/cast_name_scripts.html")
	require.NoError(t, err)
	want := []models.ActressInfo{
		{JapaneseName: "依本しおり"},
		{JapaneseName: "田中麻美"},
		{JapaneseName: "さくら"},
		{JapaneseName: "サクラ"},
		{JapaneseName: "ｻｸﾗ"},
		{JapaneseName: "田中　麻美"},
		{FirstName: "RION"},
		{FirstName: "Yorimoto", LastName: "Shiori"},
		{FirstName: "Mary", LastName: "Jane Smith"},
	}
	for _, language := range []string{"ja", "en", "cn", "tw"} {
		t.Run(language, func(t *testing.T) {
			s := &scraper{language: language}
			result, err := s.parseDetailPage(string(html), "TEST-383", "https://example.invalid/"+language+"/?v=fixture", language)
			require.NoError(t, err)
			require.Equal(t, want, result.Actresses)
		})
	}
}

func TestCastNameMappingMatchesVerifiedJapaneseIdentity(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	existing := models.Actress{DMMID: 383, JapaneseName: "依本しおり", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&existing).Error)

	html, err := os.ReadFile("testdata/cast_name_scripts.html")
	require.NoError(t, err)
	for _, language := range []string{"ja", "en", "cn", "tw"} {
		t.Run(language, func(t *testing.T) {
			s := &scraper{language: language}
			info := s.extractActresses(strings.ReplaceAll(string(html), "依本しおり", "&#20381;&#26412;&#12375;&#12362;&#12426;"))[0]
			found, outcome, err := database.ResolveActressIdentityTx(db.DB, &models.Actress{
				FirstName: info.FirstName, LastName: info.LastName, JapaneseName: info.JapaneseName,
			})
			require.NoError(t, err)
			require.Equal(t, database.ResolutionMatched, outcome)
			require.Equal(t, existing.ID, found.ID)
			require.Equal(t, existing.DMMID, found.DMMID)
		})
	}
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var stored models.Actress
	require.NoError(t, db.First(&stored, existing.ID).Error)
	require.Equal(t, existing, stored)
}

// Migration 16 marks preexisting identities verified. Their historical
// JavLibrary field layout must remain linkable when the source shape changes.
func TestCastNameMappingLegacyRescrape(t *testing.T) {
	for _, tc := range []struct{ name, first, last string }{
		{"依本しおり", "依本しおり", ""},
		{"田中　麻美", "田中", "麻美"},
		{"&#20381;&#26412;&#12375;&#12362;&#12426;", "依本しおり", ""},
		{"か\u3099", "が", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			existing := models.Actress{FirstName: tc.first, LastName: tc.last, Verified: true, Origin: database.ActressOriginUser}
			require.NoError(t, db.Create(&existing).Error)
			// The old field shape resolves to the preexisting row.
			old := models.Actress{FirstName: tc.first, LastName: tc.last}
			found, outcome, err := database.ResolveActressIdentityTx(db.DB, &old)
			require.NoError(t, err)
			require.Equal(t, database.ResolutionMatched, outcome)
			require.Equal(t, existing.ID, found.ID)
			upserter := database.NewMovieUpserter(database.NewMovieRepository(db))
			movie := &models.Movie{ContentID: "legacy-383", ID: "LEGACY-383", Title: "Fixture", Credits: []models.MovieCredit{
				{Scraped: old, CreditedName: old.FullName(), Source: "javlibrary", Origin: string(models.CreditOriginScrape)},
			}}
			saved, err := upserter.Upsert(t.Context(), movie)
			require.NoError(t, err)
			require.Len(t, saved.Credits, 1)
			originalCreditID := saved.Credits[0].ID
			require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", originalCreditID).Updates(map[string]any{
				"override_name": "Pinned", "user_override": true, "order_pinned": true, "order_index": 7,
			}).Error)
			info := (&scraper{language: "ja"}).extractActresses(`<span class="star"><a>` + tc.name + `</a></span>`)[0]
			for i := 0; i < 2; i++ {
				rescrape := &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Credits: []models.MovieCredit{
					{Scraped: models.Actress{FirstName: info.FirstName, LastName: info.LastName, JapaneseName: info.JapaneseName},
						CreditedJapaneseName: info.JapaneseName, Source: "javlibrary", Origin: string(models.CreditOriginScrape)},
				}}
				saved, err = upserter.Upsert(t.Context(), rescrape)
				require.NoError(t, err)
				require.Len(t, saved.Credits, 1)
				require.Equal(t, existing.ID, saved.Credits[0].ActressID)
				require.Equal(t, originalCreditID, saved.Credits[0].ID)
				require.Equal(t, "Pinned", saved.Credits[0].OverrideName)
				require.True(t, saved.Credits[0].UserOverride)
				require.True(t, saved.Credits[0].OrderPinned)
				require.Equal(t, 7, saved.Credits[0].OrderIndex)
				require.Len(t, saved.Actresses, 1)
				require.Equal(t, existing.ID, saved.Actresses[0].ID)
			}
			var count int64
			require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			var stored models.Actress
			require.NoError(t, db.First(&stored, existing.ID).Error)
			require.Equal(t, existing, stored)
		})
	}
}
