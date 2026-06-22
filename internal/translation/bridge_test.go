package translation

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestConfigFromAppMapsAllFields(t *testing.T) {
	thinking := true
	src := config.TranslationConfig{
		Enabled:                 true,
		Provider:                "openai-compatible",
		SourceLanguage:          "ja",
		TargetLanguage:          "en",
		TimeoutSeconds:          30,
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
		Fields: config.TranslationFieldsConfig{
			Title:         true,
			OriginalTitle: true,
			Description:   true,
			Director:      true,
			Maker:         true,
			Label:         true,
			Series:        true,
			Genres:        true,
			Actresses:     true,
		},
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "openai-key",
			Model:   "gpt-4o-mini",
		},
		DeepL: config.DeepLTranslationConfig{
			Mode:    models.DeepLModePro,
			BaseURL: "https://api.deepl.com",
			APIKey:  "deepl-key",
		},
		Google: config.GoogleTranslationConfig{
			Mode:    models.GoogleModePaid,
			BaseURL: "https://translate.googleapis.com",
			APIKey:  "google-key",
		},
		OpenAICompatible: config.OpenAICompatibleTranslationConfig{
			BaseURL:        "http://localhost:11434/v1",
			APIKey:         "compat-key",
			Model:          "llama3.1",
			EnableThinking: &thinking,
			BackendType:    "  LLAMA_CPP  ",
		},
		Anthropic: config.AnthropicTranslationConfig{
			BaseURL: "https://api.anthropic.com",
			APIKey:  "anthropic-key",
			Model:   "claude-sonnet-4-20250514",
		},
	}

	got := ConfigFromApp(src)
	want := Config{
		Enabled:                 true,
		Provider:                "openai-compatible",
		SourceLanguage:          "ja",
		TargetLanguage:          "en",
		TimeoutSeconds:          30,
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
		Fields:                  fieldsConfig{Title: true, OriginalTitle: true, Description: true, Director: true, Maker: true, Label: true, Series: true, Genres: true, Actresses: true},
		OpenAI:                  openAIConfig{BaseURL: "https://api.openai.com/v1", APIKey: "openai-key", Model: "gpt-4o-mini"},
		DeepL:                   deepLConfig{Mode: models.DeepLModePro, BaseURL: "https://api.deepl.com", APIKey: "deepl-key"},
		Google:                  googleConfig{Mode: models.GoogleModePaid, BaseURL: "https://translate.googleapis.com", APIKey: "google-key"},
		OpenAICompatible: openAICompatibleConfig{
			BaseURL:        "http://localhost:11434/v1",
			APIKey:         "compat-key",
			Model:          "llama3.1",
			EnableThinking: true,
			BackendType:    "llama.cpp",
		},
		Anthropic: anthropicConfig{BaseURL: "https://api.anthropic.com", APIKey: "anthropic-key", Model: "claude-sonnet-4-20250514"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfigFromApp() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestConfigFromAppOpenAICompatibleEnableThinkingResolution(t *testing.T) {
	tests := []struct {
		name string
		in   *bool
		want bool
	}{
		{name: "nil", in: nil, want: false},
		{name: "true", in: boolPtr(true), want: true},
		{name: "false", in: boolPtr(false), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConfigFromApp(config.TranslationConfig{
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{EnableThinking: tt.in},
			}).OpenAICompatible.EnableThinking
			if got != tt.want {
				t.Fatalf("EnableThinking = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigFromAppOpenAICompatibleBackendTypeNormalization(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "auto", in: "  auto  ", want: ""},
		{name: "vllm", in: "VLLM", want: "vllm"},
		{name: "ollama", in: " ollama ", want: "ollama"},
		{name: "llama cpp", in: "llama_cpp", want: "llama.cpp"},
		{name: "generic", in: "Generic", want: "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConfigFromApp(config.TranslationConfig{
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{BackendType: tt.in},
			}).OpenAICompatible.BackendType
			if got != tt.want {
				t.Fatalf("BackendType = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigFromAppZeroValueProducesZeroValueBridge(t *testing.T) {
	got := ConfigFromApp(config.TranslationConfig{})
	want := Config{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfigFromApp(zero) mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
