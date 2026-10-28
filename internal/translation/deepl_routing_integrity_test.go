package translation_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/require"
)

const deepLContractKey = "synthetic-deepl-routing-key"

// This transport never delegates to a network transport, even for an unexpected
// request. It records the public provider's wire contract and response ownership.
type deepLContractTransport struct {
	endpoint string
	response string
	requests []*http.Request
	payloads []map[string]json.RawMessage
	closed   int
}

func (f *deepLContractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req.Clone(req.Context()))
	if req.Body != nil {
		defer req.Body.Close()
	}
	if req.Method != http.MethodPost || req.URL.String() != f.endpoint {
		return nil, errors.New("unexpected offline DeepL request")
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		return nil, err
	}
	f.payloads = append(f.payloads, payload)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       &deepLContractBody{Reader: strings.NewReader(f.response), closed: &f.closed},
		Request:    req,
	}, nil
}

type deepLContractBody struct {
	io.Reader
	closed *int
}

func (b *deepLContractBody) Close() error {
	(*b.closed)++
	return nil
}

func preparedDeepLContractConfig(t *testing.T, mode models.DeepLMode, baseURL, source string, actresses bool) translation.Config {
	t.Helper()
	app := config.DefaultConfig(nil, nil)
	app.Metadata.Translation = config.TranslationConfig{
		Enabled: true, Provider: "deepl", SourceLanguage: source, TargetLanguage: "en",
		ApplyToPrimary: false,
		Fields:         config.TranslationFieldsConfig{Title: true, Actresses: actresses},
		DeepL:          config.DeepLTranslationConfig{Mode: mode, BaseURL: baseURL, APIKey: deepLContractKey},
	}
	_, err := config.Prepare(app)
	require.NoError(t, err)
	return translation.ConfigFromApp(app.Metadata.Translation)
}

func assertDeepLContractRequest(t *testing.T, fixture *deepLContractTransport, texts []string, source string) {
	t.Helper()
	require.Len(t, fixture.requests, 1, "one batch, not one billable request per field")
	require.Len(t, fixture.payloads, 1)
	req := fixture.requests[0]
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, fixture.endpoint, req.URL.String())
	require.Nil(t, req.URL.User, "credentials must not be placed in the endpoint")
	require.Empty(t, req.URL.RawQuery)
	require.Equal(t, "DeepL-Auth-Key "+deepLContractKey, req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	payload := fixture.payloads[0]
	var gotTexts []string
	require.NoError(t, json.Unmarshal(payload["text"], &gotTexts))
	require.Equal(t, texts, gotTexts, "selected fields must retain their batch order")
	require.JSONEq(t, `"EN"`, string(payload["target_lang"]))
	if source == "" {
		require.NotContains(t, payload, "source_lang", "auto detection omits source_lang")
		require.Len(t, payload, 2)
	} else {
		require.JSONEq(t, `"`+source+`"`, string(payload["source_lang"]))
		require.Len(t, payload, 3)
	}
	require.Equal(t, 1, fixture.closed, "provider must close the HTTP response body")
}

func TestTranslateMovie_DeepLModeEndpointRouting(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL, endpoint, source, wireSource string
		mode                                        models.DeepLMode
	}{
		{name: "pro_default", mode: models.DeepLModePro, endpoint: "https://api.deepl.com/v2/translate", source: "auto"},
		{name: "free_default", mode: models.DeepLModeFree, endpoint: "https://api-free.deepl.com/v2/translate", source: "auto"},
		{name: "pro_custom", mode: models.DeepLModePro, baseURL: "https://translation.fixture.invalid", endpoint: "https://translation.fixture.invalid/v2/translate", source: "ja", wireSource: "JA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := preparedDeepLContractConfig(t, tc.mode, tc.baseURL, tc.source, false)
			fixture := &deepLContractTransport{endpoint: tc.endpoint, response: `{"translations":[{"text":"Translated title"}]}`}
			provider := translation.NewDeepLProvider(cfg, &http.Client{Transport: fixture})
			service := translation.New(cfg, provider)
			movie := models.Movie{ID: "ROUTE-001", Title: "  元の題名  ", Description: "not selected"}
			original := movie
			output, warning, code, err := service.TranslateMovie(context.Background(), &movie, "routing-settings")
			require.NoError(t, err)
			require.Empty(t, warning)
			require.Empty(t, code)
			require.Equal(t, &translation.TranslationOutput{Movie: &models.MovieTranslation{
				Title: "Translated title", Language: "en", SourceName: "translation:deepl", SettingsHash: "routing-settings",
			}}, output)
			require.Equal(t, original, movie, "ApplyToPrimary=false must preserve source metadata")
			assertDeepLContractRequest(t, fixture, []string{"元の題名"}, tc.wireSource)
		})
	}
}

func TestTranslateMovie_SparseActressIndices(t *testing.T) {
	t.Run("interleaved_blank_names", func(t *testing.T) {
		cfg := preparedDeepLContractConfig(t, models.DeepLModePro, "", "ja", true)
		fixture := &deepLContractTransport{
			endpoint: "https://api.deepl.com/v2/translate",
			response: `{"translations":[{"text":"Translated film"},{"text":"Japanese Cast"},{"text":"Romanized Cast"}]}`,
		}
		service := translation.New(cfg, translation.NewDeepLProvider(cfg, &http.Client{Transport: fixture}))
		movie := models.Movie{
			ID: "SPARSE-001", Title: "  映画の題名  ",
			Actresses: []models.Actress{
				{},
				{ID: 11, DMMID: 101, JapaneseName: "  架空女優  ", Aliases: "source alias", Verified: true},
				{ID: 22, DMMID: 202, FirstName: " \t", LastName: "\n ", JapaneseName: " \t ", NameKey: "blank-source"},
				{ID: 33, DMMID: 303, FirstName: " Hana ", LastName: " Example ", Origin: "fixture"},
			},
		}
		original := movie
		original.Actresses = append([]models.Actress(nil), movie.Actresses...)
		output, warning, code, err := service.TranslateMovie(context.Background(), &movie, "sparse-settings")
		require.NoError(t, err)
		require.Empty(t, warning)
		require.Empty(t, code)
		require.Equal(t, &translation.TranslationOutput{
			Movie: &models.MovieTranslation{Title: "Translated film", Language: "en", SourceName: "translation:deepl", SettingsHash: "sparse-settings"},
			ActressTranslations: []models.ActressTranslationData{
				{ActressIndex: 1, Language: "en", JapaneseName: "Japanese Cast", DisplayName: "Japanese Cast", SourceName: "translation:deepl"},
				{ActressIndex: 3, Language: "en", FirstName: "Romanized", LastName: "Cast", DisplayName: "Romanized Cast", SourceName: "translation:deepl"},
			},
		}, output)
		require.Equal(t, original, movie, "blank entries and all source names/identities must be preserved")
		assertDeepLContractRequest(t, fixture, []string{"映画の題名", "架空女優", "Example Hana"}, "JA")
	})

	t.Run("all_blank_names_do_not_send_empty_batch", func(t *testing.T) {
		cfg := preparedDeepLContractConfig(t, models.DeepLModePro, "", "auto", true)
		fixture := &deepLContractTransport{}
		service := translation.New(cfg, translation.NewDeepLProvider(cfg, &http.Client{Transport: fixture}))
		movie := models.Movie{ID: "SPARSE-002", Title: " \t ", Actresses: []models.Actress{
			{}, {ID: 44, DMMID: 404, FirstName: " ", LastName: "\t", JapaneseName: "\n"},
		}}
		original := movie
		original.Actresses = append([]models.Actress(nil), movie.Actresses...)
		output, warning, code, err := service.TranslateMovie(context.Background(), &movie, "blank-settings")
		require.NoError(t, err)
		require.Empty(t, warning)
		require.Empty(t, code)
		require.Equal(t, &translation.TranslationOutput{}, output)
		require.Empty(t, fixture.requests, "empty cast metadata must never create a billable request")
		require.Zero(t, fixture.closed)
		require.Equal(t, original, movie)
	})
}
