package nfo

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testActressThumb = "https://example.test/actress.jpg"
	testPosterThumb  = "https://example.test/poster.jpg"
)

func actressImagesConfig(include bool) *Config {
	return &Config{
		FilenameTemplate:     "<ID>.nfo",
		FirstNameOrder:       true,
		IncludeActressImages: include,
	}
}

func bridgedActressImagesConfig(include bool) *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.NFO.Feature.IncludeActressImages = include
	return cfg
}

func TestConfigFromAppConfigBridgesIncludeActressImages(t *testing.T) {
	for _, include := range []bool{true, false} {
		t.Run(fmt.Sprintf("include=%t", include), func(t *testing.T) {
			bridged := ConfigFromAppConfig(bridgedActressImagesConfig(include), NFONameConfig{FilenameTemplate: "<ID>.nfo"})
			require.NotNil(t, bridged)
			assert.Equal(t, include, bridged.IncludeActressImages)
		})
	}
}

func TestBuildActorsIncludeActressImagesToggle(t *testing.T) {
	actresses := []models.Actress{{
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
		ThumbURL:     testActressThumb,
		Verified:     true,
	}}
	for _, include := range []bool{true, false} {
		t.Run(fmt.Sprintf("include=%t", include), func(t *testing.T) {
			g := NewGenerator(afero.NewMemMapFs(), actressImagesConfig(include))
			actors := g.buildActors(actresses)
			require.Len(t, actors, 1)
			if include {
				assert.Equal(t, testActressThumb, actors[0].Thumb)
			} else {
				assert.Empty(t, actors[0].Thumb)
			}
		})
	}
}

func TestBuildActorsFromCreditsIncludeActressImagesToggle(t *testing.T) {
	credits := []models.MovieCredit{{
		CreditedName: "Yui Hatano",
		Actress: &models.Actress{
			FirstName: "Yui",
			LastName:  "Hatano",
			ThumbURL:  testActressThumb,
			Verified:  true,
		},
	}}
	for _, include := range []bool{true, false} {
		t.Run(fmt.Sprintf("include=%t", include), func(t *testing.T) {
			g := NewGenerator(afero.NewMemMapFs(), actressImagesConfig(include))
			actors := g.buildActorsFromCredits(credits)
			require.Len(t, actors, 1)
			if include {
				assert.Equal(t, testActressThumb, actors[0].Thumb)
			} else {
				assert.Empty(t, actors[0].Thumb)
			}
		})
	}
}

func TestMergedExistingNFOThumbOmittedWhenActressImagesDisabled(t *testing.T) {
	scraped := []models.Actress{{FirstName: "Yui", LastName: "Hatano", Verified: true}}
	existing := []models.Actress{{FirstName: "Yui", LastName: "Hatano", ThumbURL: testActressThumb}}

	merged := mergeActressSlices(scraped, existing, false)
	require.Len(t, merged, 1)
	require.Equal(t, testActressThumb, merged[0].ThumbURL, "merger still adopts the existing NFO thumb")

	disabled := NewGenerator(afero.NewMemMapFs(), actressImagesConfig(false))
	assert.Empty(t, disabled.buildActors(merged)[0].Thumb, "disabled toggle must not emit thumbs inherited from an existing NFO")

	enabled := NewGenerator(afero.NewMemMapFs(), actressImagesConfig(true))
	assert.Equal(t, testActressThumb, enabled.buildActors(merged)[0].Thumb)
}

func TestGenerateRespectsIncludeActressImages(t *testing.T) {
	for _, include := range []bool{true, false} {
		t.Run(fmt.Sprintf("include=%t", include), func(t *testing.T) {
			fs := afero.NewMemMapFs()
			g := NewGenerator(fs, actressImagesConfig(include))
			movie := &models.Movie{
				ID:           "ABC-123",
				DisplayTitle: "Toggle Test",
				Actresses: []models.Actress{{
					FirstName: "Yui",
					LastName:  "Hatano",
					ThumbURL:  testActressThumb,
					Verified:  true,
				}},
				Poster: models.PosterState{CoverURL: testPosterThumb},
			}

			require.NoError(t, g.Generate(context.Background(), movie, "/output", "", "", nil))
			raw, err := afero.ReadFile(fs, "/output/ABC-123.nfo")
			require.NoError(t, err)
			content := string(raw)

			assert.Contains(t, content, testPosterThumb, "movie poster thumb stays independent of the actress toggle")
			if include {
				assert.Contains(t, content, testActressThumb)
			} else {
				assert.NotContains(t, content, testActressThumb)
			}
		})
	}
}
