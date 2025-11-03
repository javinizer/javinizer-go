package template

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Package-level compiled regexes for performance
var (
	cjkRegex = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`)
)

// Engine is a template processor for format strings
type Engine struct {
	// Tag pattern matches: <TAG>, <TAG:modifier>, <TAG:value>
	tagPattern *regexp.Regexp
	// Conditional pattern matches: <IF:TAG>content</IF>
	conditionalPattern *regexp.Regexp
}

// NewEngine creates a new template engine
func NewEngine() *Engine {
	return &Engine{
		// Matches: <ID>, <TITLE:50>, <RELEASEDATE:YYYY-MM-DD>, etc.
		tagPattern: regexp.MustCompile(`<([A-Z_]+)(?::([^>]+))?>`),
		// Matches: <IF:TAG>content</IF> or <IF:TAG>true<ELSE>false</IF>
		conditionalPattern: regexp.MustCompile(`<IF:([A-Z_]+)>(.*?)(?:<ELSE>(.*?))?</IF>`),
	}
}

// Execute processes a template string with the given context
func (e *Engine) Execute(template string, ctx *Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context cannot be nil")
	}

	result := template

	// Step 1: Process conditional blocks first
	result = e.processConditionals(result, ctx)

	// Step 2: Process regular tags
	matches := e.tagPattern.FindAllStringSubmatch(result, -1)

	for _, match := range matches {
		fullTag := match[0] // e.g., "<TITLE:50>"
		tagName := match[1] // e.g., "TITLE"
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
	}

	// Note: sanitization is done by caller if needed
	// We don't sanitize here because templates might be used for folder paths
	// which need to preserve slashes

	return result, nil
}

// processConditionals processes conditional blocks in the template
func (e *Engine) processConditionals(template string, ctx *Context) string {
	result := template

	// Find all conditional blocks
	matches := e.conditionalPattern.FindAllStringSubmatch(result, -1)

	for _, match := range matches {
		fullBlock := match[0]   // e.g., "<IF:SERIES>Series: <SERIES></IF>"
		tagName := match[1]     // e.g., "SERIES"
		trueContent := match[2] // e.g., "Series: <SERIES>"
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
	}

	return result
}

// resolveTag resolves a tag to its value
func (e *Engine) resolveTag(tagName, modifier string, ctx *Context) (string, error) {
	switch tagName {
	case "ID":
		return ctx.ID, nil

	case "CONTENTID":
		return ctx.ContentID, nil

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
	if maxBytes <= 0 || len(title) <= maxBytes {
		return title
	}

	runes := []rune(title)
	currentBytes := 0

	// Always use rune-safe iteration to avoid splitting UTF-8 sequences
	// even for very small byte limits
	for i, r := range runes {
		runeSize := len(string(r)) // Byte size of this rune

		if currentBytes+runeSize > maxBytes {
			// Can't fit this rune
			if i == 0 {
				// Can't fit even one rune, return empty
				return ""
			}

			// Truncate here (no ellipsis if we have < 3 bytes budget)
			truncated := string(runes[:i])

			// Only add ellipsis if we have room (at least 3 bytes left after truncated)
			if maxBytes >= len(truncated)+3 {
				// For English/Latin text, try to break at word boundary
				if !e.containsCJK(title) {
					lastSpace := strings.LastIndex(truncated, " ")
					if lastSpace > 0 && maxBytes >= len(truncated[:lastSpace])+3 {
						return truncated[:lastSpace] + "..."
					}
				}
				return truncated + "..."
			}

			// Not enough room for ellipsis, just return truncated
			return truncated
		}
		currentBytes += runeSize
	}

	// Entire title fits
	return title
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
