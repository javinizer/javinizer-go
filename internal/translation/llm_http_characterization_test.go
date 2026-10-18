package translation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type llmTrackedBody struct {
	reader       io.Reader
	read, closed int
}

func (b *llmTrackedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *llmTrackedBody) Close() error {
	b.closed++
	return errors.New("SECRET_CLOSE_ERROR")
}

type llmFailingReader struct{}

func (llmFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("SECRET_BODY_ERROR")
}

func llmCharacterizationProvider(name string, client httpclient.HTTPClient) TranslatorProvider {
	cfg := Config{
		OpenAI:           openAIConfig{BaseURL: "https://provider.invalid/v1", APIKey: "SECRET_KEY", Model: "gpt-4o-mini"},
		Anthropic:        anthropicConfig{BaseURL: "https://provider.invalid", APIKey: "SECRET_KEY", Model: "claude"},
		OpenAICompatible: openAICompatibleConfig{BaseURL: "https://provider.invalid/v1", APIKey: "SECRET_KEY", Model: "custom", BackendType: "other"},
	}
	switch name {
	case "openai":
		return NewOpenAIProvider(cfg, client)
	case "anthropic":
		return NewAnthropicProvider(cfg, client)
	default:
		return NewOpenAICompatibleProvider(cfg, client)
	}
}

func llmCharacterizationResponse(name, content string) string {
	encoded, _ := json.Marshal(content)
	if name == "anthropic" {
		return fmt.Sprintf("{%q:[{%q:%q,%q:%s}]}", "content", "type", "text", "text", encoded)
	}
	return fmt.Sprintf("{%q:[{%q:{%q:%s}}]}", "choices", "message", "content", encoded)
}

// Exercise real provider entry points, not just the extracted HTTP helper.
func TestLLMHTTPCharacterization(t *testing.T) {
	for _, name := range []string{"openai", "anthropic", "openai-compatible"} {
		for _, tc := range []struct {
			name    string
			status  int
			failure string
			kind    translationErrorKind
			suffix  string
		}{
			{name: "success", status: 200},
			{name: "last success status", status: 299},
			{name: "below success", status: 199, kind: TranslationErrorHTTPStatus},
			{name: "above success", status: 300, kind: TranslationErrorHTTPStatus},
			{name: "rate limited", status: 429, kind: TranslationErrorHTTPStatus},
			{name: "server failure", status: 500, kind: TranslationErrorHTTPStatus},
			{name: "transport failure", failure: "transport", kind: TranslationErrorProvider, suffix: "request failed"},
			{name: "body failure before status", status: 400, failure: "body", kind: TranslationErrorProvider, suffix: "response body read failed"},
			{name: "invalid JSON", status: 200, failure: "json", kind: TranslationErrorParse},
			{name: "invalid translation", status: 200, failure: "parse", kind: TranslationErrorParse},
			{name: "bounded response", status: 200, failure: "limit"},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				logPath := initHygieneLogger(t)
				content := "<<<JZ_0>>>\ntranslated"
				if tc.failure == "parse" {
					content = "<<<JZ_1>>>\nSECRET_RAW_OUTPUT"
				}
				response := llmCharacterizationResponse(name, content)
				if tc.failure == "json" {
					response = "SECRET_INVALID_JSON"
				}
				if tc.failure == "limit" {
					// Valid JSON followed by whitespace fits exactly at the limit;
					// invalid trailing bytes prove no extra response bytes are read.
					response += strings.Repeat(" ", maxTranslationResponseSize-len(response)) + "SECRET_BEYOND_LIMIT"
				}
				body := &llmTrackedBody{reader: strings.NewReader(response)}
				if tc.failure == "body" {
					body.reader = llmFailingReader{}
				}
				ctx := context.WithValue(context.Background(), struct{}{}, "request-context")
				calls := 0
				client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
					calls++
					assert.Same(t, ctx, req.Context())
					assert.Equal(t, http.MethodPost, req.Method)
					assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
					assert.Equal(t, "provider.invalid", req.URL.Host)
					raw, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					assert.Contains(t, string(raw), "SECRET_SOURCE")
					if name == "anthropic" {
						assert.Equal(t, "/v1/messages", req.URL.Path)
						assert.Equal(t, "SECRET_KEY", req.Header.Get("x-api-key"))
						assert.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
					} else {
						assert.Equal(t, "/v1/chat/completions", req.URL.Path)
						assert.Equal(t, "Bearer SECRET_KEY", req.Header.Get("Authorization"))
						assert.Contains(t, string(raw), "\"temperature\":0")
					}
					if tc.failure == "transport" {
						return nil, errors.New("SECRET_TRANSPORT_ERROR https://secret.invalid")
					}
					return &http.Response{StatusCode: tc.status, Body: body}, nil
				}}
				result, err := llmCharacterizationProvider(name, client).Translate(ctx, "ja", "en", []string{"SECRET_SOURCE"})
				require.Equal(t, 1, calls)
				rendered := ""
				if tc.kind == "" {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, []string{"translated"}, result.Texts)
					assert.Equal(t, content, result.RawLLM)
				} else {
					var te *translationError
					require.ErrorAs(t, err, &te)
					assert.Equal(t, tc.kind, te.Kind)
					assert.Nil(t, te.Cause)
					assert.Nil(t, errors.Unwrap(te))
					assert.False(t, isRetryableThinkingStrategyError(err))
					rendered = err.Error()
					if tc.kind == TranslationErrorHTTPStatus {
						assert.Equal(t, tc.status, te.StatusCode)
						assert.Equal(t, fmt.Sprintf("%s translation failed with status %d", name, tc.status), te.Message)
					} else {
						assert.Zero(t, te.StatusCode)
					}
					if tc.suffix != "" {
						assert.Equal(t, name+" translation "+tc.suffix, te.Message)
					}
					// The compatible fallback loop intentionally discards a parse result.
					if tc.failure == "parse" && name != "openai-compatible" {
						require.NotNil(t, result)
						assert.Equal(t, content, result.RawLLM)
					} else {
						assert.Nil(t, result)
					}
				}
				if tc.failure == "transport" {
					assert.Zero(t, body.closed)
				} else {
					assert.Equal(t, 1, body.closed)
				}
				if tc.failure == "limit" {
					assert.Equal(t, maxTranslationResponseSize, body.read)
				}
				// Parser diagnostics are outside the HTTP-layer redaction contract.
				assertHygiene(t, logPath, rendered, "SECRET_KEY", "SECRET_SOURCE", "SECRET_TRANSPORT_ERROR", "SECRET_BODY_ERROR", "SECRET_CLOSE_ERROR", "SECRET_INVALID_JSON", "SECRET_BEYOND_LIMIT")
				logs, readErr := os.ReadFile(logPath)
				require.NoError(t, readErr)
				logText := string(logs)
				assert.Contains(t, logText, "POST chat completion")
				assert.Equal(t, name == "openai-compatible", strings.Contains(logText, "(legacy)"))
				assert.Equal(t, name == "openai-compatible", strings.Contains(logText, "sending request..."))
				assert.Equal(t, tc.failure == "transport", strings.Contains(logText, "request failed after"))
				assert.Equal(t, name == "openai-compatible" && tc.failure != "transport", strings.Contains(logText, "response received in"))
				assert.Equal(t, tc.failure != "transport" && tc.failure != "body" && tc.status >= 200 && tc.status < 300, strings.Contains(logText, "response received (body length="))
			})
		}
	}
}

func TestLLMHTTPCompatibleFallbackCharacterization(t *testing.T) {
	var bodies []*llmTrackedBody
	var strategies []string
	client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
		for _, body := range bodies {
			require.Equal(t, 1, body.closed, "close before the next attempt")
		}
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		strategy := "none"
		for _, key := range []string{"chat_template_kwargs", "reasoning_effort", "enable_thinking"} {
			if _, exists := payload[key]; exists {
				strategy = key
			}
		}
		strategies = append(strategies, strategy)
		status := []int{400, 422, 400, 200}[len(strategies)-1]
		body := &llmTrackedBody{reader: strings.NewReader(llmCharacterizationResponse("openai-compatible", "<<<JZ_0>>>\ntranslated"))}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: status, Body: body}, nil
	}}
	result, err := llmCharacterizationProvider("openai-compatible", client).Translate(context.Background(), "ja", "en", []string{"source"})
	require.NoError(t, err)
	assert.Equal(t, []string{"translated"}, result.Texts)
	assert.Equal(t, []string{"chat_template_kwargs", "reasoning_effort", "enable_thinking", "none"}, strategies)
	for _, body := range bodies {
		assert.Equal(t, 1, body.closed)
	}
}

func TestLLMHTTPLegacyWithoutTimingCharacterization(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			logPath := initHygieneLogger(t)
			client := &mockHTTPClient{doFunc: func(*http.Request) (*http.Response, error) {
				if failed {
					return nil, errors.New("SECRET_TRANSPORT")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(llmCharacterizationResponse("openai", "<<<JZ_0>>>\ntranslated")))}, nil
			}}
			_, err := executeOpenAIChatTranslation(context.Background(), client, openAIChatCallOptions{provider: "legacy", baseURL: "https://provider.invalid", endpoint: "/chat/completions", textCount: 1})
			assert.Equal(t, failed, err != nil)
			logs, readErr := os.ReadFile(logPath)
			require.NoError(t, readErr)
			for _, message := range []string{"sending request", "request failed after", "response received in", "SECRET_TRANSPORT"} {
				assert.NotContains(t, string(logs), message)
			}
		})
	}
}
