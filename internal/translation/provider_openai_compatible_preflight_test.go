package translation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func compatiblePreflightConfig(baseURL string) Config {
	return Config{OpenAICompatible: openAICompatibleConfig{
		BaseURL: baseURL, APIKey: "fixture-api-key", Model: "fixture-model", BackendType: "other",
	}}
}

type compatiblePreflightBody struct {
	io.Reader
	closes int
}

func (b *compatiblePreflightBody) Close() error {
	b.closes++
	return nil
}

func compatiblePreflightSuccessClient(t *testing.T) (*mockHTTPClient, *int, *compatiblePreflightBody) {
	t.Helper()
	calls := 0
	body := &compatiblePreflightBody{Reader: strings.NewReader(`{"choices":[{"message":{"content":"<<<JZ_0>>>translated fixture"}}]}`)}
	client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, 1, calls, "ordinary translation must issue only one request")
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "https://compatible.example/v1/chat/completions", req.URL.String())
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		require.Equal(t, "Bearer fixture-api-key", req.Header.Get("Authorization"))
		var request openAIChatRequest
		require.NoError(t, json.NewDecoder(req.Body).Decode(&request))
		require.Equal(t, "fixture-model", request.Model)
		require.NotNil(t, request.Temperature)
		require.Zero(t, *request.Temperature)
		require.Len(t, request.Messages, 2)
		require.Equal(t, "system", request.Messages[0].Role)
		require.NotEmpty(t, request.Messages[0].Content)
		require.Equal(t, "user", request.Messages[1].Role)
		require.Contains(t, request.Messages[1].Content, "source text fixture")
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}}
	return client, &calls, body
}

func TestOpenAICompatiblePreflightEmptyWork(t *testing.T) {
	for _, tc := range []struct {
		name  string
		texts []string
	}{
		{name: "nil"}, {name: "empty", texts: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := compatiblePreflightConfig("https://compatible.example/v1")
			originalConfig := cfg
			client, calls, body := compatiblePreflightSuccessClient(t)
			provider := NewOpenAICompatibleProvider(cfg, client)
			assertEmpty := func() {
				t.Helper()
				result, err := provider.Translate(context.Background(), "ja", "en", tc.texts)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Empty(t, result.Texts)
				require.Empty(t, result.RawLLM)
				require.Empty(t, tc.texts)
			}
			assertEmpty()
			require.Zero(t, *calls)
			require.Zero(t, body.closes)
			texts := []string{"source text fixture"}
			result, err := provider.Translate(context.Background(), "ja", "en", texts)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, []string{"translated fixture"}, result.Texts)
			require.Equal(t, []string{"source text fixture"}, texts)
			require.Equal(t, 1, body.closes)
			assertEmpty()
			require.Equal(t, 1, *calls)
			require.Equal(t, originalConfig, cfg)
		})
	}
}

func TestOpenAICompatiblePreflightMalformedEndpoint(t *testing.T) {
	// This is the exported provider-library boundary with fresh raw config, not
	// application settings that have already passed URL validation.
	cfg := compatiblePreflightConfig("https://compatible.example/v1%zz")
	originalConfig := cfg
	calls := 0
	client := &mockHTTPClient{doFunc: func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("malformed endpoint must fail before HTTP")
		return nil, nil
	}}
	provider := NewOpenAICompatibleProvider(cfg, client)
	texts := []string{"source text fixture"}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := provider.Translate(context.Background(), "ja", "en", texts)
		require.Nil(t, result)
		var parseErr *url.Error
		require.ErrorAs(t, err, &parseErr)
		require.Equal(t, "parse", parseErr.Op)
		require.Equal(t, "https://compatible.example/v1%zz/chat/completions", parseErr.URL)
		var escapeErr url.EscapeError
		require.ErrorAs(t, err, &escapeErr)
		require.Equal(t, url.EscapeError("%zz"), escapeErr)
		require.NotContains(t, err.Error(), "fixture-api-key")
		require.NotContains(t, err.Error(), texts[0])
		require.Zero(t, calls)
	}
	require.Equal(t, originalConfig, cfg)
	require.Equal(t, []string{"source text fixture"}, texts)

	// Recovery uses a newly constructed valid provider; no private provider state
	// or validated application config is changed to manufacture the failure.
	validConfig := compatiblePreflightConfig("https://compatible.example/v1")
	originalValidConfig := validConfig
	validClient, validCalls, body := compatiblePreflightSuccessClient(t)
	validProvider := NewOpenAICompatibleProvider(validConfig, validClient)
	result, err := validProvider.Translate(context.Background(), "ja", "en", texts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []string{"translated fixture"}, result.Texts)
	require.Equal(t, 1, *validCalls)
	require.Equal(t, 1, body.closes)
	require.Zero(t, calls)
	require.Equal(t, originalValidConfig, validConfig)
	require.Equal(t, []string{"source text fixture"}, texts)
}
