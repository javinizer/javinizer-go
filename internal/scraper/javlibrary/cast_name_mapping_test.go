package javlibrary

import (
	"os"
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
			info := s.extractActresses(string(html))[0]
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
