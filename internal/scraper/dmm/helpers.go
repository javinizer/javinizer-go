package dmm

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// contentIDCandidate represents a candidate content ID found during search
type contentIDCandidate struct {
	contentID string
	url       string
	length    int
}

// urlCandidate represents a URL with its priority
type urlCandidate struct {
	url       string
	priority  int
	contentID string // extracted content ID from URL for length comparison
	idLength  int    // length of content ID (shorter is better)
}

func buildResolveContentIDSearchQueries(id string, normalizedContentID string) []string {
	id = strings.ToLower(strings.TrimSpace(id))
	searchID := strings.ReplaceAll(id, "-", "")
	contentID := strings.ToLower(strings.TrimSpace(normalizedContentID))
	cleanContentID := normalizedContentIDWithoutPadding(contentID)

	return uniqueNonEmptyStrings([]string{
		searchID,
		contentID,
		cleanContentID,
		id,
	})
}

func normalizedContentIDWithoutPadding(contentID string) string {
	contentID = strings.ToLower(strings.TrimSpace(contentID))
	if contentID == "" {
		return ""
	}
	return contentIDUnpadRegex.ReplaceAllString(contentID, "$1$2")
}

func stripRentalSuffix(cid string) string {
	if len(cid) >= 2 && strings.HasSuffix(strings.ToLower(cid), "r") {
		beforeR := cid[len(cid)-2]
		if beforeR >= '0' && beforeR <= '9' {
			return cid[:len(cid)-1]
		}
	}
	return cid
}

func uniqueNonEmptyStrings(values []string) []string {
	uniqueValues := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		uniqueValues = append(uniqueValues, value)
	}

	return uniqueValues
}

func extractContentIDCandidates(doc *goquery.Document, searchIDs []string) []contentIDCandidate {
	candidates := make([]contentIDCandidate, 0)
	if doc == nil || len(searchIDs) == 0 {
		return candidates
	}

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		var urlCID string

		// Check for various DMM link patterns:
		// 1. Physical DVD: /mono/dvd/-/detail/=/cid=XXX
		// 2. Digital video: /digital/videoa/-/detail/=/cid=XXX
		// 3. Monthly subscription: /monthly/standard/-/detail/=/cid=XXX
		// 4. Video streaming: video.dmm.co.jp/av/content/?id=XXX
		if strings.Contains(href, "cid=") {
			matches := dmmCIDRegex.FindStringSubmatch(href)
			if len(matches) > 1 {
				urlCID = matches[1]
			}
		} else if strings.Contains(href, "video.dmm.co.jp") && strings.Contains(href, "id=") {
			matches := dmmIDRegex.FindStringSubmatch(href)
			if len(matches) > 1 {
				urlCID = matches[1]
			}
		}

		if urlCID == "" {
			return
		}

		// Always strip rental 'r' suffix from content IDs regardless of URL path.
		// DMM uses the 'r' suffix for rental content IDs across all URL types
		// (digital, physical, streaming), not just /rental/ pages.
		urlCID = stripRentalSuffix(urlCID)

		// Clean the CID from URL using precompiled regex for consistency and performance
		// Strips DMM prefixes: "9ipx535" -> "ipx535", "h_796san167" -> "san167"
		// Normalize to lowercase and remove hyphens before regex (cleanPrefixRegex expects lowercase)
		normalizedHrefCID := strings.ToLower(strings.ReplaceAll(urlCID, "-", ""))
		cleanURLCID := cleanPrefixRegex.ReplaceAllString(normalizedHrefCID, "$1")
		if !matchesWithVariantSuffix(cleanURLCID, searchIDs...) {
			return
		}

		// Build full URL if it's a relative path
		fullURL := ""
		if strings.HasPrefix(href, "/") {
			fullURL = "https://www.dmm.co.jp" + href
		} else if strings.HasPrefix(href, "http") {
			fullURL = href
		}

		// Store canonical (cleaned) content ID for consistency downstream
		candidates = append(candidates, contentIDCandidate{
			contentID: cleanURLCID,
			url:       fullURL,
			length:    len(cleanURLCID),
		})
		logging.Debugf("DMM: ✓ Found candidate %s (canonical: %s, length: %d), URL: %s", urlCID, cleanURLCID, len(cleanURLCID), fullURL)
	})

	return candidates
}

// extractCandidateURLs extracts and prioritizes URLs from search results
func (s *Scraper) extractCandidateURLs(doc *goquery.Document, contentID string) []urlCandidate {
	var candidates []urlCandidate

	// URL patterns to exclude (unsupported page structures)
	excludePatterns := []string{
		"/rental/-/search/",    // Rental search pages (not product pages)
		"/search/",             // Search results pages
		"/list/",               // List/search pages
		"/-/search/",           // Mono search pages
		"/-/list/",             // Monthly list pages
		"/service/-/exchange/", // Exchange/redirect pages
	}

	// Only exclude video.dmm.co.jp if browser mode is disabled
	if !s.useBrowser {
		excludePatterns = append(excludePatterns, "video.dmm.co.jp") // New streaming platform uses JavaScript rendering
		logging.Debug("DMM: Excluding video.dmm.co.jp URLs (browser mode disabled)")
	} else {
		logging.Debug("DMM: Including video.dmm.co.jp URLs (browser mode enabled)")
	}

	// Priority order (higher = better):
	// 6. /mono/dvd/ (physical DVD pages - full metadata including actress data)
	// 5. /digital/videoa/ or /digital/videoc/ (digital video DVD - full metadata)
	// 4. video.dmm.co.jp/amateur/ (amateur pages)
	// 3. video.dmm.co.jp/av/ (digital streaming video pages)
	// 2. /monthly/premium/ (monthly premium - LIMITED metadata, no actress data)
	// 1. /monthly/standard/ (monthly standard - LIMITED metadata, no actress data)
	// 0. /rental/ (rental pages - LIMITED metadata, last resort when nothing else available)

	// Extract canonical base ID by stripping DMM prefixes (leading digits OR h_<digits>)
	// Examples: "4sone860" -> "sone860", "61mdb087" -> "mdb087", "h_1472smkcx003" -> "smkcx003"
	// Keep lowercase for URL matching consistency
	contentIDLower := strings.ToLower(contentID)
	baseID := cleanPrefixRegex.ReplaceAllString(contentIDLower, "$1")
	if baseID == "" {
		baseID = contentIDLower // No prefix, use as-is
	}

	logging.Debugf("DMM: extractCandidateURLs looking for contentID=%s, baseID=%s", contentIDLower, baseID)
	logging.Debugf("DMM: Browser mode enabled=%v, excludePatterns=%v", s.useBrowser, excludePatterns)

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		// Check if this link contains our canonical content-ID or base ID
		// DMM product pages can use different ID formats (e.g., sone860, 4sone860, tksone860)
		// Use lowercase canonical forms for consistent matching
		hrefLower := strings.ToLower(href)
		logging.Debugf("DMM: Checking link href=%s, contains contentID=%v, contains baseID=%v",
			hrefLower, strings.Contains(hrefLower, contentIDLower), strings.Contains(hrefLower, baseID))
		containsID := strings.Contains(hrefLower, contentIDLower) || strings.Contains(hrefLower, baseID)
		if !containsID {
			return
		}

		// Build full URL
		var fullURL string
		if strings.HasPrefix(href, "/") {
			fullURL = "https://www.dmm.co.jp" + href
		} else if strings.HasPrefix(href, "http") {
			fullURL = href
		} else {
			return
		}

		// Skip excluded patterns
		excluded := false
		for _, pattern := range excludePatterns {
			if strings.Contains(fullURL, pattern) {
				logging.Debugf("DMM: URL excluded by pattern '%s': %s", pattern, fullURL)
				excluded = true
				break
			}
		}
		if excluded {
			return
		}

		priority := urlPriority(fullURL)

		// Extract content ID from URL for comparison
		extractedID := extractContentIDFromURL(fullURL)
		if strings.Contains(fullURL, "/rental/") {
			extractedID = stripRentalSuffix(extractedID)
		}
		idLen := len(extractedID)

		candidates = append(candidates, urlCandidate{
			url:       fullURL,
			priority:  priority,
			contentID: extractedID,
			idLength:  idLen,
		})
		logging.Debugf("DMM: Found candidate URL (priority %d, ID: %s, len: %d): %s", priority, extractedID, idLen, fullURL)
	})

	logging.Debugf("DMM: Found %d candidate URLs total", len(candidates))
	return candidates
}

// normalizeContentID converts movie ID to DMM content ID format
// Example: "ABP-420" -> "abp00420"
// Amateur IDs like "oreco183", "cap123" are returned as-is (no padding)
//
// Strategy: Use heuristics to detect amateur vs standard IDs, avoiding hardcoded prefix lists.
// Conservative heuristic: Only skip padding if ID has BOTH:
// 1. No hyphen in original (amateur IDs don't use hyphens)
// 2. 4-6 letter prefix (standard studios are usually 2-3 letters like IPX, ABP)
// 3. 3-4 digit number
//
// This ensures standard studio IDs like "ABP420" (3 letters) still get padding -> "abp00420"
// while amateur IDs like "oreco183" (5 letters) don't -> "oreco183"
// The cache will correct any edge case misidentifications after the first successful search.
func normalizeContentID(id string) string {
	// Convert to lowercase
	idLower := strings.ToLower(id)

	// Check if original ID had a hyphen (standard JAV format)
	hadHyphen := strings.Contains(idLower, "-")

	// Remove hyphens for processing
	idNoHyphen := strings.ReplaceAll(idLower, "-", "")

	// Strip DMM-specific prefixes (leading digits or h_<digits> pattern)
	// Examples: h_1472smkcx003 -> smkcx003, 9ipx535 -> ipx535, h_796san167 -> san167
	// Uses precompiled cleanPrefixRegex for performance
	if cleaned := cleanPrefixRegex.ReplaceAllString(idNoHyphen, "$1"); cleaned != "" {
		idNoHyphen = cleaned
	}

	// Extract components: letters, numbers, optional suffix
	matches := normalizeContentIDRegex.FindStringSubmatch(idNoHyphen)

	if len(matches) > 2 {
		prefix := matches[1]
		number := matches[2]
		suffix := ""
		if len(matches) > 3 {
			suffix = matches[3]
		}

		// Conservative heuristic for amateur detection:
		// - No hyphen in original ID (amateur IDs rarely use hyphens)
		// - 4-6 letter prefix (standard studios are 2-3 letters: IPX, ABP, SSIS)
		// - 3-4 digit number
		// Examples that match: oreco183 (5+3), luxu456 (4+3), maan789 (4+3)
		// Examples that DON'T match: abp420 (3+3), cap123 (3+3) -> these get padding
		if !hadHyphen && len(prefix) >= 4 && len(prefix) <= 6 && len(number) >= 3 && len(number) <= 4 {
			// Likely amateur - return as-is without padding
			return prefix + number + suffix
		}

		// Standard JAV ID or ambiguous - apply zero-padding to width 5 (string-based, safe for all lengths)
		// Cache will correct if this was actually an amateur ID
		if len(number) < 5 {
			number = strings.Repeat("0", 5-len(number)) + number
		}
		return prefix + number + suffix
	}

	return idNoHyphen
}

// normalizeID converts content ID back to standard DVD-ID format with hyphen
//
// Examples:
//
//	"ipx00535"   -> "IPX-535"
//	"sone860"    -> "SONE-860"
//	"oreco183"   -> "ORECO-183"
//	"4sone860"   -> "SONE-860"   (leading digits stripped - DMM catalog prefix)
//	"61mdb087"   -> "MDB-087"    (leading digits stripped - DMM channel prefix)
//	"t28123"     -> "T-28123"    (5-digit number preserved)
//	"h_1472smkcx003" -> "SMKCX-003" (h_<digits> prefix stripped)
//
// Strategy:
//  1. Strip h_<digits> prefix if present (DMM content-ID format)
//  2. Split by word-digit boundary (letters vs numbers)
//  3. Strip leading numeric prefixes (DMM uses catalog/channel codes)
//  4. Remove leading zeros from number (e.g., "00535" -> "535")
//  5. Ensure at least 3 digits remain (pad with zeros if needed)
//  6. Always add hyphen between prefix and number for consistency
func normalizeID(contentID string) string {
	idLower := strings.ToLower(contentID)

	// Strip DMM-specific prefixes (leading digits or h_<digits> pattern)
	// Examples: h_1472smkcx003 -> smkcx003, 4sone860 -> sone860, 61mdb087 -> mdb087
	// Uses precompiled cleanPrefixRegex for performance
	if cleaned := cleanPrefixRegex.ReplaceAllString(idLower, "$1"); cleaned != "" {
		idLower = cleaned
	}

	// Match pattern: letter prefix, number, optional suffix
	// Examples: "sone860", "ipx00535", "oreco183"
	matches := normalizeIDRegex.FindStringSubmatch(idLower)

	if len(matches) > 2 {
		prefix := strings.ToUpper(matches[1])
		number := matches[2]
		suffix := ""
		if len(matches) > 3 {
			suffix = strings.ToUpper(matches[3])
		}

		// Remove leading zeros from number, but keep at least 3 digits (string-based, no overflow)
		// Examples: "00535" -> "535", "860" -> "860", "01" -> "001", "00000" -> "000"
		trimmed := strings.TrimLeft(number, "0")
		if trimmed == "" {
			trimmed = "0" // All zeros case
		}
		// Pad to minimum 3 digits
		if len(trimmed) < 3 {
			number = strings.Repeat("0", 3-len(trimmed)) + trimmed
		} else {
			number = trimmed
		}

		// Always add hyphen between prefix and number for consistency
		// This works for all JAV IDs: standard, amateur, and studio series
		return prefix + "-" + number + suffix
	}

	return strings.ToUpper(contentID)
}

// extractContentIDFromURL extracts content ID from DMM URL
// Supports both www.dmm.co.jp (cid=) and video.dmm.co.jp (id=) formats
func extractContentIDFromURL(url string) string {
	matches := dmmCIDRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}

	matches = dmmIDRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// matchesWithVariantSuffix checks if urlCID matches any of the search IDs,
// allowing for single-letter variant suffixes (a, b, c, d, etc.) that DMM uses
// to indicate different versions of the same video.
// Examples: akdl229a matches akdl229, ipx535b matches ipx535
func matchesWithVariantSuffix(urlCID string, searchIDs ...string) bool {
	for _, searchID := range searchIDs {
		// Exact match
		if urlCID == searchID {
			return true
		}

		// Check if urlCID is searchID + single letter suffix
		// Only match single lowercase letters a-z as variant suffixes
		if len(urlCID) == len(searchID)+1 && strings.HasPrefix(urlCID, searchID) {
			suffix := urlCID[len(searchID):]
			if len(suffix) == 1 && suffix[0] >= 'a' && suffix[0] <= 'z' {
				return true
			}
		}
	}
	return false
}

// hiraganaToRomaji converts hiragana to romaji using Nihon-shiki romanization
// DMM uses Nihon-shiki (si, ti, tu) not Hepburn (shi, chi, tsu)
// Example: しらかみえみか -> sirakamiemika
func hiraganaToRomaji(hiragana string) string {
	// Hiragana to romaji mapping (Nihon-shiki)
	mapping := map[string]string{
		"あ": "a", "い": "i", "う": "u", "え": "e", "お": "o",
		"か": "ka", "き": "ki", "く": "ku", "け": "ke", "こ": "ko",
		"が": "ga", "ぎ": "gi", "ぐ": "gu", "げ": "ge", "ご": "go",
		"さ": "sa", "し": "si", "す": "su", "せ": "se", "そ": "so",
		"ざ": "za", "じ": "zi", "ず": "zu", "ぜ": "ze", "ぞ": "zo",
		"た": "ta", "ち": "ti", "つ": "tu", "て": "te", "と": "to",
		"だ": "da", "ぢ": "di", "づ": "du", "で": "de", "ど": "do",
		"な": "na", "に": "ni", "ぬ": "nu", "ね": "ne", "の": "no",
		"は": "ha", "ひ": "hi", "ふ": "hu", "へ": "he", "ほ": "ho",
		"ば": "ba", "び": "bi", "ぶ": "bu", "べ": "be", "ぼ": "bo",
		"ぱ": "pa", "ぴ": "pi", "ぷ": "pu", "ぺ": "pe", "ぽ": "po",
		"ま": "ma", "み": "mi", "む": "mu", "め": "me", "も": "mo",
		"や": "ya", "ゆ": "yu", "よ": "yo",
		"ら": "ra", "り": "ri", "る": "ru", "れ": "re", "ろ": "ro",
		"わ": "wa", "を": "wo", "ん": "n",
		// Small kana
		"ゃ": "ya", "ゅ": "yu", "ょ": "yo",
		"ぁ": "a", "ぃ": "i", "ぅ": "u", "ぇ": "e", "ぉ": "o",
		"っ": "", // Small tsu (gemination marker, handled separately)
	}

	result := ""
	runes := []rune(hiragana)

	for i := 0; i < len(runes); i++ {
		char := string(runes[i])

		// Check for combined characters (きゃ, しゃ, etc.)
		if i+1 < len(runes) {
			next := string(runes[i+1])
			if next == "ゃ" || next == "ゅ" || next == "ょ" {
				// Consonant + small ya/yu/yo
				if romaji, ok := mapping[char]; ok {
					// Remove the vowel and add the y-sound
					if len(romaji) > 0 {
						consonant := romaji[:len(romaji)-1] // Remove last char (vowel)
						result += consonant + mapping[next]
						i++ // Skip next character
						continue
					}
				}
			}

			// Check for small tsu (gemination - doubles next consonant)
			if char == "っ" && i+1 < len(runes) {
				nextChar := string(runes[i+1])
				if romaji, ok := mapping[nextChar]; ok && len(romaji) > 0 {
					// Double the first consonant
					result += string(romaji[0])
					continue
				}
			}
		}

		// Regular character mapping
		if romaji, ok := mapping[char]; ok {
			result += romaji
		} else {
			// Unknown character, keep as-is
			result += char
		}
	}

	return result
}
