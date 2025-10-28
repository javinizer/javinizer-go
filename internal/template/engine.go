package template

import (
	"fmt"
	"regexp"
	"strings"
	"time"
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

	default:
		return "", fmt.Errorf("unknown tag: %s", tagName)
	}
}

// truncate limits a string to maxLen characters
func (e *Engine) truncate(s string, maxLenStr string) string {
	var maxLen int
	_, err := fmt.Sscanf(maxLenStr, "%d", &maxLen)
	if err != nil || maxLen <= 0 {
		return s
	}

	if len(s) <= maxLen {
		return s
	}

	// Truncate and add ellipsis if string is long enough
	if maxLen > 3 {
		return s[:maxLen-3] + "..."
	}
	return s[:maxLen]
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
