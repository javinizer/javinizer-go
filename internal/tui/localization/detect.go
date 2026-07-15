package localization

import (
	"strings"

	"golang.org/x/text/language"
)

// DetectOSLocale returns the operating system's preferred BCP 47 locale tags,
// most-preferred first. It never panics; on any detection failure it returns a
// single-element list containing "en" so callers fall back to English safely.
func DetectOSLocale() []string {
	tags := normalizeAndValidate(detectPlatformLocale())
	if len(tags) == 0 {
		return []string{"en"}
	}
	return tags
}

// normalizeAndValidate parses each raw POSIX locale string (e.g. "ja_JP.UTF-8"
// or "de_DE@euro"), strips the codeset and modifier, converts the underscore
// separator to a hyphen, and validates the result as a BCP 47 tag. Invalid or
// neutral ("C"/"POSIX") values are dropped. Order is preserved and duplicates
// removed so the most-preferred surviving tag stays first.
func normalizeAndValidate(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" || isNeutralLocale(r) {
			continue
		}
		tag := normalizeOne(r)
		if tag == "" {
			continue
		}
		if _, err := language.Parse(tag); err != nil {
			continue
		}
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// isNeutralLocale reports whether a raw value is the C/POSIX neutral locale,
// which carries no usable language preference.
func isNeutralLocale(raw string) bool {
	switch raw {
	case "C", "POSIX", "C.UTF-8":
		return true
	}
	return false
}

// normalizeOne strips a POSIX codeset (".UTF-8") and modifier ("@euro") suffix
// and converts the locale separator from underscore to hyphen so the result is
// a candidate BCP 47 tag (e.g. "ja_JP.UTF-8" -> "ja-JP").
func normalizeOne(raw string) string {
	if i := strings.IndexByte(raw, '@'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		raw = raw[:i]
	}
	return strings.ReplaceAll(raw, "_", "-")
}

// parseLanguageEnv splits a GNU "LANGUAGE" value — a colon-separated
// preference list such as "ja:en_US:en" — into individual raw locale tags.
func parseLanguageEnv(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// detectFromEnv reads POSIX locale environment variables in glibc priority
// order (LANGUAGE, then LC_ALL, then LC_MESSAGES, then LANG) via getenv and
// returns the raw preference list. getenv abstracts os.Getenv for testing.
func detectFromEnv(getenv func(string) string) []string {
	var tags []string
	tags = append(tags, parseLanguageEnv(getenv("LANGUAGE"))...)
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := strings.TrimSpace(getenv(name)); val != "" {
			tags = append(tags, val)
		}
	}
	return tags
}
