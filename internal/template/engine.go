package template

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Package-level compiled regexes for performance
var (
	cjkRegex              = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`)
	conditionalTokenRegex = regexp.MustCompile(`(?i)<IF:[A-Z_]+>|</IF>`)
)

const (
	DefaultMaxTemplateBytes    = 64 * 1024
	DefaultMaxOutputBytes      = 10 * 1024 * 1024
	DefaultMaxConditionalDepth = 32
)

// EngineOptions defines validation and execution limits for template rendering.
type EngineOptions struct {
	MaxTemplateBytes    int
	MaxOutputBytes      int
	MaxConditionalDepth int
	// Template language configuration (OPT-IN behavior change)
	// Setting DefaultLanguage changes how unqualified tags like <TITLE> behave
	DefaultLanguage   string
	FallbackLanguages []string
}

// parsedModifier represents a parsed tag modifier with language awareness
type parsedModifier struct {
	isLanguage       bool
	languageSpec     string
	legacyModifier   string
	rejectedLanguage bool
}

// Engine is a template processor for format strings
type Engine struct {
	// Tag pattern matches: <TAG>, <TAG:modifier>, <TAG:value>
	tagPattern *regexp.Regexp
	// Conditional pattern matches: <IF:TAG>content</IF>
	conditionalPattern *regexp.Regexp
	options            EngineOptions
}

// NewEngine creates a new template engine
func NewEngine() *Engine {
	return NewEngineWithOptions(EngineOptions{})
}

// NewEngineWithOptions creates a new template engine with custom limits.
func NewEngineWithOptions(opts EngineOptions) *Engine {
	if opts.MaxTemplateBytes <= 0 {
		opts.MaxTemplateBytes = DefaultMaxTemplateBytes
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if opts.MaxConditionalDepth <= 0 {
		opts.MaxConditionalDepth = DefaultMaxConditionalDepth
	}

	opts.DefaultLanguage = normalizeLanguageCode(opts.DefaultLanguage)
	opts.FallbackLanguages = normalizeLanguageList(opts.FallbackLanguages)

	return &Engine{
		// Matches: <ID>, <TITLE:50>, <RELEASEDATE:YYYY-MM-DD>, etc.
		// Case-insensitive to allow <id>, <Id>, <ID>, etc.
		tagPattern: regexp.MustCompile(`(?i)<([A-Z_]+)(?::([^>]+))?>`),
		// Matches: <IF:TAG>content</IF> or <IF:TAG>true<ELSE>false</IF>
		// Case-insensitive to allow <if:tag>, <IF:TAG>, etc.
		conditionalPattern: regexp.MustCompile(`(?i)<IF:([A-Z_]+)>(.*?)(?:<ELSE>(.*?))?</IF>`),
		options:            opts,
	}
}

// Execute processes a template string with the given context
func (e *Engine) Execute(template string, ctx *Context) (string, error) {
	return e.ExecuteWithContext(context.Background(), template, ctx)
}

// ExecuteWithContext processes a template string with cancellation support and output limits.
func (e *Engine) ExecuteWithContext(execCtx context.Context, template string, ctx *Context) (string, error) {
	if execCtx == nil {
		return "", fmt.Errorf("execution context cannot be nil")
	}
	if ctx == nil {
		return "", fmt.Errorf("context cannot be nil")
	}
	if err := e.checkExecutionContext(execCtx); err != nil {
		return "", err
	}
	if err := e.Validate(template); err != nil {
		return "", err
	}

	result := template

	// Step 1: Process conditional blocks first
	var err error
	result, err = e.processConditionalsWithContext(execCtx, result, ctx)
	if err != nil {
		return "", err
	}
	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}

	// Step 2: Process regular tags
	// Build replacement map to avoid quadratic string operations
	tagReplacements := make(map[string]string)
	matches := e.tagPattern.FindAllStringSubmatch(result, -1)

	for i, match := range matches {
		if i%25 == 0 {
			if err := e.checkExecutionContext(execCtx); err != nil {
				return "", err
			}
		}

		fullTag := match[0]                  // e.g., "<TITLE:50>" or "<title:50>"
		tagName := strings.ToUpper(match[1]) // Normalize to uppercase: "TITLE"
		modifier := ""
		if len(match) > 2 {
			modifier = match[2] // e.g., "50"
		}

		// Get the value for this tag (only once per unique fullTag)
		if _, seen := tagReplacements[fullTag]; !seen {
			value, err := e.resolveTag(tagName, modifier, ctx)
			if err != nil {
				// If tag cannot be resolved, use empty string
				value = ""
			}
			tagReplacements[fullTag] = value
		}
	}

	// Replace all tags at once using single-pass replacement
	result = e.tagPattern.ReplaceAllStringFunc(result, func(match string) string {
		return tagReplacements[match]
	})

	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}

	// Note: sanitization is done by caller if needed
	// We don't sanitize here because templates might be used for folder paths
	// which need to preserve slashes

	if err := e.checkExecutionContext(execCtx); err != nil {
		return "", err
	}
	return result, nil
}

// processConditionals processes conditional blocks in the template
func (e *Engine) processConditionalsWithContext(execCtx context.Context, template string, ctx *Context) (string, error) {
	result := template

	// Find all conditional blocks
	matches := e.conditionalPattern.FindAllStringSubmatch(result, -1)

	// Build replacement map to avoid quadratic string operations
	blockReplacements := make(map[string]string)

	for i, match := range matches {
		if i%25 == 0 {
			if err := e.checkExecutionContext(execCtx); err != nil {
				return "", err
			}
		}
		fullBlock := match[0]                // e.g., "<IF:SERIES>Series: <SERIES></IF>" or "<if:series>..."
		tagName := strings.ToUpper(match[1]) // Normalize to uppercase: "SERIES"
		trueContent := match[2]              // e.g., "Series: <SERIES>"
		falseContent := ""
		if len(match) > 3 {
			falseContent = match[3] // Content after <ELSE>
		}

		// Check if the tag has a value
		value, _ := e.resolveTag(tagName, "", ctx)
		hasValue := value != ""

		// Choose which content to use
		replacement := ""
		if hasValue {
			replacement = trueContent
		} else {
			replacement = falseContent
		}

		blockReplacements[fullBlock] = replacement
	}

	// Replace all conditional blocks at once using single-pass replacement
	result = e.conditionalPattern.ReplaceAllStringFunc(result, func(match string) string {
		return blockReplacements[match]
	})

	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}

	return result, nil
}

// Validate checks template shape and size before execution.
func (e *Engine) Validate(template string) error {
	if len(template) > e.options.MaxTemplateBytes {
		return fmt.Errorf("template size %d exceeds maximum %d bytes", len(template), e.options.MaxTemplateBytes)
	}

	depth := 0
	tokens := conditionalTokenRegex.FindAllString(template, -1)
	for _, token := range tokens {
		if strings.HasPrefix(strings.ToUpper(token), "<IF:") {
			depth++
			if depth > e.options.MaxConditionalDepth {
				return fmt.Errorf("conditional depth %d exceeds maximum %d", depth, e.options.MaxConditionalDepth)
			}
			continue
		}

		depth--
		if depth < 0 {
			return fmt.Errorf("invalid template conditionals: unexpected closing </IF>")
		}
	}

	if depth != 0 {
		return fmt.Errorf("invalid template conditionals: unclosed <IF> block")
	}

	return nil
}

func (e *Engine) ensureOutputWithinLimit(output string) error {
	if len(output) > e.options.MaxOutputBytes {
		return fmt.Errorf("rendered template size %d exceeds maximum %d bytes", len(output), e.options.MaxOutputBytes)
	}
	return nil
}

func (e *Engine) checkExecutionContext(execCtx context.Context) error {
	if err := execCtx.Err(); err != nil {
		return fmt.Errorf("template execution canceled: %w", err)
	}
	return nil
}

// resolveTag resolves a tag to its value
func (e *Engine) resolveTag(tagName, modifier string, ctx *Context) (string, error) {
	parsed := e.parseModifier(tagName, modifier)

	switch tagName {
	case "ID":
		value := ctx.ID
		if modifier != "" {
			return e.applyCaseModifier(value, modifier), nil
		}
		return value, nil

	case "CONTENTID":
		value := ctx.ContentID
		if modifier != "" {
			return e.applyCaseModifier(value, modifier), nil
		}
		return value, nil

	case "TITLE":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			value := e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx)
			if parsed.legacyModifier != "" {
				return e.truncate(value, parsed.legacyModifier), nil
			}
			return value, nil
		}
		value := ctx.Title
		if modifier != "" {
			return e.truncate(value, modifier), nil
		}
		return value, nil

	case "ORIGINALTITLE":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.OriginalTitle, nil

	case "YEAR":
		if ctx.ReleaseDate != nil {
			return fmt.Sprintf("%d", ctx.ReleaseDate.Year()), nil
		}
		return "", nil

	case "RELEASEDATE":
		if ctx.ReleaseDate != nil {
			if modifier != "" {
				return e.formatDate(ctx.ReleaseDate, modifier), nil
			}
			return ctx.ReleaseDate.Format("2006-01-02"), nil
		}
		return "", nil

	case "RUNTIME":
		if ctx.Runtime > 0 {
			return fmt.Sprintf("%d", ctx.Runtime), nil
		}
		return "", nil

	case "DIRECTOR":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Director, nil

	case "DESCRIPTION":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Description, nil

	case "STUDIO", "MAKER":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Maker, nil

	case "LABEL":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Label, nil

	case "SERIES":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Series, nil

	case "ACTORS", "ACTRESSES":
		if len(ctx.Actresses) > 0 {
			// Check if GroupActress is enabled and there are multiple actresses
			if ctx.GroupActress && len(ctx.Actresses) > 1 {
				return "@Group", nil
			}

			delimiter := ", "
			if modifier != "" {
				delimiter = modifier
			}
			return strings.Join(ctx.Actresses, delimiter), nil
		}
		return "", nil

	case "GENRES":
		if len(ctx.Genres) > 0 {
			delimiter := ", "
			if modifier != "" {
				delimiter = modifier
			}
			return strings.Join(ctx.Genres, delimiter), nil
		}
		return "", nil

	case "FILENAME":
		return ctx.OriginalFilename, nil

	case "INDEX":
		// For screenshot numbering, etc.
		if modifier != "" {
			// Modifier is padding width
			if ctx.Index > 0 {
				format := fmt.Sprintf("%%0%sd", modifier)
				return fmt.Sprintf(format, ctx.Index), nil
			}
		}
		if ctx.Index > 0 {
			return fmt.Sprintf("%d", ctx.Index), nil
		}
		return "", nil

	case "FIRSTNAME":
		return ctx.FirstName, nil

	case "LASTNAME":
		return ctx.LastName, nil

	case "ACTORNAME":
		// For actress filenames, use the title as the actress name
		return ctx.Title, nil

	case "RESOLUTION":
		// Extract resolution from video file
		info := ctx.GetMediaInfo()
		if info != nil {
			return info.GetResolution(), nil
		}
		return "", nil

	case "PART", "DISC":
		// Multi-part file part number
		if ctx.PartNumber > 0 {
			if modifier != "" {
				// Modifier is padding width (e.g., <PART:2> -> "01")
				format := fmt.Sprintf("%%0%sd", modifier)
				return fmt.Sprintf(format, ctx.PartNumber), nil
			}
			return fmt.Sprintf("%d", ctx.PartNumber), nil
		}
		return "", nil

	case "PARTSUFFIX":
		// Original part suffix from filename (e.g., "-pt1", "-A", "-cd2")
		return ctx.PartSuffix, nil

	case "MULTIPART":
		// Returns "true" if multi-part, empty string otherwise (for use in conditionals)
		if ctx.IsMultiPart {
			return "true", nil
		}
		return "", nil

	default:
		return "", fmt.Errorf("unknown tag: %s", tagName)
	}
}

// TruncateTitle smartly truncates a title to maxLen characters
func (e *Engine) TruncateTitle(title string, maxLen int) string {
	if maxLen <= 0 || len(title) <= maxLen {
		return title
	}

	marker := "~"

	// Check if title contains CJK characters
	isCJK := e.containsCJK(title)

	if isCJK {
		// CJK: Hard truncate at character boundary and add truncation marker
		if maxLen > 3 {
			runes := []rune(title)
			if len(runes) > maxLen-3 {
				return string(runes[:maxLen-3]) + marker
			}
		}
		return title
	}

	// English/Latin: Smart truncate at word boundary (rune-aware)
	runes := []rune(title)
	if maxLen > 3 {
		if len(runes) > maxLen-3 {
			truncated := runes[:maxLen-3]
			truncStr := string(truncated)
			lastSpace := strings.LastIndex(truncStr, " ")
			if lastSpace > 0 {
				return truncStr[:lastSpace] + marker
			}
			// No space found, truncate at character boundary
			return truncStr + marker
		}
		return title
	}

	// maxLen <= 3: truncate at rune boundary
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return title
}

// TruncateTitleBytes smartly truncates a title to fit within maxBytes (byte length)
// This is needed because file paths have byte length limits, not rune count limits
func (e *Engine) TruncateTitleBytes(title string, maxBytes int) string {
	// Handle edge cases
	if maxBytes <= 0 {
		return ""
	}
	if len(title) <= maxBytes {
		return title
	}

	marker := "~"
	markerReserve := 3

	// Preserve legacy budget behavior by reserving 3 bytes for truncation marker.
	// This keeps truncation cut points stable while changing only visible suffix.
	if maxBytes <= markerReserve {
		// Return as many bytes as we can fit (no marker)
		runes := []rune(title)
		currentBytes := 0
		for i, r := range runes {
			runeSize := len(string(r))
			if currentBytes+runeSize > maxBytes {
				if i == 0 {
					return "" // Can't fit even one rune
				}
				return string(runes[:i])
			}
			currentBytes += runeSize
		}
		return title // Shouldn't reach here
	}

	// Reserve space for marker
	budget := maxBytes - markerReserve
	runes := []rune(title)
	currentBytes := 0
	endIdx := 0

	// Find the cut point within budget
	for i, r := range runes {
		runeSize := len(string(r))
		if currentBytes+runeSize > budget {
			break
		}
		currentBytes += runeSize
		endIdx = i + 1 // +1 because we want slice [:endIdx]
	}

	if endIdx == 0 {
		// Can't fit even one rune in budget
		return marker
	}

	// Build the truncated string
	truncated := string(runes[:endIdx])

	// For non-CJK text, try to break at word boundary
	if !e.containsCJK(title) {
		// Find the last space in the truncated string
		lastSpacePos := strings.LastIndex(truncated, " ")
		if lastSpacePos > 0 {
			// Use word boundary
			truncated = truncated[:lastSpacePos]
		}
	}

	// Always trim trailing spaces before adding marker
	truncated = strings.TrimRight(truncated, " ")

	return truncated + marker
}

func (e *Engine) ValidatePathLength(path string, maxLen int) error {
	if maxLen <= 0 {
		return nil
	}
	if len(path) > maxLen {
		return fmt.Errorf("path length %d exceeds limit %d", len(path), maxLen)
	}
	return nil
}

// containsCJK checks if a string contains CJK characters
func (e *Engine) containsCJK(s string) bool {
	// Check for CJK characters (Chinese, Japanese, Korean)
	// Uses package-level cached regex for performance
	return cjkRegex.MatchString(s)
}

// truncate limits a string to maxLen characters (legacy function for backward compatibility)
func (e *Engine) truncate(s string, maxLenStr string) string {
	var maxLen int
	_, err := fmt.Sscanf(maxLenStr, "%d", &maxLen)
	if err != nil || maxLen <= 0 {
		return s
	}

	// Use the new smart truncation
	return e.TruncateTitle(s, maxLen)
}

// formatDate formats a date according to a pattern
func (e *Engine) formatDate(date *time.Time, pattern string) string {
	// Map common patterns to Go's time format
	pattern = strings.ReplaceAll(pattern, "YYYY", "2006")
	pattern = strings.ReplaceAll(pattern, "YY", "06")
	pattern = strings.ReplaceAll(pattern, "MM", "01")
	pattern = strings.ReplaceAll(pattern, "DD", "02")
	pattern = strings.ReplaceAll(pattern, "HH", "15")
	pattern = strings.ReplaceAll(pattern, "mm", "04")
	pattern = strings.ReplaceAll(pattern, "ss", "05")

	return date.Format(pattern)
}

// applyCaseModifier applies case conversion modifiers (UPPERCASE, LOWERCASE)
func (e *Engine) applyCaseModifier(value, modifier string) string {
	switch strings.ToUpper(modifier) {
	case "UPPERCASE", "UPPER":
		return strings.ToUpper(value)
	case "LOWERCASE", "LOWER":
		return strings.ToLower(value)
	default:
		// Unknown modifier, return value as-is
		return value
	}
}

// normalizeLanguageList normalizes and deduplicates a list of language codes.
// Removes invalid codes and ensures deterministic ordering.
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

// parseModifier parses a tag modifier into language spec and legacy modifier components.
// Parsing is STRICT: invalid language specs fall back to treating the modifier as legacy.
// For TITLE tag: numeric modifiers are preserved for truncation behavior.
// Language specs are normalized to lowercase 2-letter codes.
func (e *Engine) parseModifier(tagName, modifier string) parsedModifier {
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
	if tagName == "TITLE" && e.isNumericModifier(modifier) {
		return parsedModifier{
			legacyModifier: modifier,
		}
	}

	// For translatable tags, detect if modifier looks like a language spec
	// If it does but is invalid, reject it to preserve base-field fallback behavior
	if e.isTranslatableTag(tagName) && e.looksLikeLanguageSpec(modifier) {
		return parsedModifier{rejectedLanguage: true}
	}

	// For all other cases, treat as legacy modifier
	return parsedModifier{
		legacyModifier: modifier,
	}
}

// isNumericModifier checks if a modifier string represents a positive integer.
func (e *Engine) isNumericModifier(modifier string) bool {
	if modifier == "" {
		return false
	}
	n, err := strconv.Atoi(modifier)
	return err == nil && n > 0
}

func (e *Engine) looksLikeLanguageSpec(modifier string) bool {
	if modifier == "" {
		return false
	}

	if strings.Contains(modifier, "|") {
		return true
	}

	trimmed := strings.TrimSpace(modifier)
	if idx := strings.IndexAny(trimmed, "-_"); idx > 0 {
		prefix := trimmed[:idx]
		if len(prefix) == 2 {
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

func (e *Engine) isTranslatableTag(tagName string) bool {
	switch tagName {
	case "TITLE", "ORIGINALTITLE", "DIRECTOR", "MAKER", "STUDIO", "LABEL", "SERIES", "DESCRIPTION":
		return true
	default:
		return false
	}
}

// languageCandidates builds the language resolution precedence list.
// Explicit lang > Context default > Engine default > Engine fallbacks.
// All languages are normalized to ensure consistent map lookups.
func (e *Engine) languageCandidates(explicitLang string, ctx *Context) []string {
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
	if e.options.DefaultLanguage != "" {
		addCandidate(e.options.DefaultLanguage)
	}

	// 4. Engine fallback languages (already normalized at construction)
	for _, lang := range e.options.FallbackLanguages {
		addCandidate(lang)
	}

	return candidates
}

// resolveTranslatedTag resolves a translatable tag using the translation system.
// Returns the translated value if found, or falls back to base field.
func (e *Engine) resolveTranslatedTag(tagName, explicitLang string, ctx *Context) string {
	candidates := e.languageCandidates(explicitLang, ctx)

	for _, lang := range candidates {
		value := e.translationFieldValue(tagName, lang, ctx)
		if value != "" {
			return value
		}
	}

	// Fallback to base field (no translation)
	return e.resolveBaseTag(tagName, ctx)
}

// resolveBaseTag resolves a tag from the base Context fields (no translation).
func (e *Engine) resolveBaseTag(tagName string, ctx *Context) string {
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
	case "SERIES":
		return ctx.Series
	case "DESCRIPTION":
		return ctx.Description
	default:
		return ""
	}
}

// translationFieldValue extracts a field value from a specific translation.
func (e *Engine) translationFieldValue(tagName, lang string, ctx *Context) string {
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
	case "SERIES":
		return translation.Series
	case "DESCRIPTION":
		return translation.Description
	default:
		return ""
	}
}
