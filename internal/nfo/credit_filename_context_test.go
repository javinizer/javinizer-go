package nfo

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func creditFilenameMovie() *models.Movie {
	return &models.Movie{
		ID: "ABC-123",
		Credits: []models.MovieCredit{
			{CreditedName: "Yui Alias", Actress: &models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}},
			{CreditedName: "Aoi Alias", Actress: &models.Actress{FirstName: "Aoi", LastName: "Tsukasa", JapaneseName: "葵つかさ", Verified: true}},
		},
	}
}

func TestResolveNFOFilenameUsesCreditAwareContextForActressTokens(t *testing.T) {
	movie := creditFilenameMovie()
	tests := []struct {
		name string
		cfg  NFONameConfig
		want string
	}{
		{
			name: "credited names enabled for list and singular tokens",
			cfg:  NFONameConfig{FilenameTemplate: "<ACTORS> - <ACTRESS>.nfo", FirstNameOrder: true, UseCreditedName: true},
			want: "Yui Alias, Aoi Alias - Yui Alias.nfo",
		},
		{
			name: "credited names disabled use canonical fallback",
			cfg:  NFONameConfig{FilenameTemplate: "<ACTORS> - <ACTRESS>.nfo", FirstNameOrder: true},
			want: "Yui Hatano, Aoi Tsukasa - Yui Hatano.nfo",
		},
		{
			name: "language preference applies to canonical fallback",
			cfg:  NFONameConfig{FilenameTemplate: "<ACTORS> - <ACTRESS>.nfo", FirstNameOrder: true, ActressLanguageJA: true},
			want: "波多野結衣, 葵つかさ - 波多野結衣.nfo",
		},
		{
			name: "group rendering uses credit population",
			cfg:  NFONameConfig{FilenameTemplate: "<ACTORS> - <ACTRESS>.nfo", FirstNameOrder: true, UseCreditedName: true, GroupActress: true, GroupActressName: "Ensemble"},
			want: "Ensemble - Yui Alias.nfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveNFOFilename(template.NewEngine(), movie, tt.cfg))
		})
	}
}

func TestGeneratedNFOFilenameMatchesResolutionAndDiscoveryWithCredits(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate:  "<ACTORS> - <ACTRESS>.nfo",
		FirstNameOrder:    true,
		UseCreditedName:   true,
		ActressLanguageJA: true,
	}
	generator := NewGenerator(fs, cfg)
	movie := creditFilenameMovie()
	nameCfg := cfg.ToNFONameConfig(false, "", 0)

	wantFilename := "Yui Alias, Aoi Alias - Yui Alias.nfo"
	require.Equal(t, wantFilename, ResolveNFOFilename(template.NewEngine(), movie, nameCfg))

	generatedPath, err := generator.ResolveAndGenerate(context.Background(), movie, "/library", nameCfg, "", nil)
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Join("/library", wantFilename)), generatedPath)
	_, err = fs.Stat(generatedPath)
	require.NoError(t, err)

	resolvedPath, _ := NewNFOImplementor(fs, cfg, template.NewEngine()).ResolveNFOPath("/library", movie, nameCfg, "")
	require.Equal(t, generatedPath, filepath.ToSlash(resolvedPath))

	require.NoError(t, generator.Generate(context.Background(), movie, "/direct", "", "", nil))
	_, err = fs.Stat(filepath.Join("/direct", wantFilename))
	require.NoError(t, err)

	parsed, discoveredPath, err := FindExistingNFO(fs, "/library", movie, nameCfg, "", template.NewEngine())
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, generatedPath, filepath.ToSlash(discoveredPath))
}
