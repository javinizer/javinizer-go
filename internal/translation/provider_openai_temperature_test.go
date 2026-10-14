package translation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAITemperatureRequests(t *testing.T) {
	reasoningModels := []string{
		"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5.5",
		"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol",
		"o1", "o1-mini", "o1-preview", "o3", "o3-mini", "o4-mini",
	}
	legacyModels := []string{"gpt-5.1", "gpt-5.2", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "", "gpt-4o-mini", "gpt-4.1", "gpt-3.5-turbo", "custom-model",
		"gpt-50", "gpt-5-custom", "gpt-5-chat-latest", "gpt-6-custom", "gpt-6.2-sol",
		"gpt-5-2026-02-30", "gpt-5-2026-1-01", "gpt-5-2026-01-01-extra", "o10", "o3-custom"}
	type testCase struct {
		model      string
		omit       bool
		compatible bool
	}
	var cases []testCase
	// Synthetic valid date suffixes test snapshot matching for every alias.
	for _, model := range reasoningModels {
		cases = append(cases, testCase{model, true, false}, testCase{model + "-2026-01-01", true, false}, testCase{model, false, true})
	}
	for _, model := range legacyModels {
		cases = append(cases, testCase{model, false, false}, testCase{model + "-2026-01-01", false, false})
	}
	cases = append(cases, testCase{"  gpt-5.6-luna  ", true, false}, testCase{"gpt-5-2025-08-07", true, false}, testCase{"gpt-5.1-2025-11-13", false, false})
	for _, tt := range cases {
		name := tt.model
		if tt.compatible {
			name += "/compatible"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "/v1/chat/completions", req.URL.Path)
				require.Equal(t, "application/json", req.Header.Get("Content-Type"))
				require.Equal(t, "Bearer synthetic-key", req.Header.Get("Authorization"))
				var body map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				model := strings.TrimSpace(tt.model)
				if model == "" {
					model = "gpt-4o-mini"
				}
				require.JSONEq(t, `"`+model+`"`, string(body["model"]))
				temperature, present := body["temperature"]
				require.Equal(t, !tt.omit, present, "temperature presence for %s: %s", model, temperature)
				if present {
					require.Equal(t, "0", string(temperature))
				}
				require.NotContains(t, body, "reasoning_effort")
				var messages []openAIChatMessage
				require.NoError(t, json.Unmarshal(body["messages"], &messages))
				require.Len(t, messages, 2)
				require.Equal(t, "system", messages[0].Role)
				require.Contains(t, messages[1].Content, "source")
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"<<<JZ_0>>>translated"}}]}`))}, nil
			}}
			cfg := Config{OpenAI: openAIConfig{APIKey: "synthetic-key", Model: tt.model}, OpenAICompatible: openAICompatibleConfig{BaseURL: "https://compatible.example/v1", APIKey: "synthetic-key", Model: tt.model, BackendType: "other"}}
			var result *translationResult
			var err error
			if tt.compatible {
				result, err = NewOpenAICompatibleProvider(cfg, client).Translate(context.Background(), "ja", "en", []string{"source"})
			} else {
				result, err = NewOpenAIProvider(cfg, client).Translate(context.Background(), "ja", "en", []string{"source"})
			}
			require.NoError(t, err)
			require.Equal(t, []string{"translated"}, result.Texts)
			require.Equal(t, 1, calls)
		})
	}
}

func TestOpenAITemperatureUnrelatedFailure(t *testing.T) {
	logPath := initHygieneLogger(t)
	calls := 0
	client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"SECRET_PROVIDER_RESPONSE","param":"messages"}}`))}, nil
	}}
	cfg := Config{OpenAI: openAIConfig{APIKey: "SECRET_API_KEY", Model: "gpt-5.6-luna"}}
	_, err := NewOpenAIProvider(cfg, client).Translate(context.Background(), "ja", "en", []string{"SECRET_SOURCE_TEXT"})
	var typed *translationError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, TranslationErrorHTTPStatus, typed.Kind)
	require.Equal(t, 400, typed.StatusCode)
	require.Equal(t, 1, calls, "unrelated errors must not trigger a retry")
	assertHygiene(t, logPath, err.Error(), "SECRET_PROVIDER_RESPONSE", "SECRET_API_KEY", "SECRET_SOURCE_TEXT")
}
