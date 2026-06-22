package template

import (
	"strconv"
	"strings"
)

// translationResolver handles translation-aware tag resolution.
// It encapsulates language modifier parsing, language candidate ordering,
// and translated field lookup, keeping this concern separate from the
// core Engine tag registry and modifier pipeline.
type translationResolver struct {
	defaultLanguage   string
	fallbackLanguages []string
}

// newTranslationResolver creates a translation resolver from engine options.
func newTranslationResolver(opts engineOptions) *translationResolver {
	return &translationResolver{
		defaultLanguage:   opts.DefaultLanguage,
		fallbackLanguages: opts.FallbackLanguages,
	}
}

// parseModifier parses a tag modifier into language spec and legacy modifier components.
// Parsing is STRICT: invalid language specs fall back to treating the modifier as legacy.
// For TITLE tag: numeric modifiers are preserved for truncation behavior.
// Language specs are normalized to lowercase 2-letter codes.
func (r *translationResolver) parseModifier(tagName, modifier string) parsedModifier {
	if modifier == "" {
		return parsedModifier{}
	}

	// Try to normalize as language spec
	normalized := normalizeLanguageCode(modifier)
	if normalized != "" {
		return parsedModifier{
			isLanguage:   true,
			languageSpec: normalized,
		}
	}

	// Check for fallback chain (e.g., "ja|en")
	if strings.Contains(modifier, "|") {
		// Validate all parts are valid language codes
		parts := strings.Split(modifier, "|")
		valid := true
		for _, part := range parts {
			if normalizeLanguageCode(part) == "" {
				valid = false
				break
			}
		}
		if valid {
			return parsedModifier{
				isLanguage:   true,
				languageSpec: modifier,
			}
		}
	}

	// TITLE is special: numeric modifiers preserved for truncation
	if tagName == "TITLE" && r.isNumericModifier(modifier) {
		return parsedModifier{
			truncationModifier: modifier,
		}
	}

	// For translatable tags, detect if modifier looks like a language spec
	// If it does but is invalid, reject it to preserve base-field fallback behavior
	if r.isTranslatableTag(tagName) && r.looksLikeLanguageSpec(modifier) {
		return parsedModifier{rejectedLanguage: true}
	}

	// For all other cases, treat as truncation modifier
	return parsedModifier{
		truncationModifier: modifier,
	}
}

// isNumericModifier checks if a modifier string represents a positive integer.
func (r *translationResolver) isNumericModifier(modifier string) bool {
	if modifier == "" {
		return false
	}
	n, err := strconv.Atoi(modifier)
	return err == nil && n > 0
}

func (r *translationResolver) looksLikeLanguageSpec(modifier string) bool {
	if modifier == "" {
		return false
	}

	if strings.Contains(modifier, "|") {
		return true
	}

	trimmed := strings.TrimSpace(modifier)
	if idx := strings.IndexAny(trimmed, "-_"); idx > 0 {
		prefix := trimmed[:idx]
		if len(prefix) >= 2 && len(prefix) <= 3 {
			for _, r := range strings.ToLower(prefix) {
				if r < 'a' || r > 'z' {
					return false
				}
			}
			return true
		}
		return false
	}

	lower := strings.ToLower(trimmed)
	if len(lower) >= 2 && len(lower) <= 3 {
		for _, r := range lower {
			if r < 'a' || r > 'z' {
				return false
			}
		}
		return true
	}

	return false
}

func (r *translationResolver) isTranslatableTag(tagName string) bool {
	switch tagName {
	case "TITLE", "ORIGINALTITLE", "DIRECTOR", "MAKER", "STUDIO", "LABEL", "SERIES", "SET", "DESCRIPTION":
		return true
	default:
		return false
	}
}

// languageCandidates builds the language resolution precedence list.
// Explicit lang > Context default > Engine default > Engine fallbacks.
// All languages are normalized to ensure consistent map lookups.
func (r *translationResolver) languageCandidates(explicitLang string, ctx *Context) []string {
	var candidates []string
	seen := map[string]struct{}{}

	addCandidate := func(lang string) {
		lang = normalizeLanguageCode(lang)
		if lang == "" {
			return
		}
		if _, exists := seen[lang]; exists {
			return
		}
		seen[lang] = struct{}{}
		candidates = append(candidates, lang)
	}

	// 1. Explicit language spec takes highest priority
	if explicitLang != "" {
		// Normalize each language in fallback chain
		for _, lang := range strings.Split(explicitLang, "|") {
			addCandidate(lang)
		}
	}

	// 2. Context-level default language override (normalize)
	if ctx.DefaultLanguage != "" {
		addCandidate(ctx.DefaultLanguage)
	}

	// 3. Engine-level default language (already normalized at construction)
	if r.defaultLanguage != "" {
		addCandidate(r.defaultLanguage)
	}

	// 4. Engine fallback languages (already normalized at construction)
	for _, lang := range r.fallbackLanguages {
		addCandidate(lang)
	}

	return candidates
}

// resolveTranslatedTag resolves a translatable tag using the translation system.
// Returns the translated value if found, or falls back to base field.
func (r *translationResolver) resolveTranslatedTag(tagName, explicitLang string, ctx *Context) string {
	candidates := r.languageCandidates(explicitLang, ctx)

	for _, lang := range candidates {
		value := r.translationFieldValue(tagName, lang, ctx)
		if value != "" {
			return value
		}
	}

	// Fallback to base field (no translation)
	return r.resolveBaseTag(tagName, ctx)
}

// resolveBaseTag resolves a tag from the base Context fields (no translation).
func (r *translationResolver) resolveBaseTag(tagName string, ctx *Context) string {
	switch tagName {
	case "TITLE":
		return ctx.Title
	case "ORIGINALTITLE":
		return ctx.OriginalTitle
	case "DIRECTOR":
		return ctx.Director
	case "MAKER", "STUDIO":
		return ctx.Maker
	case "LABEL":
		return ctx.Label
	case "SERIES", "SET":
		return ctx.Series
	case "DESCRIPTION":
		return ctx.Description
	default:
		return ""
	}
}

// translationFieldValue extracts a field value from a specific translation.
func (r *translationResolver) translationFieldValue(tagName, lang string, ctx *Context) string {
	if ctx.Translations == nil {
		return ""
	}

	translation, ok := ctx.Translations[lang]
	if !ok {
		return ""
	}

	switch tagName {
	case "TITLE":
		return translation.Title
	case "ORIGINALTITLE":
		return translation.OriginalTitle
	case "DIRECTOR":
		return translation.Director
	case "MAKER", "STUDIO":
		return translation.Maker
	case "LABEL":
		return translation.Label
	case "SERIES", "SET":
		return translation.Series
	case "DESCRIPTION":
		return translation.Description
	default:
		return ""
	}
}

// normalizeLanguageList deduplicates and normalizes a list of language codes.
func normalizeLanguageList(langs []string) []string {
	if len(langs) == 0 {
		return nil
	}

	out := make([]string, 0, len(langs))
	seen := map[string]struct{}{}
	for _, lang := range langs {
		norm := normalizeLanguageCode(lang)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}
