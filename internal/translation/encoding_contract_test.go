package translation

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLLMTranslationPromptsExactEncodingContract(t *testing.T) {
	const system = "You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY the indexed output markers in ascending order. Do not use JSON. Do not add commentary. Do not omit any index. Keep each translation on a single logical line; if needed, replace internal newlines with spaces. Source language: ja. Target language: en."
	for _, tc := range []struct {
		name  string
		texts []string
		user  string
	}{
		{"nil", nil, "Translate this JSON array of strings: null\nReturn output in this exact pattern:"},
		{"empty", []string{}, "Translate this JSON array of strings: []\nReturn output in this exact pattern:"},
		{"ordered Unicode and escaping", []string{"日本語", "\"\\\n\t\r\b\f\x00<>&\u2028\u2029", "\xff"}, "Translate this JSON array of strings: " + `["日本語","\"\\\n\t\r\b\f\u0000\u003c\u003e\u0026\u2028\u2029","�"]` + "\nReturn output in this exact pattern:\n<<<JZ_0>>>\ntranslated text\n<<<JZ_1>>>\ntranslated text\n<<<JZ_2>>>\ntranslated text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotSystem, gotUser := buildLLMTranslationPrompts("ja", "en", tc.texts)
			require.Equal(t, system, gotSystem)
			require.Equal(t, tc.user, gotUser)
		})
	}
}

// This is a helper-boundary contract, not evidence that the public Translate
// configuration can produce non-finite temperatures or arbitrary kwargs values.
func TestExecuteOpenAIChatTranslationRejectsUnencodableDynamicRequest(t *testing.T) {
	for _, tc := range []struct {
		name            string
		temperature     *float64
		kwargs          map[string]any
		message         string
		unsupportedType bool
	}{
		{name: "NaN", temperature: encodingContractFloat(math.NaN()), message: "json: unsupported value: NaN"},
		{name: "positive infinity", temperature: encodingContractFloat(math.Inf(1)), message: "json: unsupported value: +Inf"},
		{name: "negative infinity", temperature: encodingContractFloat(math.Inf(-1)), message: "json: unsupported value: -Inf"},
		{name: "channel kwargs", kwargs: map[string]any{"unsupported": make(chan string)}, message: "json: unsupported type: chan string", unsupportedType: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockHTTPClient{}
			result, err := executeOpenAIChatTranslation(context.Background(), client, openAIChatCallOptions{provider: "fixture", baseURL: "https://translation.fixture.invalid", endpoint: "/chat/completions", request: openAIChatRequest{Model: "fixture", Temperature: tc.temperature, ChatTemplateKwargs: tc.kwargs, Messages: []openAIChatMessage{{Role: "user", Content: "text"}}}, textCount: 1})
			require.Nil(t, result)
			require.EqualError(t, err, tc.message)
			if tc.unsupportedType {
				var typed *json.UnsupportedTypeError
				require.ErrorAs(t, err, &typed)
			} else {
				var typed *json.UnsupportedValueError
				require.ErrorAs(t, err, &typed)
			}
			require.False(t, client.called, "encoding must fail before any HTTP call")
		})
	}
}
func encodingContractFloat(value float64) *float64 { return &value }

func TestDeepLExactEncodedRequestContract(t *testing.T) {
	for _, tc := range []struct{ name, source, target, suffix string }{
		{"explicit normalized source", " \tJa\u3000", "en", `,"source_lang":"JA"}`},
		{"auto omitted", " AUTO ", "en", "}"},
		{"blank omitted", " \u3000", "en", "}"},
		{"target uppercased not trimmed", "auto", " en ", "}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "https://translation.fixture.invalid/v2/translate", req.URL.String())
				require.Equal(t, http.Header{"Authorization": {"DeepL-Auth-Key fixture-key"}, "Content-Type": {"application/json"}}, req.Header)
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.NoError(t, req.Body.Close())
				target := "EN"
				if tc.target == " en " {
					target = " EN "
				}
				require.Equal(t, `{"text":["日本語","\"\\\n\t\u0000\u003c\u003e\u0026\u2028\u2029","�"],"target_lang":"`+target+`"`+tc.suffix, string(body))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"translations":[{"text":"one"},{"text":"two"},{"text":"three"}]}`))}, nil
			}}
			provider := NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: " fixture-key\t", BaseURL: " https://translation.fixture.invalid/// "}}, client)
			result, err := provider.Translate(context.Background(), tc.source, tc.target, []string{"日本語", "\"\\\n\t\x00<>&\u2028\u2029", "\xff"})
			require.NoError(t, err)
			require.Equal(t, []string{"one", "two", "three"}, result.Texts)
			require.Equal(t, 1, calls)
		})
	}
}

func TestDeepLEmptyBatchStillValidatesCredentials(t *testing.T) {
	for _, texts := range [][]string{nil, {}} {
		for _, key := range []string{"fixture-key", " \t"} {
			client := &mockHTTPClient{}
			provider := NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: key, BaseURL: "://invalid"}}, client)
			result, err := provider.Translate(context.Background(), "auto", "en", texts)
			if key == "fixture-key" {
				require.NoError(t, err)
				require.Equal(t, &translationResult{}, result)
			} else {
				require.EqualError(t, err, "deepl api_key is required")
				require.Nil(t, result)
			}
			require.False(t, client.called)
		}
	}
}
