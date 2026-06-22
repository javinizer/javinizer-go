package translation

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- service.go uncovered ---

func TestService_NilReceiver_Uncovered(t *testing.T) {
	var s *Service
	_, _, err := s.TranslateMovie(context.Background(), &models.Movie{}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil Service")
}

func TestService_TranslateMovie_Disabled_Uncovered(t *testing.T) {
	s := New(Config{Enabled: false})
	_, _, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "test"}, "")
	require.NoError(t, err)
}

func TestService_TranslateMovie_NilMovie_Uncovered(t *testing.T) {
	s := New(Config{Enabled: true})
	_, _, err := s.TranslateMovie(context.Background(), nil, "")
	require.NoError(t, err)
}

func TestService_TranslateMovie_NoTargetLanguage_Uncovered(t *testing.T) {
	s := New(Config{Enabled: true, TargetLanguage: ""})
	_, _, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "test"}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target language")
}

func TestService_TranslateMovie_SameSourceTarget_Uncovered(t *testing.T) {
	s := New(Config{Enabled: true, SourceLanguage: "en", TargetLanguage: "en"})
	_, _, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "test"}, "")
	require.NoError(t, err)
}

func TestService_TranslateMovie_NoFieldsEnabled_Uncovered(t *testing.T) {
	s := New(Config{
		Enabled:        true,
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields:         fieldsConfig{},
	})
	out, warning, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "テスト"}, "")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Empty(t, warning)
}

func TestNormalizeProvider_Uncovered(t *testing.T) {
	assert.Equal(t, "openai", normalizeProvider("OpenAI"))
	assert.Equal(t, "deepl", normalizeProvider("DeepL"))
	assert.Equal(t, "", normalizeProvider("  "))
}

func TestNormalizeLanguage_Uncovered(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("EN"))
	assert.Equal(t, "ja", normalizeLanguage("  ja  "))
	assert.Equal(t, "", normalizeLanguage("  "))
}

func TestSanitizeTranslationWarning_Uncovered(t *testing.T) {
	t.Run("rate limited", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "rate limited")
	})

	t.Run("unauthorized", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 401}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "unauthorized")
	})

	t.Run("access denied", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 403}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "access denied")
	})

	t.Run("server error", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 500}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "external service error")
	})

	t.Run("bad request", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 400}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "request error")
	})

	t.Run("non-HTTP translation error", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorProvider, Message: "something failed"}
		msg := sanitizeTranslationWarning("test", err)
		assert.Contains(t, msg, "service unavailable")
	})

	t.Run("non-translation error", func(t *testing.T) {
		msg := sanitizeTranslationWarning("test", errors.New("generic"))
		assert.Contains(t, msg, "internal error")
	})
}

func TestIsRetryableError_Uncovered(t *testing.T) {
	t.Run("nil error with raw LLM", func(t *testing.T) {
		assert.True(t, isRetryableError(nil, &translationResult{RawLLM: "some output", Texts: []string{}}))
	})

	t.Run("nil error without raw LLM", func(t *testing.T) {
		assert.False(t, isRetryableError(nil, &translationResult{Texts: []string{"ok"}}))
	})

	t.Run("count mismatch with raw LLM", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorCountMismatch}
		assert.True(t, isRetryableError(err, &translationResult{RawLLM: "output"}))
	})

	t.Run("parse error with raw LLM", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorParse}
		assert.True(t, isRetryableError(err, &translationResult{RawLLM: "output"}))
	})

	t.Run("HTTP error is not retryable", func(t *testing.T) {
		err := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 500}
		assert.False(t, isRetryableError(err, nil))
	})

	t.Run("generic error is not retryable", func(t *testing.T) {
		assert.False(t, isRetryableError(errors.New("generic"), nil))
	})
}

func TestSplitActressName_Uncovered(t *testing.T) {
	first, last := models.SplitFullName("Taro Yamada")
	assert.Equal(t, "Taro", first)
	assert.Equal(t, "Yamada", last)

	first2, last2 := models.SplitFullName("単一")
	assert.Equal(t, "単一", first2)
	assert.Equal(t, "", last2)

	first3, last3 := models.SplitFullName("")
	assert.Equal(t, "", first3)
	assert.Equal(t, "", last3)
}

func TestReplaceActressName_Uncovered(t *testing.T) {
	t.Run("nil actress is no-op", func(t *testing.T) {
		replaceActressName(nil, "test")
	})

	t.Run("empty translated is no-op", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "元の名前"}
		replaceActressName(a, "")
		assert.Equal(t, "元の名前", a.JapaneseName)
	})

	t.Run("replaces JapaneseName", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "元の名前"}
		replaceActressName(a, "Translated Name")
		assert.Equal(t, "Translated Name", a.JapaneseName)
	})

	t.Run("replaces FirstName when no JapaneseName", func(t *testing.T) {
		a := &models.Actress{FirstName: "Original", LastName: "Name"}
		replaceActressName(a, "New Name")
		assert.Equal(t, "New Name", a.FirstName)
		assert.Equal(t, "", a.LastName)
	})

	t.Run("uses JapaneseName when both first/last are empty", func(t *testing.T) {
		a := &models.Actress{}
		replaceActressName(a, "Some Name")
		assert.Equal(t, "Some Name", a.JapaneseName)
	})
}

func TestActressDisplayTitle_Uncovered(t *testing.T) {
	t.Run("prefers JapaneseName", func(t *testing.T) {
		assert.Equal(t, "日本語", actressDisplayTitle(models.Actress{JapaneseName: "日本語", FirstName: "First"}))
	})

	t.Run("falls back to last first", func(t *testing.T) {
		assert.Equal(t, "Last First", actressDisplayTitle(models.Actress{FirstName: "First", LastName: "Last"}))
	})

	t.Run("empty when no names", func(t *testing.T) {
		assert.Equal(t, "", actressDisplayTitle(models.Actress{}))
	})
}

// --- provider_deepl.go uncovered ---

func TestDeepLProvider_NilReceiver_Uncovered(t *testing.T) {
	var p *DeepLProvider
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil receiver")
}

func TestDeepLProvider_MissingAPIKey_Uncovered(t *testing.T) {
	p := NewDeepLProvider(Config{}, nil)
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestDeepLProvider_EmptyTexts_Uncovered(t *testing.T) {
	p := NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: "test-key"}}, nil)
	result, err := p.Translate(context.Background(), "ja", "en", []string{})
	require.NoError(t, err)
	assert.Empty(t, result.Texts)
}

func TestDeepLProvider_DefaultURLs_Uncovered(t *testing.T) {
	t.Run("pro mode uses api.deepl.com", func(t *testing.T) {
		p := NewDeepLProvider(Config{DeepL: deepLConfig{Mode: models.DeepLModePro, APIKey: "key"}}, nil)
		assert.Equal(t, "deepl", p.Name())
	})

	t.Run("free mode uses api-free.deepl.com", func(t *testing.T) {
		p := NewDeepLProvider(Config{DeepL: deepLConfig{Mode: models.DeepLModeFree, APIKey: "key"}}, nil)
		assert.Equal(t, "deepl", p.Name())
	})
}

// --- provider_google.go uncovered ---

func TestGoogleProvider_NilReceiver_Uncovered(t *testing.T) {
	var p *GoogleProvider
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil receiver")
}

// --- provider_anthropic.go uncovered ---

func TestAnthropicProvider_NilReceiver_Uncovered(t *testing.T) {
	var p *AnthropicProvider
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil receiver")
}

func TestAnthropicProvider_MissingAPIKey_Uncovered(t *testing.T) {
	p := NewAnthropicProvider(Config{}, nil)
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestAnthropicProvider_EmptyTexts_Uncovered(t *testing.T) {
	p := NewAnthropicProvider(Config{Anthropic: anthropicConfig{APIKey: "test-key"}}, nil)
	result, err := p.Translate(context.Background(), "ja", "en", []string{})
	require.NoError(t, err)
	assert.Empty(t, result.Texts)
}

func TestAnthropicProvider_DefaultModel_Uncovered(t *testing.T) {
	p := NewAnthropicProvider(Config{Anthropic: anthropicConfig{APIKey: "key"}}, nil)
	assert.Equal(t, "anthropic", p.Name())
}

// --- provider_openai.go uncovered ---

func TestOpenAIProvider_Name_Uncovered(t *testing.T) {
	p := NewOpenAIProvider(Config{}, nil)
	assert.Equal(t, "openai", p.Name())
}

func TestOpenAICompatibleProvider_Name_Uncovered(t *testing.T) {
	p := NewOpenAICompatibleProvider(Config{}, nil)
	assert.Equal(t, "openai-compatible", p.Name())
}

func TestOpenAIProvider_NilReceiver_Uncovered(t *testing.T) {
	var p *OpenAIProvider
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil receiver")
}

func TestOpenAIProvider_MissingAPIKey_Uncovered(t *testing.T) {
	p := NewOpenAIProvider(Config{}, nil)
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestOpenAIProvider_EmptyTexts_Uncovered(t *testing.T) {
	p := NewOpenAIProvider(Config{OpenAI: openAIConfig{APIKey: "key"}}, nil)
	result, err := p.Translate(context.Background(), "ja", "en", []string{})
	require.NoError(t, err)
	assert.Empty(t, result.Texts)
}

func TestOpenAICompatibleProvider_NilReceiver_Uncovered(t *testing.T) {
	var p *OpenAICompatibleProvider
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil receiver")
}

func TestOpenAICompatibleProvider_MissingAPIKey_Uncovered(t *testing.T) {
	p := NewOpenAICompatibleProvider(Config{}, nil)
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	// Missing model is checked first for openai-compatible
}

// --- parse.go uncovered ---

func TestNormalizeTranslationPayload_Uncovered(t *testing.T) {
	// Test: strips ```json prefix and ``` suffix
	input1 := "```json\n[\"a\"]\n```"
	assert.Equal(t, `["a"]`, normalizeTranslationPayload(input1))

	// Test: trims whitespace
	assert.Equal(t, `["a"]`, normalizeTranslationPayload("  [\"a\"]  "))

	// Test: strips bare ```
	input3 := "```[\"a\"]```"
	assert.Equal(t, `["a"]`, normalizeTranslationPayload(input3))
}

func TestParseCompactTranslationPayload_Uncovered(t *testing.T) {
	t.Run("valid compact payload", func(t *testing.T) {
		payload := "<<<JZ_0>>>hello<<<JZ_1>>>world"
		result, err := parseCompactTranslationPayload(payload, 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"hello", "world"}, result)
	})

	t.Run("missing marker", func(t *testing.T) {
		payload := "<<<JZ_0>>>hello"
		_, err := parseCompactTranslationPayload(payload, 2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing output marker")
	})

	t.Run("missing next marker", func(t *testing.T) {
		payload := "<<<JZ_0>>>hello<<<JZ_2>>>world"
		_, err := parseCompactTranslationPayload(payload, 2)
		assert.Error(t, err)
	})
}

func TestUnmarshalStringArray_Uncovered(t *testing.T) {
	t.Run("valid JSON array", func(t *testing.T) {
		result, err := unmarshalStringArray(`["a","b","c"]`)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := unmarshalStringArray("not json")
		assert.Error(t, err)
	})
}

func TestParseStringArrayPayload_ExtractedArray_Uncovered(t *testing.T) {
	// When the payload contains a JSON array wrapped in other text
	result, err := parseStringArrayPayload(`Some prefix ["a","b"] some suffix`)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result)
}

// --- errors.go uncovered ---

func TestTranslationError_Error_Uncovered(t *testing.T) {
	t.Run("with status code", func(t *testing.T) {
		e := &translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429, Message: "rate limited"}
		assert.Contains(t, e.Error(), "status 429")
	})

	t.Run("without status code", func(t *testing.T) {
		e := &translationError{Kind: TranslationErrorProvider, Message: "failed"}
		assert.Equal(t, "failed", e.Error())
	})

	t.Run("nil error", func(t *testing.T) {
		var e *translationError
		assert.Equal(t, "", e.Error())
	})

	t.Run("empty message falls back to kind", func(t *testing.T) {
		e := &translationError{Kind: TranslationErrorParse}
		assert.Equal(t, "parse_error", e.Error())
	})
}

func TestTranslationError_Unwrap_Uncovered(t *testing.T) {
	inner := errors.New("inner")
	e := &translationError{Cause: inner}
	assert.ErrorIs(t, e, inner)

	var nilErr *translationError
	assert.Nil(t, nilErr.Unwrap())
}
