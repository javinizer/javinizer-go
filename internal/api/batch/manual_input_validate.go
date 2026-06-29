package batch

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

const (
	// maxManualInputLen is the per-value character cap for a manual input.
	// Inputs exceeding this are rejected (400) to bound log/metadata bloat and
	// parser cost (security F7 / Phase 2 #4).
	maxManualInputLen = 4096
)

// sanitizeManualInput strips Unicode format (Cf) and control (Cc) characters
// from a manual input. Cf covers zero-width/bidi/BOM codepoints; Cc covers
// NUL/TAB/LF/CR and the rest of the C0 control block. Stripping by unicode
// category — not a hardcoded codepoint list — survives future additions to
// those categories (security F7). Visible runes (including ASCII space, which
// is Zs not Cc) are preserved; edge whitespace is trimmed later by
// buildScrapeCmd's TrimSpace.
func sanitizeManualInput(input string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r) {
			return -1
		}
		return r
	}, input)
}

// validateManualURL enforces the URL-shaped checks for a single sanitized manual
// input. A non-http(s) scheme is rejected (Phase 2 #1). Plain IDs (no scheme)
// pass through — they are never fetched as URLs (security F4). The registry is
// consumed by the CanHandleURL check added in Phase 2 #2.
func validateManualURL(input string, registry matcher.URLScraperLister) error {
	u, err := url.Parse(input)
	if err != nil {
		// url.Parse errors embed the raw input verbatim (e.g.
		// `parse "https://…?token=secret": …`), which would leak a signed-URL
		// token into the 400 response. The specific parse detail isn't
		// actionable for the client, so return a fixed message.
		return fmt.Errorf("malformed manual input")
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", u.Scheme)
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		parsed, perr := matcher.ParseInput(input, registry)
		if perr != nil {
			return perr
		}
		if !parsed.IsURL {
			return fmt.Errorf("no enabled scraper can handle URL %q", scrape.RedactURLQuery(input))
		}
	}
	return nil
}

// validateAndSanitizeManualInputs sanitizes (strips Cf/Cc), validates, and returns
// the manual-input map for a CREATE batch scrape. It is the 400 layer on top of
// the existing URL classifier (matcher.ParseInput). Returns a 400-eligible
// error on: more inputs than files (Phase 2 #4), a per-value length over
// maxManualInputLen (Phase 2 #4), a non-http(s) URL scheme (Phase 2 #1), or an
// http(s) URL no enabled scraper CanHandleURLs (Phase 2 #2). The returned map
// holds the sanitized values so a hidden codepoint that passed validation
// cannot reach the scrape.
func validateAndSanitizeManualInputs(
	rawInputs map[string]string,
	files []string,
	registry matcher.URLScraperLister,
) (map[string]string, error) {
	if len(rawInputs) > len(files) {
		return nil, fmt.Errorf("manual_inputs count (%d) exceeds files count (%d)", len(rawInputs), len(files))
	}
	if len(rawInputs) == 0 {
		return rawInputs, nil
	}
	fileSet := make(map[string]struct{}, len(files))
	for _, f := range files {
		fileSet[f] = struct{}{}
	}
	result := make(map[string]string, len(rawInputs))
	for path, raw := range rawInputs {
		if _, ok := fileSet[path]; !ok {
			return nil, fmt.Errorf("manual_inputs key %q is not in files", path)
		}
		sanitized := strings.TrimSpace(sanitizeManualInput(raw))
		if utf8.RuneCountInString(sanitized) > maxManualInputLen {
			return nil, fmt.Errorf("manual input for %q exceeds %d characters", path, maxManualInputLen)
		}
		if sanitized == "" {
			continue
		}
		if err := validateManualURL(sanitized, registry); err != nil {
			return nil, err
		}
		result[path] = sanitized
	}
	return result, nil
}
