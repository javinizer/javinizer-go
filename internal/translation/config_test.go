package translation

import (
	"reflect"
	"testing"

	appconfig "github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestConfigBridgeShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		expected []fieldSpec
	}{
		{
			name:  "Config",
			value: Config{},
			expected: []fieldSpec{
				{"Enabled", typeFor[bool]()},
				{"Provider", typeFor[string]()},
				{"SourceLanguage", typeFor[string]()},
				{"TargetLanguage", typeFor[string]()},
				{"TimeoutSeconds", typeFor[int]()},
				{"ApplyToPrimary", typeFor[bool]()},
				{"OverwriteExistingTarget", typeFor[bool]()},
				{"Fields", reflect.TypeFor[fieldsConfig]()},
				{"OpenAI", reflect.TypeFor[openAIConfig]()},
				{"DeepL", reflect.TypeFor[deepLConfig]()},
				{"Google", reflect.TypeFor[googleConfig]()},
				{"OpenAICompatible", reflect.TypeFor[openAICompatibleConfig]()},
				{"Anthropic", reflect.TypeFor[anthropicConfig]()},
			},
		},
		{
			name:  "fieldsConfig",
			value: fieldsConfig{},
			expected: []fieldSpec{
				{"Title", typeFor[bool]()},
				{"OriginalTitle", typeFor[bool]()},
				{"Description", typeFor[bool]()},
				{"Director", typeFor[bool]()},
				{"Maker", typeFor[bool]()},
				{"Label", typeFor[bool]()},
				{"Series", typeFor[bool]()},
				{"Genres", typeFor[bool]()},
				{"Actresses", typeFor[bool]()},
			},
		},
		{
			name:  "openAIConfig",
			value: openAIConfig{},
			expected: []fieldSpec{
				{"BaseURL", typeFor[string]()},
				{"APIKey", typeFor[string]()},
				{"Model", typeFor[string]()},
			},
		},
		{
			name:  "deepLConfig",
			value: deepLConfig{},
			expected: []fieldSpec{
				{"Mode", typeFor[models.DeepLMode]()},
				{"BaseURL", typeFor[string]()},
				{"APIKey", typeFor[string]()},
			},
		},
		{
			name:  "googleConfig",
			value: googleConfig{},
			expected: []fieldSpec{
				{"Mode", typeFor[models.GoogleMode]()},
				{"BaseURL", typeFor[string]()},
				{"APIKey", typeFor[string]()},
			},
		},
		{
			name:  "openAICompatibleConfig",
			value: openAICompatibleConfig{},
			expected: []fieldSpec{
				{"BaseURL", typeFor[string]()},
				{"APIKey", typeFor[string]()},
				{"Model", typeFor[string]()},
				{"EnableThinking", typeFor[bool]()},
				{"BackendType", typeFor[string]()},
			},
		},
		{
			name:  "anthropicConfig",
			value: anthropicConfig{},
			expected: []fieldSpec{
				{"BaseURL", typeFor[string]()},
				{"APIKey", typeFor[string]()},
				{"Model", typeFor[string]()},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertStructShape(t, tc.value, tc.expected)
		})
	}
}

func TestConfigBridgeConstruction(t *testing.T) {
	t.Parallel()

	got := Config{
		Enabled:                 true,
		Provider:                "openai-compatible",
		SourceLanguage:          "ja",
		TargetLanguage:          "en",
		TimeoutSeconds:          30,
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
		Fields: fieldsConfig{
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
		OpenAI: openAIConfig{
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "openai-key",
			Model:   "gpt-4o-mini",
		},
		DeepL: deepLConfig{
			Mode:    models.DeepLModeFree,
			BaseURL: "https://api-free.deepl.com",
			APIKey:  "deepl-key",
		},
		Google: googleConfig{
			Mode:    models.GoogleModePaid,
			BaseURL: "https://translation.googleapis.com",
			APIKey:  "google-key",
		},
		OpenAICompatible: openAICompatibleConfig{
			BaseURL:        "http://localhost:11434/v1",
			APIKey:         "local-key",
			Model:          "llama3.1",
			EnableThinking: true,
			BackendType:    "llama.cpp",
		},
		Anthropic: anthropicConfig{
			BaseURL: "https://api.anthropic.com",
			APIKey:  "anthropic-key",
			Model:   "claude-sonnet-4-20250514",
		},
	}

	if !got.Enabled || got.Provider != "openai-compatible" || !got.OpenAICompatible.EnableThinking {
		t.Fatalf("unexpected bridge construction: %+v", got)
	}
	if got.Fields.Actresses != true || got.OpenAI.Model != "gpt-4o-mini" || got.Anthropic.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("bridge fields missing: %+v", got)
	}
}

func TestTranslationConfigSourceShape(t *testing.T) {
	t.Parallel()

	assertStructShape(t, appconfig.TranslationConfig{}, []fieldSpec{
		{"Enabled", typeFor[bool]()},
		{"Provider", typeFor[string]()},
		{"SourceLanguage", typeFor[string]()},
		{"TargetLanguage", typeFor[string]()},
		{"TimeoutSeconds", typeFor[int]()},
		{"ApplyToPrimary", typeFor[bool]()},
		{"OverwriteExistingTarget", typeFor[bool]()},
		{"Fields", reflect.TypeFor[appconfig.TranslationFieldsConfig]()},
		{"OpenAI", reflect.TypeFor[appconfig.OpenAITranslationConfig]()},
		{"DeepL", reflect.TypeFor[appconfig.DeepLTranslationConfig]()},
		{"Google", reflect.TypeFor[appconfig.GoogleTranslationConfig]()},
		{"OpenAICompatible", reflect.TypeFor[appconfig.OpenAICompatibleTranslationConfig]()},
		{"Anthropic", reflect.TypeFor[appconfig.AnthropicTranslationConfig]()},
	})

	assertStructShape(t, appconfig.TranslationFieldsConfig{}, []fieldSpec{
		{"Title", typeFor[bool]()},
		{"OriginalTitle", typeFor[bool]()},
		{"Description", typeFor[bool]()},
		{"Director", typeFor[bool]()},
		{"Maker", typeFor[bool]()},
		{"Label", typeFor[bool]()},
		{"Series", typeFor[bool]()},
		{"Genres", typeFor[bool]()},
		{"Actresses", typeFor[bool]()},
	})

	assertStructShape(t, appconfig.OpenAITranslationConfig{}, []fieldSpec{
		{"BaseURL", typeFor[string]()},
		{"APIKey", typeFor[string]()},
		{"Model", typeFor[string]()},
	})

	assertStructShape(t, appconfig.DeepLTranslationConfig{}, []fieldSpec{
		{"Mode", typeFor[models.DeepLMode]()},
		{"BaseURL", typeFor[string]()},
		{"APIKey", typeFor[string]()},
	})

	assertStructShape(t, appconfig.GoogleTranslationConfig{}, []fieldSpec{
		{"Mode", typeFor[models.GoogleMode]()},
		{"BaseURL", typeFor[string]()},
		{"APIKey", typeFor[string]()},
	})

	assertStructShape(t, appconfig.OpenAICompatibleTranslationConfig{}, []fieldSpec{
		{"BaseURL", typeFor[string]()},
		{"APIKey", typeFor[string]()},
		{"Model", typeFor[string]()},
		{"EnableThinking", reflect.TypeFor[*bool]()},
		{"BackendType", typeFor[string]()},
	})

	assertStructShape(t, appconfig.AnthropicTranslationConfig{}, []fieldSpec{
		{"BaseURL", typeFor[string]()},
		{"APIKey", typeFor[string]()},
		{"Model", typeFor[string]()},
	})
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func assertStructShape(t *testing.T, value any, expected []fieldSpec) {
	t.Helper()

	typ := reflect.TypeOf(value)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("expected struct, got %s", typ.Kind())
	}
	if got := typ.NumField(); got != len(expected) {
		t.Fatalf("unexpected field count for %s: got %d want %d", typ.Name(), got, len(expected))
	}
	for i, want := range expected {
		field := typ.Field(i)
		if field.Name != want.name {
			t.Fatalf("unexpected field %d name for %s: got %s want %s", i, typ.Name(), field.Name, want.name)
		}
		if field.Type != want.typ {
			t.Fatalf("unexpected field %s type for %s: got %s want %s", field.Name, typ.Name(), field.Type, want.typ)
		}
	}
}

func typeFor[T any]() reflect.Type {
	return reflect.TypeFor[T]()
}
