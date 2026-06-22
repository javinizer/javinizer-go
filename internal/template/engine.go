package template

import (
	"context"
	"fmt"
	"path/filepath"
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

// engineOptions defines validation and execution limits for template rendering.
type engineOptions struct {
	MaxTemplateBytes    int
	MaxOutputBytes      int
	MaxConditionalDepth int
	// Template language configuration (OPT-IN behavior change)
	// Setting DefaultLanguage changes how unqualified tags like <TITLE> behave
	DefaultLanguage   string
	FallbackLanguages []string
}

// parsedModifier represents a parsed tag modifier with language awareness
// plus optional per-tag overrides (ACTORS tags only).
type parsedModifier struct {
	isLanguage         bool
	languageSpec       string
	truncationModifier string
	firstNameOrder     *bool  // nil = use ctx.FirstNameOrder; non-nil = tag-level override
	delimiter          string // explicit per-tag joiner from <TAG:DELIM=X> (may be empty)
	delimSet           bool   // true = DELIM= was present (distinguishes empty value from absent)
	rejectedLanguage   bool
}

// tagResolver resolves a tag name to its raw string value from the context.
// Modifier processing is handled separately by applyModifier.
type tagResolver func(ctx *Context) (string, error)

// listDelimiterSentinel is an internal marker used by list-type tag resolvers
// (ACTORS, ACTRESSES, GENRES) to mark the boundary between items.
// applyModifier replaces this sentinel with the actual delimiter.
const listDelimiterSentinel = "\x00SEP\x00"

// Engine is a template processor for format strings
type Engine struct {
	// Tag pattern matches: <TAG>, <TAG:modifier>, <TAG:value>
	tagPattern *regexp.Regexp
	// Conditional pattern matches: <IF:TAG>content</IF>
	conditionalPattern *regexp.Regexp
	options            engineOptions
	// tagRegistry maps tag names to their resolver functions.
	// Adding a new tag = adding a registry entry.
	tagRegistry map[string]tagResolver
	// modifierPipeline is the ordered list of modifier transformation steps.
	// Each step is tried in order; the first that returns handled=true wins.
	modifierPipeline []modifierStep
	// translationResolver handles language-modifier parsing and translated
	// field lookup, keeping this concern separate from the core engine.
	translationResolver *translationResolver
}

type EngineInterface interface {
	Execute(template string, ctx *Context) (string, error)
	ExecuteWithContext(execCtx context.Context, template string, ctx *Context) (string, error)
	ExecuteWithMaxBytes(tmpl string, ctx *Context, maxBytes int) (string, error)
	TruncateTitle(title string, maxLen int) string
	TruncateTitleBytes(title string, maxBytes int) string
	ValidatePathLength(path string, maxLen int) error
}

var _ EngineInterface = (*Engine)(nil)

// NewEngine creates a new template engine
func NewEngine() *Engine {
	return newEngineWithOptions(engineOptions{})
}

// newEngineWithOptions creates a new template engine with custom limits.
func newEngineWithOptions(opts engineOptions) *Engine {
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

	eng := &Engine{
		// Matches: <ID>, <TITLE:50>, <RELEASEDATE:YYYY-MM-DD>, etc.
		// Case-insensitive to allow <id>, <Id>, <ID>, etc.
		tagPattern: regexp.MustCompile(`(?i)<([A-Z_]+)(?::([^>]+))?>`),
		// Matches: <IF:TAG>content</IF> or <IF:TAG>true<ELSE>false</IF>
		// Case-insensitive to allow <if:tag>, <IF:TAG>, etc.
		conditionalPattern:  regexp.MustCompile(`(?i)<IF:([A-Z_]+)>(.*?)(?:<ELSE>(.*?))?</IF>`),
		options:             opts,
		tagRegistry:         newTagRegistry(),
		translationResolver: newTranslationResolver(opts),
	}
	eng.modifierPipeline = newModifierPipeline(eng)
	return eng
}

// Execute processes a template string with the given context
func (e *Engine) Execute(template string, ctx *Context) (string, error) {
	return e.ExecuteWithContext(context.Background(), template, ctx)
}

func (e *Engine) ExecuteWithMaxBytes(tmpl string, ctx *Context, maxBytes int) (string, error) {
	sentinel := "\x00MAXBYTES\x00"
	frameCtx := ctx.Clone()
	frameCtx.Title = sentinel
	frameCtx.OriginalTitle = sentinel

	frame, err := e.Execute(tmpl, frameCtx)
	if err != nil {
		return e.Execute(tmpl, ctx)
	}

	frameBytes := len(frame) - strings.Count(frame, sentinel)*len(sentinel)
	titleBudget := maxBytes - frameBytes
	if titleBudget <= 0 {
		return e.Execute(tmpl, ctx)
	}

	titleBytes := len(ctx.Title)
	if titleBytes <= titleBudget {
		return e.Execute(tmpl, ctx)
	}

	truncatedCtx := ctx.Clone()
	truncated := e.TruncateTitleBytes(ctx.Title, titleBudget)
	truncatedCtx.Title = truncated
	if ctx.OriginalTitle == ctx.Title {
		truncatedCtx.OriginalTitle = truncated
	} else {
		truncatedCtx.OriginalTitle = e.TruncateTitleBytes(ctx.OriginalTitle, titleBudget)
	}

	return e.Execute(tmpl, truncatedCtx)
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

// resolveTag resolves a tag to its value using the three-step pipeline:
//  1. Lookup tag in registry → resolve raw value
//  2. If translatable tag with valid language spec, override with translated value
//  3. Apply modifier transformation (truncation, case, formatting, etc.)
func (e *Engine) resolveTag(tagName, modifier string, ctx *Context) (string, error) {
	// Actress-family tags (<ACTORS>, <ACTRESSES>, <ACTRESS>, <ACTORNAME>,
	// <ACTRESSNAME>) support per-tag modifiers — language (JA), name order
	// (FIRST/LAST), and delimiter (DELIM=x). These need both the context and
	// the modifier at resolution time, so they bypass the registry/applyModifier
	// split (which keeps modifier handling context-free).
	switch tagName {
	case "ACTORS", "ACTRESSES":
		return e.resolveActressListTag(modifier, ctx), nil
	case "ACTRESS", "ACTORNAME", "ACTRESSNAME":
		return e.resolveActressNameTag(modifier, ctx), nil
	}

	resolver, ok := e.tagRegistry[tagName]
	if !ok {
		return "", fmt.Errorf("unknown tag: %s", tagName)
	}

	value, err := resolver(ctx)
	if err != nil {
		return "", err
	}

	// For translatable tags, check if translation should override the base value
	parsed := e.translationResolver.parseModifier(tagName, modifier)
	if e.translationResolver.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
		translated := e.translationResolver.resolveTranslatedTag(tagName, parsed.languageSpec, ctx)
		if translated != "" {
			value = translated
		}
	}

	value = e.applyModifier(value, modifier, tagName, parsed)

	return value, nil
}

// applyModifier applies post-resolution modifier transformations to a resolved tag value.
// This separates value resolution from value transformation.
// modifierStep is a single transformation step in the modifier pipeline.
// Each step receives the current value, the raw modifier string, the tag name,
// and the parsed modifier. If the step handles the tag, it returns the
// transformed value and true; otherwise it returns the original value and false.
type modifierStep func(value, modifier, tagName string, parsed parsedModifier) (string, bool)

// applyModifier applies post-resolution modifier transformations to a resolved tag value
// using a pipeline of modifierStep functions. Each step is tried in order; the first
// step that returns true (handled) short-circuits the pipeline.
// This separates value resolution from value transformation and keeps each
// modifier type in sync with parseModifier without a monolithic switch.
func (e *Engine) applyModifier(value, modifier, tagName string, parsed parsedModifier) string {
	for _, step := range e.modifierPipeline {
		if result, handled := step(value, modifier, tagName, parsed); handled {
			return result
		}
	}
	return value
}

// newModifierPipeline builds the ordered pipeline of modifier steps.
// The order matters: list-sentinel resolution must run first (even with empty modifier),
// then case-modifier tags, then translatable truncation, then date formatting, then padding.
func newModifierPipeline(e *Engine) []modifierStep {
	return []modifierStep{
		// Step 1: List-type tags — sentinel delimiter resolution.
		func(value, modifier, tagName string, _ parsedModifier) (string, bool) {
			switch tagName {
			case "ACTORS", "ACTRESSES", "GENRES":
				if strings.Contains(value, listDelimiterSentinel) {
					delimiter := ", "
					if modifier != "" {
						delimiter = modifier
					}
					return strings.ReplaceAll(value, listDelimiterSentinel, delimiter), true
				}
			}
			return value, false
		},

		// Step 2: Short-circuit when no modifier is present (after list sentinel check).
		func(value, modifier, _ string, _ parsedModifier) (string, bool) {
			if modifier == "" {
				return value, true
			}
			return value, false
		},

		// Step 3: Case-modifier tags (ID, CONTENTID).
		func(value, modifier, tagName string, _ parsedModifier) (string, bool) {
			switch tagName {
			case "ID", "CONTENTID":
				return e.applyCaseModifier(value, modifier), true
			}
			return value, false
		},

		// Step 4: Translatable tags — truncation from parsed modifier or numeric fallback.
		func(value, modifier, tagName string, parsed parsedModifier) (string, bool) {
			if e.translationResolver.isTranslatableTag(tagName) {
				if parsed.truncationModifier != "" {
					return e.truncate(value, parsed.truncationModifier), true
				}
				// For TITLE with a plain numeric modifier (not a language spec), truncate
				if tagName == "TITLE" && e.translationResolver.isNumericModifier(modifier) {
					return e.truncate(value, modifier), true
				}
			}
			return value, false
		},

		// Step 5: RELEASEDATE format modifier.
		func(value, modifier, tagName string, _ parsedModifier) (string, bool) {
			if tagName == "RELEASEDATE" && value != "" {
				return e.formatDateFromString(value, modifier), true
			}
			return value, false
		},

		// Step 6: Padding modifiers for numeric fields (INDEX, PART, DISC).
		func(value, modifier, tagName string, _ parsedModifier) (string, bool) {
			switch tagName {
			case "INDEX", "PART", "DISC":
				if modifier != "" && value != "" {
					// Preserve original behavior: use modifier as format width even if invalid
					// (e.g., <INDEX:xyz> with Index=5 produces "5yzd" via fmt.Sprintf("%0xyzd", 5))
					format := fmt.Sprintf("%%0%sd", modifier)
					num, parseErr := strconv.Atoi(value)
					if parseErr == nil {
						return fmt.Sprintf(format, num), true
					}
				}
			}
			return value, false
		},
	}
}

// formatDateFromString formats a date string (expected "2006-01-02") according to a pattern.
func (e *Engine) formatDateFromString(dateStr, pattern string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr // Return as-is if parsing fails
	}
	return e.formatDate(&t, pattern)
}

// newTagRegistry builds the map of tag names to resolver functions.
// Each resolver extracts the raw value from context; modifier processing
// is handled separately by applyModifier.
// newSimpleTagRegistry returns direct-field and translatable tags.
func newSimpleTagRegistry() map[string]tagResolver {
	registry := make(map[string]tagResolver)

	registry["ID"] = func(ctx *Context) (string, error) { return ctx.ID, nil }
	registry["CONTENTID"] = func(ctx *Context) (string, error) { return ctx.ContentID, nil }
	registry["FILENAME"] = func(ctx *Context) (string, error) {
		name := ctx.OriginalFilename
		if ext := filepath.Ext(name); ext != "" && len(ext) < len(name) {
			name = strings.TrimSuffix(name, ext)
		}
		return name, nil
	}
	registry["FILENAME_EXT"] = func(ctx *Context) (string, error) { return ctx.OriginalFilename, nil }
	registry["FILENAMEEXT"] = registry["FILENAME_EXT"]
	registry["FIRSTNAME"] = func(ctx *Context) (string, error) { return ctx.FirstName, nil }
	registry["LASTNAME"] = func(ctx *Context) (string, error) { return ctx.LastName, nil }
	registry["PARTSUFFIX"] = func(ctx *Context) (string, error) { return ctx.PartSuffix, nil }

	// Translatable tags — resolved via resolveTranslatedTag with parsed language
	registry["TITLE"] = func(ctx *Context) (string, error) {
		return ctx.Title, nil // applyModifier handles translation when engine has language config
	}
	registry["ORIGINALTITLE"] = func(ctx *Context) (string, error) { return ctx.OriginalTitle, nil }
	registry["DIRECTOR"] = func(ctx *Context) (string, error) { return ctx.Director, nil }
	registry["DESCRIPTION"] = func(ctx *Context) (string, error) { return ctx.Description, nil }
	registry["STUDIO"] = func(ctx *Context) (string, error) { return ctx.Maker, nil }
	registry["MAKER"] = func(ctx *Context) (string, error) { return ctx.Maker, nil }
	registry["LABEL"] = func(ctx *Context) (string, error) { return ctx.Label, nil }
	registry["SERIES"] = func(ctx *Context) (string, error) { return ctx.Series, nil }
	registry["SET"] = func(ctx *Context) (string, error) { return ctx.Series, nil }

	return registry
}

// newComputedTagRegistry returns computed-value tags (YEAR, RELEASEDATE, etc.).
func newComputedTagRegistry() map[string]tagResolver {
	registry := make(map[string]tagResolver)

	registry["YEAR"] = func(ctx *Context) (string, error) {
		if ctx.ReleaseDate != nil {
			return fmt.Sprintf("%d", ctx.ReleaseDate.Year()), nil
		}
		if ctx.ReleaseYear > 0 {
			return fmt.Sprintf("%d", ctx.ReleaseYear), nil
		}
		return "", nil
	}

	registry["RELEASEDATE"] = func(ctx *Context) (string, error) {
		if ctx.ReleaseDate != nil {
			return ctx.ReleaseDate.Format("2006-01-02"), nil
		}
		return "", nil
	}

	registry["RUNTIME"] = func(ctx *Context) (string, error) {
		if ctx.Runtime > 0 {
			return fmt.Sprintf("%d", ctx.Runtime), nil
		}
		return "", nil
	}

	registry["RATING"] = func(ctx *Context) (string, error) {
		if ctx.Rating > 0 {
			return fmt.Sprintf("%.1f", ctx.Rating), nil
		}
		return "", nil
	}

	registry["MULTIPART"] = func(ctx *Context) (string, error) {
		if ctx.IsMultiPart {
			return "true", nil
		}
		return "", nil
	}

	// Numeric tags with padding support
	registry["INDEX"] = func(ctx *Context) (string, error) {
		if ctx.Index > 0 {
			return fmt.Sprintf("%d", ctx.Index), nil
		}
		return "", nil
	}

	registry["PART"] = func(ctx *Context) (string, error) {
		if ctx.PartNumber > 0 {
			return fmt.Sprintf("%d", ctx.PartNumber), nil
		}
		return "", nil
	}
	registry["DISC"] = registry["PART"]

	// Media info tags
	registry["RESOLUTION"] = func(ctx *Context) (string, error) {
		info := ctx.getMediaInfo()
		if info != nil {
			return info.GetResolution(), nil
		}
		return "", nil
	}

	return registry
}

// newListTagRegistry returns list-type tags (ACTORS, GENRES, etc.).
func newListTagRegistry() map[string]tagResolver {
	registry := make(map[string]tagResolver)

	registry["ACTORS"] = func(ctx *Context) (string, error) {
		if len(ctx.Actresses) > 0 {
			if ctx.GroupActress && len(ctx.Actresses) > 1 {
				groupName := ctx.GroupActressName
				if groupName == "" {
					groupName = "@Group"
				}
				return groupName, nil
			}
			names := ctx.formatActressNames()
			return strings.Join(names, listDelimiterSentinel), nil
		}
		return "", nil
	}
	registry["ACTRESSES"] = registry["ACTORS"]

	registry["GENRES"] = func(ctx *Context) (string, error) {
		if len(ctx.Genres) > 0 {
			return strings.Join(ctx.Genres, listDelimiterSentinel), nil
		}
		return "", nil
	}

	return registry
}

// newActressTagRegistry returns actress name resolution tags.
func newActressTagRegistry() map[string]tagResolver {
	registry := make(map[string]tagResolver)

	actressNameResolver := func(ctx *Context) (string, error) {
		if ctx.ActressName != "" {
			return ctx.ActressName, nil
		}
		if len(ctx.ActressDetails) > 0 {
			return ctx.formatActressName(ctx.ActressDetails[0]), nil
		}
		if len(ctx.Actresses) > 0 {
			return ctx.Actresses[0], nil
		}
		return "", nil
	}
	registry["ACTRESS"] = actressNameResolver
	registry["ACTORNAME"] = actressNameResolver
	registry["ACTRESSNAME"] = actressNameResolver

	return registry
}

// newTagRegistry builds the map of tag names to resolver functions by merging
// the four sub-registries.
func newTagRegistry() map[string]tagResolver {
	registry := make(map[string]tagResolver)
	for k, v := range newSimpleTagRegistry() {
		registry[k] = v
	}
	for k, v := range newComputedTagRegistry() {
		registry[k] = v
	}
	for k, v := range newListTagRegistry() {
		registry[k] = v
	}
	for k, v := range newActressTagRegistry() {
		registry[k] = v
	}
	return registry
}

// TruncateTitle smartly truncates a title to maxLen characters
func (e *Engine) TruncateTitle(title string, maxLen int) string {
	if maxLen <= 0 || len(title) <= maxLen {
		return title
	}

	marker := "..."

	isCJK := e.containsCJK(title)

	if isCJK {
		if maxLen > 3 {
			runes := []rune(title)
			if len(runes) > maxLen-3 {
				return string(runes[:maxLen-3]) + marker
			}
		}
		return title
	}

	runes := []rune(title)
	if maxLen > 3 {
		if len(runes) > maxLen-3 {
			truncated := runes[:maxLen-3]
			truncStr := string(truncated)
			lastSpace := strings.LastIndex(truncStr, " ")
			if lastSpace > 0 {
				return truncStr[:lastSpace] + marker
			}
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

	marker := "..."
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

// Deprecated: Use TruncateTitle directly instead of truncate.
// truncate limits a string to maxLen characters, delegating to TruncateTitle.
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

// resolveActressListTag resolves <ACTORS>/<ACTRESSES> with modifier-driven
// behavior: per-tag JA language preference, FIRST/LAST name order, and the
// DELIM= keyword for the joiner. It also honours GroupActress substitution
// (multiple -> @Group, empty/unknown -> @Unknown).
func (e *Engine) resolveActressListTag(modifier string, ctx *Context) string {
	if len(ctx.Actresses) == 0 && len(ctx.ActressDetails) == 0 {
		// No actresses at all. Under GroupActress, mirror the original
		// PowerShell javinizer which substitutes @Unknown when the actress
		// list is empty (so folder naming stays "@Unknown" rather than
		// blank). Without GroupActress, return empty.
		if ctx.GroupActress {
			return resolveGroupUnknownName(ctx.GroupUnknownActressName)
		}
		return ""
	}

	pm := e.parseActressModifier(modifier)
	preferJa := ctx.ActressLanguageJa
	if pm.isLanguage {
		preferJa = languageSpecPrefersJapanese(pm.languageSpec)
	}
	names := ctx.formatActressNamesLang(preferJa, pm.firstNameOrder)

	if ctx.GroupActress {
		if len(names) > 1 {
			groupName := ctx.GroupActressName
			if groupName == "" {
				groupName = "@Group"
			}
			return groupName
		}
		// Single actress: mirror the original javinizer which substitutes
		// @Unknown when the only name is unknown or empty.
		if len(names) == 0 || isUnknownActressName(names[0]) {
			return resolveGroupUnknownName(ctx.GroupUnknownActressName)
		}
		return names[0]
	}

	// Join delimiter precedence (main's coherent ACTORS design):
	//  1. Tag-level DELIM= modifier (explicit, may be empty: <ACTORS:DELIM=>).
	//  2. ctx.ActressDelimiter (config-level actress_delimiter).
	//  3. Default ", ".
	// A bare non-keyword modifier (e.g. <ACTORS:|>) does NOT act as a
	// delimiter for ACTORS — use <ACTORS:DELIM=|> instead. This avoids
	// ambiguity with the JA/FIRST keyword modifiers. (GENRES still supports
	// bare-modifier-as-delimiter via the registry sentinel pipeline.)
	delimiter := ctx.ActressDelimiter
	if delimiter == "" {
		delimiter = ", "
	}
	if pm.delimSet {
		delimiter = pm.delimiter
	}
	return strings.Join(names, delimiter)
}

// resolveActressNameTag resolves the single-name actress tags
// <ACTRESS>, <ACTORNAME>, <ACTRESSNAME> with the same modifier handling as
// resolveActressListTag (JA / FIRST / LAST) applied to the first actress.
func (e *Engine) resolveActressNameTag(modifier string, ctx *Context) string {
	if ctx.ActressName != "" {
		return ctx.ActressName
	}
	pm := e.parseActressModifier(modifier)
	preferJa := ctx.ActressLanguageJa
	if pm.isLanguage {
		preferJa = languageSpecPrefersJapanese(pm.languageSpec)
	}
	if len(ctx.ActressDetails) > 0 {
		return ctx.formatActressNameLang(ctx.ActressDetails[0], preferJa, pm.firstNameOrder)
	}
	if len(ctx.Actresses) > 0 {
		return ctx.Actresses[0]
	}
	return ""
}

// parseActressModifier parses a modifier on the actress tags (<ACTORS>,
// <ACTRESS>, <ACTORNAME>). It supports three kinds of components combined
// with a comma:
//   - Delimiter: DELIM=<value> — explicit joiner between names. Captures the
//     remainder of the modifier literally (may contain commas).
//   - Language spec: JA, EN, JA|EN, ...
//   - Name order: FIRST|FIRSTNAMEORDER | LAST|LASTNAMEORDER
//
// Hard break: the legacy implicit-delimiter form <ACTORS:|> is no longer
// supported. Use <ACTORS:DELIM=|> instead. When the modifier has no
// recognized keyword and no DELIM= prefix, parseActressModifier returns an
// empty parsedModifier and the resolver falls back to ctx.ActressDelimiter
// (the configured actress_delimiter, default ", ").
func (e *Engine) parseActressModifier(modifier string) parsedModifier {
	if modifier == "" {
		return parsedModifier{}
	}

	// Extract DELIM=value first so the value can freely contain commas.
	// The keyword is case-insensitive but the value itself is preserved as-is.
	var delimited string
	delimSet := false
	prefix := modifier
	if idx := strings.Index(strings.ToUpper(modifier), "DELIM="); idx >= 0 {
		delimSet = true
		delimited = modifier[idx+len("DELIM="):]
		prefix = modifier[:idx]
		// Trim any trailing comma in prefix so the keyword-split step doesn't
		// see an empty trailing element.
		prefix = strings.TrimRight(prefix, ",")
	}

	// Split the prefix on commas into keyword components.
	var parts []string
	if prefix != "" {
		if strings.Contains(prefix, ",") {
			parts = strings.Split(prefix, ",")
		} else {
			parts = []string{prefix}
		}
	}

	var (
		languageSpec   string
		firstNameOrder *bool
		sawKeyword     bool
	)
	for _, raw := range parts {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if order := parseActressOrderModifier(trimmed); order != nil {
			firstNameOrder = order
			sawKeyword = true
			continue
		}
		if e.translationResolver.looksLikeLanguageSpec(trimmed) {
			// Accept the component; preserve the original (single code or chain).
			normalized := normalizeLanguageCode(trimmed)
			if normalized == "" {
				// try as fallback chain
				chainParts := strings.Split(trimmed, "|")
				valid := true
				for _, p := range chainParts {
					if normalizeLanguageCode(p) == "" {
						valid = false
						break
					}
				}
				if !valid {
					continue
				}
				normalized = trimmed
			}
			if normalized != "" {
				if languageSpec != "" {
					languageSpec = languageSpec + "|" + normalized
				} else {
					languageSpec = normalized
				}
				sawKeyword = true
			}
		}
	}

	// No DELIM= and no keyword: hard break — return empty so the resolver
	// falls back to ctx.ActressDelimiter.
	if !sawKeyword && !delimSet {
		return parsedModifier{}
	}

	pm := parsedModifier{firstNameOrder: firstNameOrder, delimSet: delimSet}
	if languageSpec != "" {
		pm.isLanguage = true
		pm.languageSpec = languageSpec
	}
	pm.delimiter = delimited
	return pm
}

// parseActressOrderModifier recognizes name-order keywords for the
// <ACTORS>/<ACTRESS>/<ACTORNAME> tags. Returns nil if the token is not an
// order modifier.
func parseActressOrderModifier(part string) *bool {
	switch strings.ToLower(strings.TrimSpace(part)) {
	case "first", "firstnameorder":
		b := true
		return &b
	case "last", "lastnameorder":
		b := false
		return &b
	}
	return nil
}

// languageSpecPrefersJapanese reports whether the given language spec (which
// may be a single code like "ja" or a fallback chain like "ja|en") includes
// Japanese. Used by the <ACTORS:JA> / <ACTRESS:JA> tag modifiers to select
// actress Japanese-name rendering.
func languageSpecPrefersJapanese(languageSpec string) bool {
	for _, lang := range strings.Split(languageSpec, "|") {
		if strings.EqualFold(strings.TrimSpace(lang), "ja") {
			return true
		}
	}
	return false
}

// resolveGroupUnknownName returns the configured @Unknown replacement name,
// falling back to the original PowerShell javinizer's hard-coded "@Unknown".
// Used by the <ACTORS>/<ACTRESSES> resolver when GroupActress is enabled and
// the actress list is empty or the only name is unknown.
func resolveGroupUnknownName(configured string) string {
	if configured == "" {
		return "@Unknown"
	}
	return configured
}

// isUnknownActressName reports whether a name should be treated as 'unknown'
// for GroupActress substitution. Mirrors the original PowerShell javinizer
// check: an exact empty string, or any name containing "unknown"
// (case-insensitive).
func isUnknownActressName(name string) bool {
	if name == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), "unknown")
}
