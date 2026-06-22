package nfo

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// mergeActresses merges actress slices
func mergeActresses(fieldName string, scraped, nfo []models.Actress, strategy MergeStrategy, fm *fieldMerger) []models.Actress {
	scrapedEmpty := len(scraped) == 0
	nfoEmpty := len(nfo) == 0

	if scrapedEmpty && nfoEmpty {
		fm.recordEmpty(fieldName)
		return nil
	}

	if scrapedEmpty {
		fm.recordNFO(fieldName)
		return nfo
	}
	if nfoEmpty {
		fm.recordScraper(fieldName)
		return scraped
	}

	// Both have data — use smart merge that preserves DMMIDs from scraped data.
	// NFO-parsed actresses never have DMMIDs, so losing scraped DMMIDs breaks
	// actress database lookups and associations.
	switch strategy {
	case PreferNFO, PreserveExisting, FillMissingOnly:
		// Merge actresses by matching names, preferring NFO name data but
		// always preserving DMMID from scraped source.
		merged := mergeActressSlices(scraped, nfo, true)
		fm.recordNFO(fieldName)
		fm.recordConflict()
		return merged
	case MergeArrays:
		// Merge and deduplicate with cross-source matching.
		// Unlike the naive key-based approach, this matches actresses across
		// sources using all available identifiers (JapaneseName, DMMID, romanized name)
		// to avoid creating duplicate entries for the same person.
		merged := mergeActressSlices(scraped, nfo, false)
		fm.recordMerged(fieldName, 0.9)
		return merged
	default: // PreferScraper
		fm.recordScraper(fieldName)
		fm.recordConflict()
		return scraped
	}
}

// actressMatch represents a matched or unmatched actress from the deduplication phase.
type actressMatch struct {
	actress     models.Actress
	fromScraper bool
	partner     *models.Actress // non-nil if matched with an actress from the other source
}

// actressLookupIndices holds pre-built lookup maps for cross-source matching.
type actressLookupIndices struct {
	byJpName        map[string][]int
	byRomanizedName map[string][]int
}

// actressEntry is the internal representation used by deduplicateActresses.
type actressEntry struct {
	actress     models.Actress
	fromScraper bool
	matched     bool
	partnerIdx  int // index of matched partner, -1 if unmatched
}

// matchPair represents a matched pair of entry indices.
type matchPair struct {
	aIdx int // index of the entry being matched
	bIdx int // index of the partner
}

// buildActressLookupIndices creates lookup maps from entries filtered by the
// provided fromScraper flag.
func buildActressLookupIndices(entries []actressEntry, fromScraper bool) actressLookupIndices {
	idx := actressLookupIndices{
		byJpName:        make(map[string][]int),
		byRomanizedName: make(map[string][]int),
	}
	for i, e := range entries {
		if e.fromScraper != fromScraper {
			continue
		}
		a := e.actress
		if jp := strings.ToLower(strings.TrimSpace(a.JapaneseName)); jp != "" {
			idx.byJpName["jp:"+jp] = append(idx.byJpName["jp:"+jp], i)
		}
		fn := strings.ToLower(strings.TrimSpace(a.FirstName))
		ln := strings.ToLower(strings.TrimSpace(a.LastName))
		if fn != "" || ln != "" {
			idx.byRomanizedName["name:"+fn+"|"+ln] = append(idx.byRomanizedName["name:"+fn+"|"+ln], i)
		}
	}
	return idx
}

// matchEntriesByDirection tries to match entries from one source against lookup
// indices of the other source, applying matches eagerly so that each entry
// can only be matched once. fromScraper indicates which source we're matching
// FROM (true = matching scraped entries against NFO indices, false = matching NFO
// entries against scraped indices).
func matchEntriesByDirection(entries []actressEntry, lookup actressLookupIndices, fromScraper bool) []matchPair {
	var pairs []matchPair

	matchFn := func(actress models.Actress) int {
		if jp := strings.ToLower(strings.TrimSpace(actress.JapaneseName)); jp != "" {
			key := "jp:" + jp
			for _, idx := range lookup.byJpName[key] {
				if !entries[idx].matched {
					return idx
				}
			}
		}
		fn := strings.ToLower(strings.TrimSpace(actress.FirstName))
		ln := strings.ToLower(strings.TrimSpace(actress.LastName))
		if fn != "" || ln != "" {
			for _, key := range []string{"name:" + fn + "|" + ln, "name:" + ln + "|" + fn} {
				for _, idx := range lookup.byRomanizedName[key] {
					if !entries[idx].matched {
						return idx
					}
				}
			}
		}
		return -1
	}

	for i := range entries {
		if entries[i].fromScraper != fromScraper || entries[i].matched {
			continue
		}
		matchedIdx := matchFn(entries[i].actress)
		if matchedIdx < 0 {
			continue
		}
		// Eagerly mark matched so subsequent iterations see updated state
		entries[matchedIdx].matched = true
		entries[i].matched = true
		pairs = append(pairs, matchPair{aIdx: i, bIdx: matchedIdx})
	}
	return pairs
}

// deduplicateActresses builds entries from both slices, performs cross-source matching
// by JapaneseName/romanized name/DMMID, and returns matches for strategy resolution.
func deduplicateActresses(scraped, nfo []models.Actress) []actressMatch {
	entries := make([]actressEntry, 0, len(scraped)+len(nfo))

	for _, a := range scraped {
		entries = append(entries, actressEntry{actress: a, fromScraper: true, partnerIdx: -1})
	}
	for _, a := range nfo {
		entries = append(entries, actressEntry{actress: a, fromScraper: false, partnerIdx: -1})
	}

	// Build lookup indices for both sources
	scrapedIndices := buildActressLookupIndices(entries, true)
	nfoIndices := buildActressLookupIndices(entries, false)

	// Phase 1: Match NFO actresses to scraped actresses using NFO's identifiers
	// (matchEntriesByDirection eagerly marks matched entries)
	phase1 := matchEntriesByDirection(entries, scrapedIndices, false)
	for _, pair := range phase1 {
		entries[pair.bIdx].partnerIdx = pair.aIdx
		entries[pair.aIdx].partnerIdx = pair.bIdx
	}

	// Phase 2: Reverse match — for any unmatched scraped actresses, try to find
	// an NFO match using the scraped actress's identifiers against NFO lookup indices.
	// (matchEntriesByDirection eagerly marks matched entries)
	phase2 := matchEntriesByDirection(entries, nfoIndices, true)
	for _, pair := range phase2 {
		entries[pair.bIdx].partnerIdx = pair.aIdx
		entries[pair.aIdx].partnerIdx = pair.bIdx
	}

	// Convert entries to matches with partner references
	matches := make([]actressMatch, 0, len(entries))
	for _, e := range entries {
		m := actressMatch{
			actress:     e.actress,
			fromScraper: e.fromScraper,
		}
		if e.partnerIdx >= 0 {
			partner := entries[e.partnerIdx].actress
			m.partner = &partner
		}
		matches = append(matches, m)
	}

	return matches
}

// applyActressMergeStrategy resolves field merging for matched actresses according to preferNFO.
// For matched pairs, the scraped actress is the base, with NFO fields applied per strategy.
func applyActressMergeStrategy(matches []actressMatch, preferNFO bool) []models.Actress {
	result := make([]models.Actress, 0, len(matches))

	for _, m := range matches {
		if m.partner == nil {
			// Unmatched — use as-is
			result = append(result, m.actress)
			continue
		}

		if m.fromScraper {
			// Merge: scraped base + NFO partner
			merged := m.actress
			nfoActress := *m.partner
			if preferNFO {
				if nfoActress.JapaneseName != "" {
					merged.JapaneseName = nfoActress.JapaneseName
				}
				if nfoActress.FirstName != "" {
					merged.FirstName = nfoActress.FirstName
				}
				if nfoActress.LastName != "" {
					merged.LastName = nfoActress.LastName
				}
			} else {
				if merged.JapaneseName == "" && nfoActress.JapaneseName != "" {
					merged.JapaneseName = nfoActress.JapaneseName
				}
				if merged.FirstName == "" && nfoActress.FirstName != "" {
					merged.FirstName = nfoActress.FirstName
				}
				if merged.LastName == "" && nfoActress.LastName != "" {
					merged.LastName = nfoActress.LastName
				}
			}
			if merged.ThumbURL == "" && nfoActress.ThumbURL != "" {
				merged.ThumbURL = nfoActress.ThumbURL
			}
			result = append(result, merged)
			// Skip the NFO partner entry
		} else {
			// NFO entry with scraped partner — the scraped entry handles the merge.
			// Skip this NFO entry; the paired scraped entry already produced the merged result.
			continue
		}
	}

	return result
}

func mergeActressSlices(scraped, nfo []models.Actress, preferNFO bool) []models.Actress {
	matches := deduplicateActresses(scraped, nfo)
	return applyActressMergeStrategy(matches, preferNFO)
}
