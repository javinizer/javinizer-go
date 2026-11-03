package translation

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This package-local adapter uses the ordinary provider interface and no HTTP
// client. Its retryable parse failure represents a provider response, not a
// cancellation synthesized by the provider itself.
type retryCancellationContractProvider struct {
	success bool
	inputs  [][]string
}

func (*retryCancellationContractProvider) Name() string { return "openai" }

func (p *retryCancellationContractProvider) Translate(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
	p.inputs = append(p.inputs, append([]string(nil), texts...))
	if p.success {
		return &translationResult{Texts: []string{"Recovered title", "Recovered description"}}, nil
	}
	return &translationResult{RawLLM: "incomplete provider response"}, &translationError{
		Kind: TranslationErrorParse, Message: "provider response lacks output markers",
	}
}

var _ TranslatorProvider = (*retryCancellationContractProvider)(nil)

func TestTranslateMovieCancellationDuringRetryBackoffPreservesAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &retryCancellationContractProvider{}
		cfg := Config{Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en", ApplyToPrimary: true}
		cfg.Fields.Title, cfg.Fields.Description = true, true
		service := New(cfg, provider)
		movie := &models.Movie{
			ID: "RETRY-001", ContentID: "retry00001", Title: "原題", Description: "元の説明",
			Genres:       []models.Genre{{ID: 31, Name: "Original genre"}},
			Actresses:    []models.Actress{{ID: 32, JapaneseName: "元の名前"}},
			Translations: []models.MovieTranslation{{Language: "en", Title: "Existing title", SettingsHash: "old"}},
		}
		before := movie.Clone()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var output *TranslationOutput
		var warning string
		var code TranslationWarningCode
		var err error
		// Bound cleanup even if a regression prevents cancellation from joining.
		// This timeout is virtual inside the bubble, never a wall-clock sleep.
		join := func() bool {
			select {
			case <-done:
				return true
			case <-time.After(time.Second):
				t.Error("translation did not join after cancellation")
				return false
			}
		}
		defer func() { cancel(); join() }()
		started := time.Now()
		go func() {
			defer close(done)
			output, warning, code, err = service.TranslateMovie(ctx, movie, "new-settings")
		}()
		// Wait for durable blocking, not elapsed wall time. The provider cannot
		// block; the service's retry timer is the only outstanding operation.
		// synctest also fixes New's time-based jitter seed without private writes.
		synctest.Wait()
		require.Equal(t, started, time.Now(), "no backoff timer should have elapsed")
		require.Equal(t, [][]string{{before.Title, before.Description}}, provider.inputs)
		select {
		case <-done:
			t.Fatal("translation returned instead of waiting for retry backoff")
		default:
		}
		cancel()
		synctest.Wait()
		require.True(t, join())
		require.Equal(t, started, time.Now(), "cancellation must join before any virtual timeout or retry")
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, output)
		require.Empty(t, warning, "user cancellation must not attach a provider warning")
		require.Empty(t, code)
		require.Equal(t, before, movie)
		require.Len(t, provider.inputs, 1, "cancellation must prevent the second provider call")

		provider.success = true
		output, warning, code, err = service.TranslateMovie(t.Context(), movie, "recovered-settings")
		require.NoError(t, err)
		require.Empty(t, warning)
		require.Empty(t, code)
		require.NotNil(t, output)
		require.NotNil(t, output.Movie)
		require.Equal(t, [][]string{{before.Title, before.Description}, {before.Title, before.Description}}, provider.inputs)
		require.Equal(t, "Recovered title", output.Movie.Title)
		require.Equal(t, "Recovered description", output.Movie.Description)
		require.Equal(t, "recovered-settings", output.Movie.SettingsHash)
		before.Title, before.Description = "Recovered title", "Recovered description"
		require.Equal(t, before, movie, "retry must alter only the selected primary fields")
	})
}

func TestCompactParserRequiresInitialMarkerAndRetainsOrderOnRetry(t *testing.T) {
	// Direct helper validation is intentional: the higher-level parser only
	// selects compact mode after finding marker zero. Do not claim that caller
	// exercises this missing-initial-marker branch.
	for _, payload := range []string{"", "plain provider prose", "<<<JZ_1>>>later item"} {
		got, err := parseCompactTranslationPayload(payload, 2)
		require.EqualError(t, err, "failed to parse compact translation payload: missing output marker 0")
		require.Nil(t, got, "never publish a partial translation from a malformed response")
	}
	got, err := parseCompactTranslationPayload("<<<JZ_0>>> 日本語 🌸 \n<<<JZ_1>>> deuxième élément ", 2)
	require.NoError(t, err)
	require.Equal(t, []string{"日本語 🌸", "deuxième élément"}, got)
}
