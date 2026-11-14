package translation

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixedChatRequestWireContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		adapter    LLMChatAdapter
		path, want string
		headers    map[string]string
	}{
		{"anthropic", &anthropicChatAdapter{apiKey: "synthetic-key"}, "/v1/messages", `{"max_tokens":4096,"messages":[{"role":"user","content":"user\n\"\\日本語"}],"model":"model\u003c\u0026\u003e","system":"system\u2028"}`, map[string]string{"X-Api-Key": "synthetic-key", "Anthropic-Version": "2023-06-01"}},
		{"openai zero", &openAIChatAdapter{headers: map[string]string{"Authorization": "Bearer synthetic-key", "Content-Type": "ignored"}}, "/chat/completions", `{"model":"model\u003c\u0026\u003e","temperature":0,"messages":[{"role":"system","content":"system\u2028"},{"role":"user","content":"user\n\"\\日本語"}]}`, map[string]string{"Authorization": "Bearer synthetic-key"}},
		{"openai omitted", &openAIChatAdapter{omitTemperature: true}, "/chat/completions", `{"model":"model\u003c\u0026\u003e","messages":[{"role":"system","content":"system\u2028"},{"role":"user","content":"user\n\"\\日本語"}]}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := tc.adapter.BuildRequest(ctx, "https://provider.invalid", "model<&>", "system\u2028", "user\n\"\\日本語", 2)
			require.NoError(t, err)
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "https://provider.invalid"+tc.path, req.URL.String())
			require.Equal(t, "application/json", req.Header.Get("Content-Type"))
			for key, value := range tc.headers {
				require.Equal(t, value, req.Header.Get(key))
			}
			raw, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, req.Body.Close())
			require.Equal(t, tc.want, string(raw))
			require.Equal(t, int64(len(raw)), req.ContentLength)
			replay, err := req.GetBody()
			require.NoError(t, err)
			repeated, err := io.ReadAll(replay)
			require.NoError(t, err)
			require.NoError(t, replay.Close())
			require.Equal(t, raw, repeated)
			cancel()
			require.ErrorIs(t, req.Context().Err(), context.Canceled)
			bad, err := tc.adapter.BuildRequest(t.Context(), "http://[", "model", "system", "user", 1)
			require.Error(t, err)
			require.Nil(t, bad)
		})
	}
}

func TestGooglePaidRequestWireContract(t *testing.T) {
	for _, source := range []string{"", "auto", "ja"} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			client := &mockHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "https://provider.invalid/language/translate/v2?key=synthetic%2B%26key", req.URL.String())
				require.Equal(t, "application/json", req.Header.Get("Content-Type"))
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				want := `{"q":["first\n\"\\日本語","\u003c\u0026\u003e\u2029"],"target":"en","format":"text"}`
				if source == "ja" {
					want = `{"q":["first\n\"\\日本語","\u003c\u0026\u003e\u2029"],"target":"en","source":"ja","format":"text"}`
				}
				require.Equal(t, want, string(raw))
				require.Equal(t, int64(len(raw)), req.ContentLength)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"translations":[{"translatedText":"first &amp;"},{"translatedText":"second"}]}}`))}, nil
			}}
			cfg := Config{Google: googleConfig{APIKey: " synthetic+&key ", BaseURL: " https://provider.invalid/ "}}
			texts := []string{"first\n\"\\日本語", "<&>\u2029"}
			before := append([]string(nil), texts...)
			result, err := translateWithGooglePaid(t.Context(), client, cfg, source, "en", texts)
			require.NoError(t, err)
			require.Equal(t, []string{"first &", "second"}, result.Texts)
			require.Equal(t, 1, calls)
			require.Equal(t, before, texts)
		})
	}
}

func TestGooglePaidRequestErrorsRemainRedacted(t *testing.T) {
	cfg := Config{Google: googleConfig{APIKey: "synthetic-secret", BaseURL: "http://["}}
	client := &mockHTTPClient{doFunc: func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached transport")
		return nil, nil
	}}
	result, err := translateWithGooglePaid(t.Context(), client, cfg, "ja", "en", []string{"private text"})
	require.Nil(t, result)
	require.EqualError(t, err, "google paid translation request construction failed")
	require.NotContains(t, err.Error(), cfg.Google.APIKey)
	cfg.Google.BaseURL = "https://provider.invalid"
	client.doFunc = func(req *http.Request) (*http.Response, error) {
		return nil, errors.New(req.URL.String() + " private text")
	}
	result, err = translateWithGooglePaid(t.Context(), client, cfg, "ja", "en", []string{"private text"})
	require.Nil(t, result)
	require.EqualError(t, err, "google paid translation request failed")
	require.NotContains(t, err.Error(), cfg.Google.APIKey)
	require.NotContains(t, err.Error(), "private text")
}

func TestGooglePaidRequestPrimitiveEncodingContract(t *testing.T) {
	interfaces := []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()}
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for _, iface := range interfaces {
			require.False(t, typ.Implements(iface))
			require.False(t, reflect.PointerTo(typ).Implements(iface))
		}
		switch typ.Kind() {
		case reflect.String:
		case reflect.Slice:
			check(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				check(typ.Field(i).Type)
			}
		default:
			t.Fatalf("unexpected JSON input type: %v", typ)
		}
	}
	check(reflect.TypeFor[googlePaidTranslateRequest]())
}
