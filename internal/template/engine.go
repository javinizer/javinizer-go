package template

import (
	"context"
	"fmt"
	"regexp"
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

		// Get the value for this tag
		value, err := e.resolveTag(tagName, modifier, ctx)
		if err != nil {
			// If tag cannot be resolved, keep the original tag or use empty string
			value = ""
		}

		// Replace the tag with its value
		result = strings.Replace(result, fullTag, value, 1)
		if err := e.ensureOutputWithinLimit(result); err != nil {
			return "", err
		}
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

		// Replace the entire conditional block
		result = strings.Replace(result, fullBlock, replacement, 1)
		if err := e.ensureOutputWithinLimit(result); err != nil {
			return "", err
		}
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
		value := ctx.Title
		if modifier != "" {
			// Modifier is max length
			return e.truncate(value, modifier), nil
		}
		return value, nil

	case "ORIGINALTITLE":
		return ctx.OriginalTitle, nil

	case "YEAR":
		if ctx.ReleaseDate != nil {
			return fmt.Sprintf("%d", ctx.ReleaseDate.Year()), nil
		}
		return "", nil

	case "RELEASEDATE":
		if ctx.ReleaseDate != nil {
			if modifier != "" {
				// Custom date format
				return e.formatDate(ctx.ReleaseDate, modifier), nil
			}
			// Default: YYYY-MM-DD
			return ctx.ReleaseDate.Format("2006-01-02"), nil
		}
		return "", nil

	case "RUNTIME":
		if ctx.Runtime > 0 {
			return fmt.Sprintf("%d", ctx.Runtime), nil
		}
		return "", nil

	case "DIRECTOR":
		return ctx.Director, nil

	case "STUDIO", "MAKER":
		return ctx.Maker, nil

	case "LABEL":
		return ctx.Label, nil

	case "SERIES":
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

	// Check if title contains CJK characters
	isCJK := e.containsCJK(title)

	if isCJK {
		// CJK: Hard truncate at character boundary and add ellipsis
		if maxLen > 3 {
			runes := []rune(title)
			if len(runes) > maxLen-3 {
				return string(runes[:maxLen-3]) + "..."
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
				return truncStr[:lastSpace] + "..."
			}
			// No space found, truncate at character boundary
			return truncStr + "..."
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

	ellipsis := "..."
	ellipsisLen := len(ellipsis)

	// If maxBytes is too small for ellipsis + content, just hard truncate
	if maxBytes <= ellipsisLen {
		// Return as many bytes as we can fit (no ellipsis)
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

	// Reserve space for ellipsis
	budget := maxBytes - ellipsisLen
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
		return ellipsis
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

	// Always trim trailing spaces before adding ellipsis
	truncated = strings.TrimRight(truncated, " ")

	return truncated + ellipsis
}

// ValidatePathLength checks if a path exceeds the maximum length
func (e *Engine) ValidatePathLength(path string, maxLen int) error {
	if maxLen <= 0 {
		return nil // No validation if maxLen is not set
	}

	if len(path) > maxLen {
		return fmt.Errorf("path length %d exceeds limit %d: %s", len(path), maxLen, path)
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
