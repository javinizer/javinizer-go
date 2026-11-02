package translation

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This adapter deliberately violates the provider result contract. It is wired
// through New, not a private service field or helper. The package-local test is
// necessary because TranslatorProvider's result type is not exported.
type missingResultContractProvider struct {
	calls          int
	source, target string
	texts          []string
	result         *translationResult
}

func (*missingResultContractProvider) Name() string { return "openai" }
func (p *missingResultContractProvider) Translate(_ context.Context, source, target string, texts []string) (*translationResult, error) {
	p.calls++
	p.source, p.target = source, target
	p.texts = append([]string(nil), texts...)
	return p.result, nil
}

var _ TranslatorProvider = (*missingResultContractProvider)(nil)

// This is malformed-provider containment, not a claim that the default HTTP
// providers return (nil,nil). Failure must not become a panic, successful empty
// translation, retry storm, or partial replacement of the caller's metadata.
func TestTranslateMovieContainsMissingProviderResultWithoutMutatingMovie(t *testing.T) {
	for _, applyToPrimary := range []bool{false, true} {
		name := "translation record only"
		if applyToPrimary {
			name = "apply to primary"
		}
		t.Run(name, func(t *testing.T) {
			provider := &missingResultContractProvider{}
			cfg := Config{Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en", ApplyToPrimary: applyToPrimary}
			cfg.Fields.Title, cfg.Fields.Description = true, true
			service := New(cfg, provider)
			movie := &models.Movie{
				ContentID: "provider-contract", ID: "PROVIDER-001", Title: "Private original title", Description: "Private original description",
				Genres:       []models.Genre{{ID: 12, Name: "Original genre"}},
				Actresses:    []models.Actress{{ID: 13, JapaneseName: "原名"}},
				Translations: []models.MovieTranslation{{Language: "en", Title: "Existing translation", SettingsHash: "previous"}},
			}
			before := movie.Clone()
			output, warning, code, err := service.TranslateMovie(t.Context(), movie, "current-settings")
			require.Nil(t, output)
			require.EqualError(t, err, "translation provider returned no result")
			require.Equal(t, TranslationWarningUnavailable, code)
			require.Equal(t, "Translation (openai): provider unavailable or unusable response", warning)
			require.NotContains(t, warning, movie.Title)
			require.NotContains(t, warning, movie.Description)
			status, hasStatus := WarningStatusCode(t.Context(), err)
			require.Zero(t, status)
			require.False(t, hasStatus)
			require.Equal(t, before, movie)
			require.Equal(t, 1, provider.calls, "an unusable provider result is not a transient HTTP/LLM parse retry")
			require.Equal(t, "ja", provider.source)
			require.Equal(t, "en", provider.target)
			require.Equal(t, []string{before.Title, before.Description}, provider.texts)

			// A later well-formed result must still work on the same service; the
			// failed request must not poison its plan or consume the original text.
			provider.result = &translationResult{Texts: []string{"Translated title", "Translated description"}}
			output, warning, code, err = service.TranslateMovie(t.Context(), movie, "current-settings")
			require.NoError(t, err)
			require.NotNil(t, output)
			require.NotNil(t, output.Movie)
			require.Empty(t, warning)
			require.Empty(t, code)
			require.Equal(t, 2, provider.calls)
			require.Equal(t, []string{before.Title, before.Description}, provider.texts)
			require.Equal(t, "Translated title", output.Movie.Title)
			require.Equal(t, "Translated description", output.Movie.Description)
			require.Equal(t, "en", output.Movie.Language)
			require.Equal(t, "translation:openai", output.Movie.SourceName)
			require.Equal(t, "current-settings", output.Movie.SettingsHash)
			if applyToPrimary {
				before.Title, before.Description = "Translated title", "Translated description"
			}
			require.Equal(t, before, movie)
		})
	}
}
