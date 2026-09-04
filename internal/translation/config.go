package translation

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Config is the translation bridge config used by the translation package.
type Config struct {
	Enabled                 bool
	Provider                string
	SourceLanguage          string
	TargetLanguage          string
	TimeoutSeconds          int
	ApplyToPrimary          bool
	OverwriteExistingTarget bool
	Fields                  fieldsConfig
	OpenAI                  openAIConfig
	DeepL                   deepLConfig
	Google                  googleConfig
	OpenAICompatible        openAICompatibleConfig
	Anthropic               anthropicConfig
}

// fieldsConfig controls which metadata fields are translated.
type fieldsConfig struct {
	Title         bool
	OriginalTitle bool
	Description   bool
	Director      bool
	Maker         bool
	Label         bool
	Series        bool
	Genres        bool
	Actresses     bool
}

// openAIConfig holds OpenAI-compatible API settings.
type openAIConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// deepLConfig holds DeepL provider settings.
type deepLConfig struct {
	Mode    models.DeepLMode
	BaseURL string
	APIKey  string
}

// googleConfig holds Google Translate provider settings.
type googleConfig struct {
	Mode    models.GoogleMode
	BaseURL string
	APIKey  string
}

// EffectiveGoogleMode resolves the configured Google mode, applying the
// empty-defaults-to-free rule. It is the single source of truth shared by the
// Google provider dispatch, warning classification messages, and structured
// log fields, so labeling never misreports the mode when it is unset.
func (c Config) EffectiveGoogleMode() models.GoogleMode {
	mode := models.GoogleMode(strings.ToLower(strings.TrimSpace(string(c.Google.Mode))))
	if mode == "" {
		mode = models.GoogleModeFree
	}
	return mode
}

// openAICompatibleConfig holds settings for OpenAI-compatible endpoints.
type openAICompatibleConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	EnableThinking bool
	BackendType    string
}

// anthropicConfig holds Anthropic (Claude) translation settings.
type anthropicConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}
