package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateMovie_WithAllFields(t *testing.T) {
	svc := New(Config{
		Enabled:        true,
		Provider:       "deepl",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		ApplyToPrimary: true,
		Fields: fieldsConfig{
			Title: true, OriginalTitle: true, Description: true,
			Director: true, Maker: true, Label: true, Series: true,
			Genres: true, Actresses: true,
		},
	}, &coverageMockProvider{name: "deepl", result: &translationResult{Texts: []string{
		"translated title", "translated orig", "translated desc",
		"translated dir", "translated maker", "translated label", "translated series",
		"genre1", "actress1",
	}}})

	movie := &models.Movie{
		Title: "タイトル", OriginalTitle: "オリジナル", Description: "説明",
		Director: "監督", Maker: "メーカー", Label: "レーベル", Series: "シリーズ",
		Genres:    []models.Genre{{Name: "ジャンル1"}},
		Actresses: []models.Actress{{JapaneseName: "女優1"}},
	}

	out, _, err := svc.TranslateMovie(context.Background(), movie, "hash123")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "translated title", movie.Title)
	assert.Equal(t, "genre1", movie.Genres[0].Name)
}

func TestTranslateMovie_ApplyToPrimaryEnabled(t *testing.T) {
	svc := New(Config{
		Enabled: true, Provider: "deepl", SourceLanguage: "ja",
		TargetLanguage: "en", ApplyToPrimary: true, Fields: fieldsConfig{Title: true},
	}, &coverageMockProvider{name: "deepl", result: &translationResult{Texts: []string{"translated"}}})

	movie := &models.Movie{Title: "original"}
	out, _, err := svc.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "translated", movie.Title)
}

func TestTranslateMovie_CountMismatch(t *testing.T) {
	svc := New(Config{
		Enabled: true, Provider: "deepl", SourceLanguage: "ja",
		TargetLanguage: "en", Fields: fieldsConfig{Title: true},
	}, &coverageMockProvider{name: "deepl", result: &translationResult{Texts: []string{"a", "b"}}})

	movie := &models.Movie{Title: "only one"}
	_, _, err := svc.TranslateMovie(context.Background(), movie, "")
	assert.Error(t, err)
}

func TestDeepLProvider_Translate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DeepL-Auth-Key testkey", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"translations\":[{\"text\":\"hello\"},{\"text\":\"world\"}]}"))
	}))
	defer server.Close()

	p := NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	result, err := p.Translate(context.Background(), "en", "de", []string{"hallo", "welt"})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello", "world"}, result.Texts)
}

func TestDeepLProvider_Translate_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer server.Close()

	p := NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	_, err := p.Translate(context.Background(), "en", "de", []string{"hello"})
	assert.Error(t, err)
}

func TestAnthropicProvider_Translate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "testkey", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"content\":[{\"type\":\"text\",\"text\":\"[\\\"translated\\\"]\"}]}"))
	}))
	defer server.Close()

	p := NewAnthropicProvider(Config{Anthropic: anthropicConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	result, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, []string{"translated"}, result.Texts)
}

func TestAnthropicProvider_Translate_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer server.Close()

	p := NewAnthropicProvider(Config{Anthropic: anthropicConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	_, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	assert.Error(t, err)
}

func TestOpenAIProvider_Translate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer testkey", r.Header.Get("Authorization"))
		resp := openAIChatResponse{}
		content, _ := json.Marshal([]string{"translated"})
		resp.Choices = []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Content json.RawMessage `json:"content"`
		}{Content: content}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOpenAIProvider(Config{OpenAI: openAIConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	result, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, []string{"translated"}, result.Texts)
}

func TestOpenAIProvider_Translate_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	p := NewOpenAIProvider(Config{OpenAI: openAIConfig{APIKey: "testkey", BaseURL: server.URL}}, server.Client())
	_, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	assert.Error(t, err)
}

func TestGoogleProvider_Translate_FreeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := []any{
			[]any{[]any{"hello", "en"}},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	p := NewGoogleProvider(Config{Google: googleConfig{BaseURL: server.URL}}, server.Client())
	result, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, result.Texts)
}

func TestGoogleProvider_Translate_FreeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer server.Close()

	p := NewGoogleProvider(Config{Google: googleConfig{BaseURL: server.URL}}, server.Client())
	_, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	assert.Error(t, err)
}

func TestGoogleProvider_Translate_PaidSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"data\":{\"translations\":[{\"translatedText\":\"hello\"}]}}"))
	}))
	defer server.Close()

	p := NewGoogleProvider(Config{Google: googleConfig{Mode: models.GoogleModePaid, APIKey: "key", BaseURL: server.URL}}, server.Client())
	result, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, result.Texts)
}

func TestLooksLikeOllamaBaseURL_HostMatch(t *testing.T) {
	assert.True(t, looksLikeOllamaBaseURL("http://localhost:11434/v1"))
	assert.True(t, looksLikeOllamaBaseURL("http://my-ollama-server:11434"))
	assert.False(t, looksLikeOllamaBaseURL("http://localhost:8080"))
}

func TestLooksLikeOllamaBaseURL_InvalidURL(t *testing.T) {
	assert.False(t, looksLikeOllamaBaseURL("://not-a-url"))
}

func TestLooksLikeLlamaCppBackend(t *testing.T) {
	assert.True(t, looksLikeLlamaCppBackend("http://llama-server:8080", ""))
	assert.True(t, looksLikeLlamaCppBackend("", "model.gguf"))
	assert.True(t, looksLikeLlamaCppBackend("", "my-gguf-model"))
	assert.False(t, looksLikeLlamaCppBackend("http://localhost:8080", "gpt-4"))
}

func TestExtractContentString_Empty(t *testing.T) {
	assert.Equal(t, "", extractContentString(nil))
	assert.Equal(t, "", extractContentString(json.RawMessage{}))
}

func TestExtractContentString_String(t *testing.T) {
	raw, _ := json.Marshal("hello world")
	assert.Equal(t, "hello world", extractContentString(raw))
}

func TestExtractContentString_NonString(t *testing.T) {
	raw := json.RawMessage("{\"thinking\":\"...\",\"content\":\"text\"}")
	assert.Equal(t, "{\"thinking\":\"...\",\"content\":\"text\"}", extractContentString(raw))
}

type coverageMockProvider struct {
	name   string
	result *translationResult
	err    error
}

func (m *coverageMockProvider) Name() string { return m.name }
func (m *coverageMockProvider) Translate(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestTranslateMovie_UnsupportedProvider(t *testing.T) {
	svc := New(Config{Enabled: true, Provider: "nonexistent", TargetLanguage: "en", Fields: fieldsConfig{Title: true}})
	_, _, err := svc.TranslateMovie(context.Background(), &models.Movie{Title: "hello"}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestBuildOpenAICompatibleThinkingStrategies_All(t *testing.T) {
	for _, bt := range []string{"vllm", "ollama", "llama.cpp", "other"} {
		strategies := buildOpenAICompatibleThinkingStrategies("http://x", "model", openAICompatibleConfig{BackendType: bt})
		assert.NotEmpty(t, strategies, "backend type %s should have strategies", bt)
	}
}

func TestBuildOpenAICompatibleThinkingStrategies_OllamaAutoDetect(t *testing.T) {
	strategies := buildOpenAICompatibleThinkingStrategies("http://localhost:11434", "model", openAICompatibleConfig{})
	assert.NotEmpty(t, strategies)
	assert.Equal(t, openAICompatibleThinkingStrategyReasoningEffort, strategies[0])
}

func TestBuildOpenAICompatibleThinkingStrategies_LlamaCppAutoDetect(t *testing.T) {
	strategies := buildOpenAICompatibleThinkingStrategies("http://llama-host:8080", "model", openAICompatibleConfig{})
	assert.NotEmpty(t, strategies)
	assert.Equal(t, openAICompatibleThinkingStrategyEnableThinking, strategies[0])
}

func TestParseLLMTranslationPayload_CompactFormat(t *testing.T) {
	payload := "<<<JZ_0>>>\nhello\n<<<JZ_1>>>\nworld"
	result, err := parseLLMTranslationPayload(payload, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"hello", "world"}, result)
}

func TestParseLLMTranslationPayload_CompactMissingMarker(t *testing.T) {
	payload := "<<<JZ_0>>>\nhello"
	_, err := parseLLMTranslationPayload(payload, 2)
	assert.Error(t, err)
}

func TestOpenAICompatibleProvider_Translate_MissingModel(t *testing.T) {
	p := NewOpenAICompatibleProvider(Config{OpenAICompatible: openAICompatibleConfig{BaseURL: "http://localhost"}}, http.DefaultClient)
	_, err := p.Translate(context.Background(), "en", "ja", []string{"hello"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}
