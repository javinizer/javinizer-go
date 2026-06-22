package translation

import "github.com/javinizer/javinizer-go/internal/config"

// ConfigFromApp converts the application translation config into the translation bridge config.
//
// Config-bridge reads: cfg.Metadata.Translation.Enabled, cfg.Metadata.Translation.Provider,
// cfg.Metadata.Translation.SourceLanguage, cfg.Metadata.Translation.TargetLanguage,
// cfg.Metadata.Translation.TimeoutSeconds, cfg.Metadata.Translation.ApplyToPrimary,
// cfg.Metadata.Translation.OverwriteExistingTarget, cfg.Metadata.Translation.Fields,
// cfg.Metadata.Translation.OpenAI, cfg.Metadata.Translation.DeepL,
// cfg.Metadata.Translation.Google, cfg.Metadata.Translation.OpenAICompatible,
// cfg.Metadata.Translation.Anthropic
func ConfigFromApp(cfg config.TranslationConfig) Config {
	return Config{
		Enabled:                 cfg.Enabled,
		Provider:                cfg.Provider,
		SourceLanguage:          cfg.SourceLanguage,
		TargetLanguage:          cfg.TargetLanguage,
		TimeoutSeconds:          cfg.TimeoutSeconds,
		ApplyToPrimary:          cfg.ApplyToPrimary,
		OverwriteExistingTarget: cfg.OverwriteExistingTarget,
		Fields:                  fieldsConfigFromApp(cfg.Fields),
		OpenAI:                  openAIConfigFromApp(cfg.OpenAI),
		DeepL:                   deepLConfigFromApp(cfg.DeepL),
		Google:                  googleConfigFromApp(cfg.Google),
		OpenAICompatible:        openAICompatibleConfigFromApp(cfg.OpenAICompatible),
		Anthropic:               anthropicConfigFromApp(cfg.Anthropic),
	}
}

func fieldsConfigFromApp(cfg config.TranslationFieldsConfig) fieldsConfig {
	return fieldsConfig{
		Title:         cfg.Title,
		OriginalTitle: cfg.OriginalTitle,
		Description:   cfg.Description,
		Director:      cfg.Director,
		Maker:         cfg.Maker,
		Label:         cfg.Label,
		Series:        cfg.Series,
		Genres:        cfg.Genres,
		Actresses:     cfg.Actresses,
	}
}

func openAIConfigFromApp(cfg config.OpenAITranslationConfig) openAIConfig {
	return openAIConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	}
}

func deepLConfigFromApp(cfg config.DeepLTranslationConfig) deepLConfig {
	return deepLConfig{
		Mode:    cfg.Mode,
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
	}
}

func googleConfigFromApp(cfg config.GoogleTranslationConfig) googleConfig {
	return googleConfig{
		Mode:    cfg.Mode,
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
	}
}

func openAICompatibleConfigFromApp(cfg config.OpenAICompatibleTranslationConfig) openAICompatibleConfig {
	return openAICompatibleConfig{
		BaseURL:        cfg.BaseURL,
		APIKey:         cfg.APIKey,
		Model:          cfg.Model,
		EnableThinking: cfg.EffectiveEnableThinking(),
		BackendType:    cfg.NormalizedBackendType(),
	}
}

func anthropicConfigFromApp(cfg config.AnthropicTranslationConfig) anthropicConfig {
	return anthropicConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	}
}
