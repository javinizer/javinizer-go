package matcher

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// Matches pt1, PT2, part1, PART2 with optional separators before/after
	reNumericPart = regexp.MustCompile(`(?i)(?:^|[-_.\s])(?:(pt|part))[-_.\s]?(\d{1,2})(?:$|[-_.\s])`)
	// Matches plain numbers: -1, -2, _3, .1, etc. (common multi-part pattern)
	rePlainNumber = regexp.MustCompile(`^[-_.\s]?(\d{1,2})$`)
	reDiscPart    = regexp.MustCompile(`(?i)(?:^|[-_.\s])(cd|disc|disk)[-_.\s]?(\d{1,2})(?:$|[-_.\s])`)
	// Strict letter-only remainder: optional sep + [a-z] + optional sep
	reLetterOnlyRemainder = regexp.MustCompile(`(?i)^\s*[-_.\s]?([a-z])\s*$`)
	reLetterWithTrailing  = regexp.MustCompile(`(?i)^\s*[-_.\s]?([a-z])[-_.\s]+\[?(?:\d{3,}|\d[a-z0-9]*[a-z][a-z0-9]*)\]?[a-z0-9]*(?:[-_.\s]+\[?[a-z0-9]+\]?)*\s*$`)
	// Trailing number at end of remainder after separator: ...-1, ..._2, ...3, etc.
	// Catches part numbers buried behind noise like "-un-javgg.net-1"
	reTrailingPartNumber = regexp.MustCompile(`[-_.](\d{1,2})$`)
)

// Pattern type constants for multipart detection
const (
	// PatternExplicit indicates explicit multipart patterns (pt1, part2, -1, -2, cd1, disc1, disk1)
	// These are always considered multipart without directory context validation.
	PatternExplicit = "explicit"
	// PatternLetter indicates ambiguous single-letter patterns (A, B, C)
	// These need directory context validation to confirm multipart status.
	PatternLetter = "letter"
	// PatternTrailing indicates a trailing number after a separator at the end of
	// a noisy remainder (e.g., "-un-javgg.net-1"). The trailing position is suggestive
	// of a part number, but not as unambiguous as a clean remainder-only match.
	// Directory context validation is required to confirm multipart status.
	PatternTrailing = "trailing"
	// PatternNone indicates no multipart pattern detected
	PatternNone = ""
)

// DetectPartSuffix parses the portion of filename after the first occurrence of id
// and returns (number, suffix, patternType, trailingPrefix) where:
//   - number: 0 for single-part, 1..N for part index
//   - suffix: normalized string to append to base name (including leading dash)
//   - patternType: "explicit" for unambiguous patterns, "letter" for ambiguous single-letter,
//     "trailing" for trailing-number patterns, "" for no pattern detected
//   - trailingPrefix: for PatternTrailing, the noise portion before the part number
//     (e.g., "-un-javgg.net" for "SGKI-071-un-javgg.net-1"); used by
//     ValidateMultipartInDirectory to ensure trailing files share the same prefix
func DetectPartSuffix(nameWithoutExt, id string) (int, string, string, string) {
	// Find the first occurrence of id case-insensitively to get the remainder
	lowerName := strings.ToLower(nameWithoutExt)
	lowerID := strings.ToLower(id)
	idx := strings.Index(lowerName, lowerID)

	remainder := nameWithoutExt
	if idx >= 0 {
		remainder = nameWithoutExt[idx+len(id):]
	}

	// Trim common separators/spaces around the remainder
	trimmed := strings.TrimSpace(remainder)

	// 1) Numeric parts: pt1 / part1 with optional dash/no-dash - EXPLICIT
	if m := reNumericPart.FindStringSubmatch(trimmed); len(m) == 3 {
		token := strings.ToLower(m[1]) // "pt" or "part"
		numStr := m[2]
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return n, "-" + token + numStr, PatternExplicit, ""
		}
	}

	// 2) Plain numbers: -1, -2, _3, etc. (common multi-part pattern like pred-151-1.mp4) - EXPLICIT
	if m := rePlainNumber.FindStringSubmatch(trimmed); len(m) == 2 {
		numStr := m[1]
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return n, "-" + numStr, PatternExplicit, ""
		}
	}

	// 3) Disc parts: cd/disc/disk + 1-2 digits - EXPLICIT
	if m := reDiscPart.FindStringSubmatch(trimmed); len(m) == 3 {
		token := strings.ToLower(m[1])
		numStr := m[2]
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return n, "-" + token + numStr, PatternExplicit, ""
		}
	}

	// 4) Letter parts: single trailing letter (A/B/C/...) optionally separated by dash/underscore/space
	// Only accept when the remainder is just that letter (plus optional separators) - AMBIGUOUS
	// These need directory context validation to confirm multipart status.
	if m := reLetterOnlyRemainder.FindStringSubmatch(trimmed); len(m) == 2 {
		if n, suf, ok := letterPart(m[1]); ok {
			return n, suf, PatternLetter, ""
		}
	}

	// 5) Letter + digit-first trailing content (e.g. a-4k, b-1080p, a-4k-60). The part
	// identity is the letter; the trailing content is a resolution/quality tag. Checked
	// BEFORE the trailing-number pattern so a numeric quality component (e.g. -60 fps
	// shorthand in "a-4k-60") is not consumed as a trailing part number.
	if idx := reLetterWithTrailing.FindStringSubmatchIndex(trimmed); idx != nil {
		if n, suf, ok := letterPart(trimmed[idx[2]:idx[3]]); ok {
			return n, suf, PatternLetter, trimmed[idx[3]:]
		}
	}

	// 6) Trailing number after separator at end of remainder: -un-javgg.net-1, _site.name-2
	// This handles filenames where site tags or other noise sits between the ID and the part number.
	// The separator (- or _) + digits at the very end suggests a part number, but it's not
	// as unambiguous as a clean remainder-only match (step 2), so directory validation is required.
	// The trailingPrefix (noise before the part number) is captured for validation — only files
	// with the same prefix should cross-validate as multipart parts.
	if m := reTrailingPartNumber.FindStringSubmatch(trimmed); len(m) == 2 {
		numStr := m[1]
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			prefix := trimmed[:len(trimmed)-len(m[0])]
			return n, "-" + numStr, PatternTrailing, prefix
		}
	}

	// No recognizable part
	return 0, "", PatternNone, ""
}

func letterPart(letter string) (int, string, bool) {
	upper := strings.ToUpper(letter)
	n := int(upper[0]-'A') + 1
	if n < 1 || n > 26 {
		return 0, "", false
	}
	return n, "-" + upper, true
}
