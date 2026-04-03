package translation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// =============================================================================
// New tests
// =============================================================================

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.TranslationConfig
	}{
		{
			name: "returns non-nil service",
			cfg:  config.TranslationConfig{Enabled: true},
		},
		{
			name: "service has http client",
			cfg:  config.TranslationConfig{},
		},
		{
			name: "service preserves config",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)
			assert.NotNil(t, s)
			assert.NotNil(t, s.httpClient)
			assert.Equal(t, tt.cfg.Enabled, s.cfg.Enabled)
		})
	}
}

// =============================================================================
// TranslateMovie tests - early returns and validation
// =============================================================================

func TestTranslateMovie_EarlyReturns(t *testing.T) {
	tests := []struct {
		name    string
		service *Service
		movie   *models.Movie
		wantErr bool
		wantNil bool
	}{
		{
			name:    "nil service returns nil",
			service: nil,
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
		{
			name: "nil movie returns nil",
			service: New(config.TranslationConfig{
				Enabled: true,
			}),
			movie:   nil,
			wantNil: true,
		},
		{
			name: "disabled service returns nil",
			service: New(config.TranslationConfig{
				Enabled: false,
			}),
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
		{
			name: "missing target language returns error",
			service: New(config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "",
			}),
			movie:   &models.Movie{Title: "Test"},
			wantErr: true,
		},
		{
			name: "same source and target language returns nil",
			service: New(config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				SourceLanguage: "en",
				TargetLanguage: "en",
			}),
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.service.TranslateMovie(context.Background(), tt.movie)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Nil(t, result)
			}
		})
	}
}

// =============================================================================
// TranslateMovie tests - ApplyToPrimary flag
// =============================================================================

func TestTranslateMovie_ApplyToPrimary(t *testing.T) {
	t.Run("apply to primary enabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `["Translated Title"]`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "テスト"}
		result, err := s.TranslateMovie(context.Background(), movie)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Translated Title", result.Title)
		// Verify movie was mutated when ApplyToPrimary is enabled
		assert.Equal(t, "Translated Title", movie.Title)
	})

	t.Run("apply to primary disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `["Translated Title"]`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: false,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "テスト"}
		originalTitle := movie.Title
		result, err := s.TranslateMovie(context.Background(), movie)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Translated Title", result.Title)
		// Verify movie was NOT mutated when ApplyToPrimary is disabled
		assert.Equal(t, originalTitle, movie.Title)
	})
}

// =============================================================================
// translateTexts tests - connection failure handling
// =============================================================================

func TestTranslateTexts_ConnectionFailure(t *testing.T) {
	t.Run("openai connection refused", func(t *testing.T) {
		// Create a test server that immediately closes to force connection refused
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.Close() // Close immediately to make connection fail

		cfg := config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

		require.Error(t, err)
		// Verify it's a connection-related error (not HTTP status error)
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "dial tcp") ||
			strings.Contains(errMsg, "connection reset"),
			"expected connection error, got: %v", errMsg)
	})

	t.Run("deepl connection refused", func(t *testing.T) {
		// Create a test server that immediately closes to force connection refused
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.Close() // Close immediately to make connection fail

		cfg := config.TranslationConfig{
			Provider:       "deepl",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			DeepL: config.DeepLTranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

		require.Error(t, err)
		// Verify it's a connection-related error
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "dial tcp") ||
			strings.Contains(errMsg, "connection reset"),
			"expected connection error, got: %v", errMsg)
	})
}

// =============================================================================
// translateTexts tests - provider dispatch
// =============================================================================

func TestTranslateTexts_Dispatch(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "openai provider",
			provider: "openai",
			cfg: config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: func() string {
						// Create a test server that immediately closes to force connection refused
						s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
						}))
						// Close the server immediately so the URL becomes unreachable
						s.Close()
						return s.URL
					}(),
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure
		},
		{
			name:     "deepl provider",
			provider: "deepl",
			cfg: config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					BaseURL: func() string {
						// Create a test server that immediately closes to force connection refused
						s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
						}))
						// Close the server immediately so the URL becomes unreachable
						s.Close()
						return s.URL
					}(),
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure
		},
		{
			name:     "google paid mode requires api key",
			provider: "google",
			cfg: config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:   "paid",
					APIKey: "",
				},
			},
			wantErr:     true,
			errContains: "google api_key is required for paid mode",
		},
		{
			name:     "unsupported provider",
			provider: "custom",
			cfg: config.TranslationConfig{
				Provider:       "custom",
				TargetLanguage: "en",
				SourceLanguage: "ja",
			},
			wantErr:     true,
			errContains: "unsupported translation provider",
		},
		{
			name:     "uppercase provider",
			provider: "OPENAI",
			cfg: config.TranslationConfig{
				Provider:       "OPENAI",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
		{
			name:     "mixed case provider",
			provider: "DeePl",
			cfg: config.TranslationConfig{
				Provider:       "DeePl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// translateWithDeepL tests
// =============================================================================

func TestTranslateWithDeepL(t *testing.T) {
	tests := []struct {
		name            string
		mode            string // "free" or "pro"
		handler         func(http.ResponseWriter, *http.Request)
		wantErr         bool
		errContains     string
		expectCount     int // Expected number of results (default 1)
		validateRequest func(t *testing.T, r *http.Request)
	}{
		{
			name: "free mode success",
			mode: "free",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "translated text"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
			validateRequest: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "DeepL-Auth-Key test-key", r.Header.Get("Authorization"))

				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "EN", body["target_lang"])
				assert.Equal(t, "JA", body["source_lang"])
			},
		},
		{
			name: "pro mode success",
			mode: "pro",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "translated text"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
			validateRequest: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "DeepL-Auth-Key test-key", r.Header.Get("Authorization"))

				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "EN", body["target_lang"])
			},
		},
		{
			name: "API returns error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Forbidden"))
			},
			wantErr:     true,
			errContains: "deepl translation failed",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			wantErr:     true,
			errContains: "failed to decode deepl response",
		},
		{
			name: "multiple texts translated",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "first"},
						{"text": "second"},
						{"text": "third"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     false,
			expectCount: 3,
			validateRequest: func(t *testing.T, r *http.Request) {
				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				texts, ok := body["text"].([]interface{})
				require.True(t, ok)
				assert.Len(t, texts, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v2/translate", func(w http.ResponseWriter, r *http.Request) {
				if tt.validateRequest != nil {
					tt.validateRequest(t, r)
				}
				tt.handler(w, r)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			mode := tt.mode
			if mode == "" {
				mode = "free"
			}
			cfg := config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					Mode:    mode,
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			}

			s := New(cfg)
			inputTexts := []string{"test"}
			if tt.name == "multiple texts translated" {
				inputTexts = []string{"test1", "test2", "test3"}
			}
			result, err := s.translateTexts(context.Background(), "ja", "en", inputTexts)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				expectedCount := 1
				if tt.expectCount > 0 {
					expectedCount = tt.expectCount
				}
				assert.Len(t, result, expectedCount)
				if expectedCount == 1 {
					assert.Equal(t, "translated text", result[0])
				}
			}
		})
	}
}

func TestTranslateWithDeepL_MissingAPIKey(t *testing.T) {
	s := New(config.TranslationConfig{
		Provider:       "deepl",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		DeepL: config.DeepLTranslationConfig{
			Mode:    "free",
			BaseURL: "http://example.com",
			APIKey:  "",
		},
	})

	_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deepl api_key is required")
}

func TestTranslateWithDeepL_SourceLanguage(t *testing.T) {
	var capturedSourceLang string

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/translate", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if sl, ok := body["source_lang"].(string); ok {
			capturedSourceLang = sl
		}
		response := map[string]interface{}{
			"translations": []map[string]string{
				{"text": "translated"},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s := New(config.TranslationConfig{
		Provider:       "deepl",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		DeepL: config.DeepLTranslationConfig{
			Mode:    "free",
			BaseURL: server.URL,
			APIKey:  "test-key",
		},
	})

	result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
	require.NoError(t, err)
	assert.Equal(t, "JA", capturedSourceLang)
	assert.Len(t, result, 1)
}

// =============================================================================
// translateWithGoogle tests
// =============================================================================

func TestTranslateWithGoogle(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		handler     func(http.ResponseWriter, *http.Request)
		wantErr     bool
		errContains string
	}{
		{
			name: "free mode success",
			mode: "free",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Google free API returns nested array format: [[[translated_text, null, ...]]]
				response := []any{
					[]any{
						[]any{"translated text", nil, "en", nil, nil, nil, "gtx"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
		},
		{
			name: "paid mode success",
			mode: "paid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "translated text"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
		},
		{
			name: "missing API key for paid mode",
			mode: "paid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Server won't be called
			},
			wantErr:     true,
			errContains: "google api_key is required for paid mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			if tt.handler != nil {
				// Google free uses /translate_a/single, paid uses /language/translate/v2
				if tt.mode == "free" {
					mux.HandleFunc("/translate_a/single", tt.handler)
				} else {
					mux.HandleFunc("/language/translate/v2", tt.handler)
				}
			}

			server := httptest.NewServer(mux)
			defer server.Close()

			cfg := config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    tt.mode,
					BaseURL: server.URL,
					APIKey: func() string {
						if tt.name == "missing API key for paid mode" {
							return ""
						}
						return "test-key"
					}(),
				},
			}

			s := New(cfg)
			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
				assert.Equal(t, "translated text", result[0])
			}
		})
	}
}

// =============================================================================
// translateWithOpenAI tests
// =============================================================================

func TestTranslateWithOpenAI(t *testing.T) {
	t.Run("successful translation", func(t *testing.T) {
		var capturedBody map[string]interface{}
		var capturedAuthHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint
			assert.Equal(t, "/chat/completions", r.URL.Path)
			// Verify auth header
			capturedAuthHeader = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `["translated text"]`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "translated text", result[0])
		assert.Equal(t, "gpt-4o-mini", capturedBody["model"])
		// Verify request structure
		messages := capturedBody["messages"].([]interface{})
		assert.Len(t, messages, 2)
		assert.Equal(t, "system", messages[0].(map[string]interface{})["role"])
		assert.Equal(t, "user", messages[1].(map[string]interface{})["role"])
		// Verify auth header format
		assert.Equal(t, "Bearer test-key", capturedAuthHeader)
	})

	t.Run("API returns error status", func(t *testing.T) {
		var capturedAuthHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint
			assert.Equal(t, "/chat/completions", r.URL.Path)
			// Verify auth header
			capturedAuthHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Invalid API key"))
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "openai translation failed")
		assert.Contains(t, err.Error(), "Invalid API key")
		// Verify auth header format
		assert.Equal(t, "Bearer test-key", capturedAuthHeader)
	})

	t.Run("malformed JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode openai response")
	})

	t.Run("empty choices in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "openai response contained no choices")
	})

	t.Run("invalid JSON in content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "not a json array",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse translated output payload")
	})
}

func TestTranslateWithOpenAI_DefaultValues(t *testing.T) {
	t.Run("default model when not specified", func(t *testing.T) {
		var capturedBody map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `["translated"]`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "gpt-4o-mini", capturedBody["model"])
	})
}

func TestTranslateWithGooglePaid(t *testing.T) {
	tests := []struct {
		name            string
		handler         func(http.ResponseWriter, *http.Request)
		validateRequest func(t *testing.T, r *http.Request)
		wantErr         bool
		errContains     string
	}{
		{
			name: "successful paid translation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "translated text"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
				// Verify key query parameter
				assert.Contains(t, r.URL.RawQuery, "key=")
			},
			wantErr: false,
		},
		{
			name: "API returns error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Unauthorized"))
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr:     true,
			errContains: "google paid translation failed",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr:     true,
			errContains: "failed to decode google paid response",
		},
		{
			name: "HTML entity unescaping",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "&lt;hello&gt;"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/language/translate/v2", func(w http.ResponseWriter, r *http.Request) {
				if tt.validateRequest != nil {
					tt.validateRequest(t, r)
				}
				tt.handler(w, r)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    "paid",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

func TestTranslateWithGooglePaid_EndpointAndAuth(t *testing.T) {
	var capturedKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/language/translate/v2", func(w http.ResponseWriter, r *http.Request) {
		// Extract key from query string
		capturedKey = r.URL.Query().Get("key")
		// Verify endpoint path
		assert.Equal(t, "/language/translate/v2", r.URL.Path)
		response := map[string]interface{}{
			"data": map[string]interface{}{
				"translations": []map[string]string{
					{"translatedText": "translated text"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s := New(config.TranslationConfig{
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Google: config.GoogleTranslationConfig{
			Mode:    "paid",
			BaseURL: server.URL,
			APIKey:  "test-key",
		},
	})

	result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "translated text", result[0])
	// Verify key query parameter contains the API key
	assert.Equal(t, "test-key", capturedKey)
}

func TestTranslateWithGoogleFree(t *testing.T) {
	t.Run("successful free translation", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			// Verify query parameters directly in handler
			assert.Equal(t, "gtx", r.URL.Query().Get("client"))
			assert.Equal(t, "ja", r.URL.Query().Get("sl"))
			assert.Equal(t, "en", r.URL.Query().Get("tl"))
			// Google free API returns nested array: [[[translated_text, null, "en", ...]]]
			response := []any{
				[]any{
					[]any{"translated text", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "translated text", result[0])
	})

	t.Run("API returns error status", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation failed")
		assert.Contains(t, err.Error(), "Internal Server Error")
	})

	t.Run("multiple texts translated sequentially", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{
					[]any{"translated", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test1", "test2"})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("source language auto when empty", func(t *testing.T) {
		var capturedSL string
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			capturedSL = r.URL.Query().Get("sl")
			response := []any{
				[]any{
					[]any{"translated", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "", // Empty should become "auto"
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "", "en", []string{"test"})
		require.NoError(t, err)
		assert.Equal(t, "auto", capturedSL)
	})

	t.Run("malformed response - empty array", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected google free response shape")
	})

	t.Run("malformed response - nested array without segments", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation returned empty text")
	})

	t.Run("malformed response - non-string segment", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{
					[]any{123, nil, "en"}, // Number instead of string
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation returned empty text")
	})
}

// =============================================================================
// TranslateMovie full flow tests
// =============================================================================

func TestTranslateMovie_FullFlow(t *testing.T) {
	tests := []struct {
		name                string
		cfg                 config.TranslationConfig
		movie               *models.Movie
		mockResponse        []string
		wantErr             bool
		wantTitle           string
		wantPrimarySet      bool
		wantTranslatedCount int
	}{
		{
			name: "happy_path_translates_and_updates_fields",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockResponse:        []string{"Translated Title"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      true,
			wantTranslatedCount: 1,
		},
		{
			name: "apply_to_primary_false_does_not_modify_movie",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: false,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockResponse:        []string{"Translated Title"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      false,
			wantTranslatedCount: 1,
		},
		{
			name: "translates_multiple_fields",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title:       true,
					Description: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title:       "テスト",
				Description: "説明",
			},
			mockResponse:        []string{"Translated Title", "Translated Description"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      true,
			wantTranslatedCount: 2,
		},
		{
			name: "translates_actresses",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Actresses: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "Test",
				Actresses: []models.Actress{
					{JapaneseName: "田中香", FirstName: "Yui", LastName: "Tanaka"},
				},
			},
			mockResponse:        []string{"Yui Tanaka"},
			wantErr:             false,
			wantPrimarySet:      true,
			wantTranslatedCount: 1,
		},
		{
			name: "translates_genres",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Genres: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "Test",
				Genres: []models.Genre{
					{Name: "ジャンル 1"},
					{Name: "ジャンル 2"},
				},
			},
			mockResponse:        []string{"Genre 1", "Genre 2"},
			wantErr:             false,
			wantPrimarySet:      true,
			wantTranslatedCount: 2,
		},
		{
			name: "empty_fields_are_skipped",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "",
			},
			wantErr:        false,
			wantPrimarySet: false,
		},
		{
			name: "provider_error_returns_error",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockResponse:   nil,
			wantErr:        true,
			wantPrimarySet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server

			if tt.mockResponse == nil {
				// Error case - return 500
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Internal Server Error"))
				}))
			} else {
				// Success case - return mock response
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					response := map[string]interface{}{
						"choices": []map[string]interface{}{
							{
								"message": map[string]string{
									"content": mustMarshal(t, tt.mockResponse),
								},
							},
						},
					}
					_ = json.NewEncoder(w).Encode(response)
				}))
			}
			defer server.Close()

			tt.cfg.OpenAI.BaseURL = server.URL
			s := New(tt.cfg)

			movieCopy := &models.Movie{}
			*movieCopy = *tt.movie
			if len(tt.movie.Actresses) > 0 {
				movieCopy.Actresses = make([]models.Actress, len(tt.movie.Actresses))
				copy(movieCopy.Actresses, tt.movie.Actresses)
			}
			if len(tt.movie.Genres) > 0 {
				movieCopy.Genres = make([]models.Genre, len(tt.movie.Genres))
				copy(movieCopy.Genres, tt.movie.Genres)
			}

			originalTitle := movieCopy.Title

			result, err := s.TranslateMovie(context.Background(), movieCopy)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantTranslatedCount > 0 {
				require.NotNil(t, result)
			}

			if tt.wantTitle != "" {
				assert.Equal(t, tt.wantTitle, result.Title)
			}

			// Verify that the appropriate fields were translated based on config
			if tt.cfg.Fields.Title {
				if tt.wantPrimarySet {
					assert.NotEqual(t, originalTitle, movieCopy.Title)
				} else {
					assert.Equal(t, originalTitle, movieCopy.Title)
				}
			}

			// Verify actresses translation if configured
			if tt.cfg.Fields.Actresses && len(tt.movie.Actresses) > 0 {
				originalActressName := actressDisplayName(tt.movie.Actresses[0])
				assert.NotEqual(t, originalActressName, actressDisplayName(movieCopy.Actresses[0]))
			}

			// Verify genres translation if configured
			if tt.cfg.Fields.Genres && len(tt.movie.Genres) > 0 {
				for i := range tt.movie.Genres {
					assert.NotEqual(t, tt.movie.Genres[i].Name, movieCopy.Genres[i].Name)
				}
			}
		})
	}
}

func mustMarshal(t *testing.T, v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal %T: %v", v, err)
	}
	return string(data)
}

// =============================================================================
// Translation count mismatch tests
// =============================================================================

func TestTranslateMovie_TranslationCountMismatch(t *testing.T) {
	tests := []struct {
		name          string
		inputCount    int
		responseCount int
		wantErr       bool
		errContains   string
	}{
		{
			name:          "returns_error_when_fewer_translations_than_inputs",
			inputCount:    3,
			responseCount: 2,
			wantErr:       true,
			errContains:   "returned 2 items for 3 inputs",
		},
		{
			name:          "returns_error_when_more_translations_than_inputs",
			inputCount:    2,
			responseCount: 4,
			wantErr:       true,
			errContains:   "returned 4 items for 2 inputs",
		},
		{
			name:          "exact_match_succeeds",
			inputCount:    3,
			responseCount: 3,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				translations := make([]string, tt.responseCount)
				for i := 0; i < tt.responseCount; i++ {
					translations[i] = fmt.Sprintf("Translation %d", i+1)
				}
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": mustMarshal(t, translations),
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			// Configure fields based on inputCount
			fields := config.TranslationFieldsConfig{}
			if tt.inputCount >= 1 {
				fields.Title = true
			}
			if tt.inputCount >= 2 {
				fields.Description = true
			}
			if tt.inputCount >= 3 {
				fields.Director = true
			}

			s := New(config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields:         fields,
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			// Create movie with multiple fields to match inputCount
			movie := &models.Movie{
				Title:       "Test Title",
				Description: "Test Description",
			}
			if tt.inputCount >= 3 {
				movie.Director = "Test Director"
			}

			_, err := s.TranslateMovie(context.Background(), movie)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

// =============================================================================
// Malformed response tests
// =============================================================================

func TestTranslateWithOpenAI_MalformedResponses(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		wantErr     bool
		errContains string
	}{
		{
			name: "returns_error_for_missing_choices_field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"not_choices": []interface{}{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "openai response contained no choices",
		},
		{
			name: "returns_error_for_null_choices",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": nil,
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "openai response contained no choices",
		},
		{
			name: "returns_error_for_missing_message_field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"not_message": "data"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "failed to parse translated output payload",
		},
		{
			name: "returns_error_for_empty_content",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": "",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "failed to parse translated output payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// HTTP error response tests
// =============================================================================

func TestTranslateWithOpenAI_HTTPErrorResponses(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		wantErr      bool
		errContains  string
		bodyContains string
	}{
		{
			name:         "returns_error_for_401_unauthorized",
			statusCode:   http.StatusUnauthorized,
			body:         "Invalid API key",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Invalid API key",
		},
		{
			name:         "returns_error_for_429_rate_limit",
			statusCode:   http.StatusTooManyRequests,
			body:         "Rate limit exceeded",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Rate limit exceeded",
		},
		{
			name:         "returns_error_for_500_internal_server_error",
			statusCode:   http.StatusInternalServerError,
			body:         "Internal server error",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Internal server error",
		},
		{
			name:         "returns_error_for_503_service_unavailable",
			statusCode:   http.StatusServiceUnavailable,
			body:         "Service temporarily unavailable",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Service temporarily unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
			if tt.bodyContains != "" {
				assert.Contains(t, err.Error(), tt.bodyContains)
			}
		})
	}
}

// =============================================================================
// Context cancellation tests
// =============================================================================

func TestTranslateMovie_ContextCancellation(t *testing.T) {
	t.Run("cancels_during_request", func(t *testing.T) {
		started := make(chan struct{})
		done := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			// Wait for either context cancellation or done signal
			select {
			case <-r.Context().Done():
				// Client disconnected or context canceled
				return
			case <-done:
			}
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		ctx, cancel := context.WithCancel(context.Background())

		// Start a goroutine to cancel after the request has started
		go func() {
			<-started
			cancel()
		}()

		movie := &models.Movie{Title: "テスト"}
		_, err := s.TranslateMovie(ctx, movie)

		close(done) // Unblock the server handler
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled) ||
			strings.Contains(err.Error(), "canceled") ||
			strings.Contains(err.Error(), "context"),
			"expected context cancellation error, got: %v", err)
	})

	t.Run("deadline_exceeded", func(t *testing.T) {
		started := make(chan struct{})
		done := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			// Wait for either context cancellation or done signal
			select {
			case <-r.Context().Done():
				// Client disconnected or context canceled
				return
			case <-done:
			}
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		movie := &models.Movie{Title: "テスト"}
		_, err := s.TranslateMovie(ctx, movie)

		close(done) // Unblock the server handler
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded) ||
			strings.Contains(err.Error(), "deadline exceeded"),
			"expected deadline exceeded error, got: %v", err)
	})
}

// =============================================================================
// DeepL specific error tests
// =============================================================================

func TestTranslateWithDeepL_ErrorResponses(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "returns_error_for_403_forbidden",
			statusCode:  http.StatusForbidden,
			body:        "Invalid auth key",
			wantErr:     true,
			errContains: "deepl translation failed",
		},
		{
			name:        "returns_error_for_400_bad_request",
			statusCode:  http.StatusBadRequest,
			body:        "Invalid text parameter",
			wantErr:     true,
			errContains: "deepl translation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					Mode:    "free",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

// =============================================================================
// Google specific error tests
// =============================================================================

func TestTranslateWithGooglePaid_ErrorResponses(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "returns_error_for_401_unauthorized",
			statusCode:  http.StatusUnauthorized,
			body:        `{"error":{"message":"Invalid API key"}}`,
			wantErr:     true,
			errContains: "google paid translation failed",
		},
		{
			name:        "returns_error_for_403_quota_exceeded",
			statusCode:  http.StatusForbidden,
			body:        `{"error":{"message":"Quota exceeded"}}`,
			wantErr:     true,
			errContains: "google paid translation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    "paid",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

// =============================================================================
// parseStringArrayPayload additional edge case tests
// =============================================================================

func TestParseStringArrayPayload_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   "not a json array",
			wantErr: true,
		},
		{
			name:    "json with markdown fences",
			input:   "```json[\"hello\",\"world\"]```",
			wantErr: false,
		},
		{
			name:    "json with newlines",
			input:   "```json\n[\"hello\",\"world\"]\n```",
			wantErr: false,
		},
		{
			name:    "text before array",
			input:   "Here is the translation: [\"hello\"]",
			wantErr: false,
		},
		{
			name:    "unicode characters",
			input:   `["こんにちは","世界"]`,
			wantErr: false,
		},
		{
			name:    "single element",
			input:   `["single"]`,
			wantErr: false,
		},
		{
			name:    "nested array should fail",
			input:   `[["nested"]]`,
			wantErr: true,
		},
		{
			name:    "escaped quotes in strings",
			input:   `["hello \"world\"","test"]`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseStringArrayPayload(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// translateWithOpenAICompatible tests
// =============================================================================

func TestTranslateWithOpenAICompatible(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "success with api key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/chat/completions", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": `["translated text"]`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr: false,
		},
		{
			name: "success without api key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "", r.Header.Get("Authorization"))
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": `["translated"]`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: false,
		},
		{
			name: "missing model returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Should not be called
				t.Error("handler should not be called")
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible model is required",
		},
		{
			name: "upstream error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Invalid key"))
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible translation failed",
		},
		{
			name: "malformed json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "failed to decode openai-compatible response",
		},
		{
			name: "empty choices in response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible response contained no choices",
		},
		{
			name: "uses default base url when empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": `["translated"]`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && tt.errContains == "openai-compatible model is required" {
				s := New(tt.cfg)
				_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			tt.cfg.OpenAICompatible.BaseURL = server.URL
			s := New(tt.cfg)

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

// =============================================================================
// translateWithAnthropic tests
// =============================================================================

func TestTranslateWithAnthropic(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "success with valid response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/messages", r.URL.Path)
				assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": `["translated text"]`},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "missing api key returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "",
				},
			},
			wantErr:     true,
			errContains: "anthropic api_key is required",
		},
		{
			name: "upstream error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Invalid API key"))
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "anthropic translation failed",
		},
		{
			name: "malformed json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "failed to decode anthropic response",
		},
		{
			name: "empty content blocks",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"content": []map[string]string{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "anthropic response contained no content blocks",
		},
		{
			name: "uses default model when not specified",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "claude-sonnet-4-20250514", body["model"])
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": `["translated"]`},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "uses custom model when specified",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "claude-3-5-sonnet-20241022", body["model"])
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": `["translated"]`},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
					Model:  "claude-3-5-sonnet-20241022",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && tt.errContains == "anthropic api_key is required" {
				s := New(tt.cfg)
				_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			tt.cfg.Anthropic.BaseURL = server.URL
			s := New(tt.cfg)

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

// =============================================================================
// Dispatch tests for new providers
// =============================================================================

func TestTranslateTexts_Dispatch_NewProviders(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "openai-compatible provider dispatch",
			provider: "openai-compatible",
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: true, // Error due to connection failure (no server)
		},
		{
			name:     "anthropic provider dispatch",
			provider: "anthropic",
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure (no server)
		},
		{
			name:     "uppercase openai-compatible provider",
			provider: "OPENAI-COMPATIBLE",
			cfg: config.TranslationConfig{
				Provider:       "OPENAI-COMPATIBLE",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: true,
		},
		{
			name:     "uppercase anthropic provider",
			provider: "ANTHROPIC",
			cfg: config.TranslationConfig{
				Provider:       "ANTHROPIC",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
