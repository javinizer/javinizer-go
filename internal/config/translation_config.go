package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// FlareSolverrConfig holds FlareSolverr configuration for bypassing Cloudflare
type FlareSolverrConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`         // Enable FlareSolverr for bypassing Cloudflare
	URL        string `yaml:"url" json:"url"`                 // FlareSolverr endpoint (default: http://localhost:8191/v1)
	Timeout    int    `yaml:"timeout" json:"timeout"`         // Request timeout in seconds (default: 30)
	MaxRetries int    `yaml:"max_retries" json:"max_retries"` // Max retry attempts for FlareSolverr calls (default: 3)
	SessionTTL int    `yaml:"session_ttl" json:"session_ttl"` // Session TTL in seconds (default: 300)
}

// MetadataConfig holds metadata aggregation settings
type MetadataConfig struct {
	Priority         PriorityConfig         `yaml:"priority" json:"priority"`
	ActressDatabase  ActressDatabaseConfig  `yaml:"actress_database" json:"actress_database"`   // Actress image database (SQLite-backed)
	GenreReplacement GenreReplacementConfig `yaml:"genre_replacement" json:"genre_replacement"` // Genre replacement/normalization (SQLite-backed)
	WordReplacement  WordReplacementConfig  `yaml:"word_replacement" json:"word_replacement"`   // Word uncensor/text replacement (SQLite-backed)
	TagDatabase      TagDatabaseConfig      `yaml:"tag_database" json:"tag_database"`           // Per-movie tag database (SQLite-backed)
	Translation      TranslationConfig      `yaml:"translation" json:"translation"`             // Metadata translation pipeline
	IgnoreGenres     []string               `yaml:"ignore_genres" json:"ignore_genres"`
	RequiredFields   []string               `yaml:"required_fields" json:"required_fields"`
	NFO              NFOConfig              `yaml:"nfo" json:"nfo"`
}

// TranslationConfig holds metadata translation settings.
type TranslationConfig struct {
	Enabled                 bool                              `yaml:"enabled" json:"enabled"`                                     // Enable metadata translation after aggregation
	Provider                string                            `yaml:"provider" json:"provider"`                                   // openai, openai-compatible, anthropic, deepl, google
	SourceLanguage          string                            `yaml:"source_language" json:"source_language"`                     // Source language code (e.g., en, ja, auto)
	TargetLanguage          string                            `yaml:"target_language" json:"target_language"`                     // Target language code (e.g., en, ja, zh)
	TimeoutSeconds          int                               `yaml:"timeout_seconds" json:"timeout_seconds"`                     // Request timeout in seconds
	ApplyToPrimary          bool                              `yaml:"apply_to_primary" json:"apply_to_primary"`                   // Replace primary movie metadata with translated text
	OverwriteExistingTarget bool                              `yaml:"overwrite_existing_target" json:"overwrite_existing_target"` // Overwrite target-language translation if already present
	Fields                  TranslationFieldsConfig           `yaml:"fields" json:"fields"`                                       // Per-field translation controls
	OpenAI                  OpenAITranslationConfig           `yaml:"openai" json:"openai"`                                       // OpenAI/OpenAI-compatible provider settings
	DeepL                   DeepLTranslationConfig            `yaml:"deepl" json:"deepl"`                                         // DeepL provider settings
	Google                  GoogleTranslationConfig           `yaml:"google" json:"google"`                                       // Google provider settings
	OpenAICompatible        OpenAICompatibleTranslationConfig `yaml:"openai_compatible" json:"openai_compatible"`                 // OpenAI-compatible (Ollama, vLLM, etc.) provider settings
	Anthropic               AnthropicTranslationConfig        `yaml:"anthropic" json:"anthropic"`                                 // Anthropic (Claude) provider settings
}

// TranslationFieldsConfig controls which metadata fields are translated.
type TranslationFieldsConfig struct {
	Title         bool `yaml:"title" json:"title"`
	OriginalTitle bool `yaml:"original_title" json:"original_title"`
	Description   bool `yaml:"description" json:"description"`
	Director      bool `yaml:"director" json:"director"`
	Maker         bool `yaml:"maker" json:"maker"`
	Label         bool `yaml:"label" json:"label"`
	Series        bool `yaml:"series" json:"series"`
	Genres        bool `yaml:"genres" json:"genres"`
	Actresses     bool `yaml:"actresses" json:"actresses"`
}

// OpenAITranslationConfig holds OpenAI-compatible API settings.
type OpenAITranslationConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"` // OpenAI-compatible base URL (e.g., https://api.openai.com/v1)
	APIKey  string `yaml:"api_key" json:"api_key"`   // API key for the provider
	Model   string `yaml:"model" json:"model"`       // Model name (e.g., gpt-4o-mini)
}

// DeepLTranslationConfig holds DeepL provider settings.
type DeepLTranslationConfig struct {
	Mode    string `yaml:"mode" json:"mode"`         // free or pro
	BaseURL string `yaml:"base_url" json:"base_url"` // Optional override (defaults to mode-specific endpoint)
	APIKey  string `yaml:"api_key" json:"api_key"`   // DeepL API key
}

// GoogleTranslationConfig holds Google Translate provider settings.
type GoogleTranslationConfig struct {
	Mode    string `yaml:"mode" json:"mode"`         // free or paid
	BaseURL string `yaml:"base_url" json:"base_url"` // Optional override
	APIKey  string `yaml:"api_key" json:"api_key"`   // Required for paid mode
}

// OpenAICompatibleTranslationConfig holds settings for self-hosted or third-party
// OpenAI-compatible translation endpoints (Ollama, vLLM, LM Studio, OpenRouter, etc.).
type OpenAICompatibleTranslationConfig struct {
	BaseURL        string `yaml:"base_url" json:"base_url"`                         // e.g., http://localhost:11434/v1
	APIKey         string `yaml:"api_key" json:"api_key"`                           // Optional for local endpoints
	Model          string `yaml:"model" json:"model"`                               // e.g., llama3.1
	EnableThinking *bool  `yaml:"enable_thinking" json:"enable_thinking,omitempty"` // Toggle reasoning/thinking when supported by the backend
	BackendType    string `yaml:"backend_type,omitempty" json:"backend_type,omitempty" swaggerignore:"true"`
}

// AnthropicTranslationConfig holds Anthropic (Claude) translation settings.
type AnthropicTranslationConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"` // e.g., https://api.anthropic.com
	APIKey  string `yaml:"api_key" json:"api_key"`   // Required
	Model   string `yaml:"model" json:"model"`       // e.g., claude-sonnet-4-20250514
}

// PriorityConfig defines scraper priority for metadata aggregation.
// Supports both a global priority list and per-field overrides.
// When marshaled, per-field entries appear as top-level keys in the priority object:
//
//	priority:
//	  id: [r18dev, dmm]
//	  title: [dmm, r18dev]
//
// The Priority field is the global default; Fields overrides per metadata field.
type PriorityConfig struct {
	// Priority is the global scraper execution order.
	// If empty, derived from registered scraper priorities at initialization.
	// If set, used directly for all metadata fields that lack a Fields override.
	Priority []string `yaml:"priority" json:"priority"`
	// Fields holds per-metadata-field scraper priority overrides.
	// Keys are snake_case field names matching the API (e.g. "title", "actress", "cover_url").
	// An empty or nil slice for a field means "use global priority".
	Fields map[string][]string `yaml:"-" json:"-"`
}

// GetFieldPriority returns the priority list for a specific metadata field.
// If the field has no override (or the override is empty), it falls back to
// the global Priority list. Returns nil if neither is set.
func (p *PriorityConfig) GetFieldPriority(fieldKey string) []string {
	if p == nil {
		return nil
	}
	if override, ok := p.Fields[fieldKey]; ok && len(override) > 0 {
		return override
	}
	if len(p.Priority) > 0 {
		return p.Priority
	}
	return nil
}

// MarshalJSON serializes PriorityConfig as a flat JSON object.
// The "priority" key holds the global list; per-field keys hold overrides.
func (p PriorityConfig) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 1+len(p.Fields))
	if p.Priority != nil {
		m["priority"] = p.Priority
	}
	for k, v := range p.Fields {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON deserializes PriorityConfig from a flat JSON object.
// The "priority" key populates the global list; all other array-valued keys
// populate Fields.
func (p *PriorityConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return p.decodeFromMap(raw)
}

// MarshalYAML serializes PriorityConfig as a flat YAML mapping.
func (p PriorityConfig) MarshalYAML() (interface{}, error) {
	m := make(map[string]any, 1+len(p.Fields))
	if p.Priority != nil {
		m["priority"] = p.Priority
	}
	for k, v := range p.Fields {
		m[k] = v
	}
	return m, nil
}

// UnmarshalYAML deserializes PriorityConfig from a YAML mapping node.
func (p *PriorityConfig) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == 0 {
		return nil
	}
	var raw map[string]any
	if err := node.Decode(&raw); err != nil {
		return err
	}
	return p.decodeFromMap(raw)
}

// decodeFromMap populates Priority and Fields from a generic map.
// "priority" key → Priority; all other string-array keys → Fields.
func (p *PriorityConfig) decodeFromMap(raw map[string]any) error {
	p.Fields = make(map[string][]string)
	for key, value := range raw {
		if key == "priority" {
			if value == nil {
				p.Priority = nil
				continue
			}
			arr, ok := value.([]any)
			if !ok {
				continue
			}
			p.Priority = make([]string, 0, len(arr))
			for _, elem := range arr {
				s, ok := elem.(string)
				if !ok {
					continue
				}
				p.Priority = append(p.Priority, s)
			}
			continue
		}
		// Per-field override
		if value == nil {
			continue
		}
		arr, ok := value.([]any)
		if !ok {
			continue
		}
		fieldPriority := make([]string, 0, len(arr))
		for _, elem := range arr {
			s, ok := elem.(string)
			if !ok {
				continue
			}
			fieldPriority = append(fieldPriority, s)
		}
		p.Fields[key] = fieldPriority
	}
	return nil
}

// ActressDatabaseConfig holds actress image database configuration
type ActressDatabaseConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`             // Enable actress image lookup from database
	AutoAdd      bool `yaml:"auto_add" json:"auto_add"`           // Automatically add new actresses to database
	ConvertAlias bool `yaml:"convert_alias" json:"convert_alias"` // Convert actress names using alias database
}

// GenreReplacementConfig holds genre replacement/normalization configuration
type GenreReplacementConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`   // Enable genre replacement from database
	AutoAdd bool `yaml:"auto_add" json:"auto_add"` // Automatically add new genres to database (identity mapping)
}

// WordReplacementConfig holds word uncensor/text replacement configuration
type WordReplacementConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"` // Enable word replacement from database
}

// TagDatabaseConfig holds per-movie tag database configuration
type TagDatabaseConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"` // Enable per-movie tag lookup from database
}

// NFOConfig holds NFO generation settings
type NFOConfig struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	DisplayTitle         string   `yaml:"display_title" json:"display_title"`
	FilenameTemplate     string   `yaml:"filename_template" json:"filename_template"`
	FirstNameOrder       bool     `yaml:"first_name_order" json:"first_name_order"`
	ActressLanguageJA    bool     `yaml:"actress_language_ja" json:"actress_language_ja"`
	PerFile              bool     `yaml:"per_file" json:"per_file"`                         // Create separate NFO for each multi-part file
	UnknownActressMode   string   `yaml:"unknown_actress_mode" json:"unknown_actress_mode"` // skip (default) or fallback
	UnknownActressText   string   `yaml:"unknown_actress_text" json:"unknown_actress_text"` // Text for fallback mode
	ActressAsTag         bool     `yaml:"actress_as_tag" json:"actress_as_tag"`
	AddGenericRole       bool     `yaml:"add_generic_role" json:"add_generic_role"`         // Add generic "Actress" role to all actresses
	AltNameRole          bool     `yaml:"alt_name_role" json:"alt_name_role"`               // Use alternate name (Japanese) in role field
	IncludeOriginalPath  bool     `yaml:"include_originalpath" json:"include_originalpath"` // Include source filename in NFO
	IncludeStreamDetails bool     `yaml:"include_stream_details" json:"include_stream_details"`
	IncludeFanart        bool     `yaml:"include_fanart" json:"include_fanart"`
	IncludeTrailer       bool     `yaml:"include_trailer" json:"include_trailer"`
	RatingSource         string   `yaml:"rating_source" json:"rating_source"`
	Tag                  []string `yaml:"tag" json:"tag"`
	Tagline              string   `yaml:"tagline" json:"tagline"`
	Credits              []string `yaml:"credits" json:"credits"`
}

func (n *NFOConfig) IsUnknownActressFallback() bool {
	return n.UnknownActressMode == "fallback"
}

// SettingsHash computes a deterministic hash of output-affecting translation settings.
// The hash is used for cache invalidation - when settings change, the hash changes,
// triggering re-translation of cached movies.
// Returns a 16-character hex string (truncated SHA256).
func (tc *TranslationConfig) SettingsHash() string {
	// Extract only output-affecting settings (exclude api_key, base_url, timeout)
	hashInput := settingsHashInput{
		Provider:                tc.Provider,
		SourceLanguage:          strings.ToLower(strings.TrimSpace(tc.SourceLanguage)),
		TargetLanguage:          strings.ToLower(strings.TrimSpace(tc.TargetLanguage)),
		ApplyToPrimary:          tc.ApplyToPrimary,
		OverwriteExistingTarget: tc.OverwriteExistingTarget,
		Fields:                  tc.Fields,
	}

	// Add provider-specific model settings (these affect output)
	switch tc.Provider {
	case "openai":
		hashInput.OpenAIModel = tc.OpenAI.Model
	case "openai_compatible", "openai-compatible":
		hashInput.OpenAICompatibleModel = tc.OpenAICompatible.Model
		hashInput.OpenAICompatibleEnableThinking = tc.OpenAICompatible.EffectiveEnableThinking()
	case "anthropic":
		hashInput.AnthropicModel = tc.Anthropic.Model
	case "deepl":
		hashInput.DeepLMode = tc.DeepL.Mode
	case "google":
		hashInput.GoogleMode = tc.Google.Mode
	}

	// Serialize to JSON with sorted keys for determinism
	jsonBytes, err := json.Marshal(hashInput)
	if err != nil {
		return "" // Should never happen with simple struct
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(jsonBytes)

	// Return truncated hex string (16 chars = 64 bits, sufficient for our use case)
	return hex.EncodeToString(hash[:8])
}

// settingsHashInput is a simplified struct for hash computation.
// Only includes settings that affect translation output.
type settingsHashInput struct {
	Provider                       string                  `json:"provider"`
	SourceLanguage                 string                  `json:"source_language"`
	TargetLanguage                 string                  `json:"target_language"`
	ApplyToPrimary                 bool                    `json:"apply_to_primary"`
	OverwriteExistingTarget        bool                    `json:"overwrite_existing_target"`
	Fields                         TranslationFieldsConfig `json:"fields"`
	OpenAIModel                    string                  `json:"openai_model,omitempty"`
	OpenAICompatibleModel          string                  `json:"openai_compatible_model,omitempty"`
	OpenAICompatibleEnableThinking bool                    `json:"openai_compatible_enable_thinking,omitempty"`
	AnthropicModel                 string                  `json:"anthropic_model,omitempty"`
	DeepLMode                      string                  `json:"deepl_mode,omitempty"`
	GoogleMode                     string                  `json:"google_mode,omitempty"`
}

func (oc OpenAICompatibleTranslationConfig) EffectiveEnableThinking() bool {
	if oc.EnableThinking == nil {
		return false
	}
	return *oc.EnableThinking
}

func (oc OpenAICompatibleTranslationConfig) NormalizedBackendType() string {
	switch strings.ToLower(strings.TrimSpace(oc.BackendType)) {
	case "", "auto":
		return ""
	case "vllm":
		return "vllm"
	case "ollama":
		return "ollama"
	case "llama.cpp", "llamacpp", "llama_cpp":
		return "llama.cpp"
	case "other", "generic":
		return "other"
	default:
		return strings.ToLower(strings.TrimSpace(oc.BackendType))
	}
}
