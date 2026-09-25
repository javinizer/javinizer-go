package matcher

import (
	"regexp"
	"strings"
)

var (
	fusedRemasterRegex     = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)((?:\d{1,3}|\d{6}))([ez]?)(hd|ai|h)(?:(cd|disc|disk|pt|part)(\d{1,2}))?(?:$|[-_.\s[\]()])`)
	separatedRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)[._\s]+(\d{1,6})([ez]?)?[-._\s]?(hd|ai|h)(?:(cd|disc|disk|pt|part)(\d{1,2}))?(?:$|[-_.\s[\]()])`)
	reRemasterRemainder    = regexp.MustCompile(`(?i)^[-_.\s]?(HD|AI|H)(?:(cd|disc|disk|pt|part)\d{1,2})?(?:$|[-_.\s[\]()])`)
	remasterCodecTailRegex = regexp.MustCompile(`(?i)^[-_.\s]?\d{3}(?:\D|$)`)
	contentIDShapeRegex    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[A-Za-z]{0,3}))(?:[-_.\s[\]()](.*)$|(?:cd|disc|disk|pt|part)\d{1,2}$|$)`)
	trailingCatalogIDRegex = regexp.MustCompile(`(?i)(?:[a-z]{1,}-(?:\d{2}|[0-3689]\d\d|4[0-79]\d|48[1-9]|5[0-689]\d|57[0-57-9]|7[0-13-9]\d|72[1-9])\b|[a-z]{1,}-\d{6,}\b|[a-z]{1,}-\d{1,6}[-._\s]?(?:hd|ai|h)\b|t28-\d{1,}\b|[hn]_\d+[a-z]+\d+|\b[a-z]+\d{4,5}[a-z]{0,3}\b|\b\d+[a-z]{2,}\d+[a-z]{0,3}\b|\b(?:t28|[a-z]{1,})[-._\s]\d{1,6}[-._\s]?(?:hd|ai|h)\b|\b[a-z]{2,6}\d{1,6}\b|\b[a-z](?:\d{5}|\d{4}|[013-9]\d\d|2(?:[013-9]\d|4\d|6[0-36-9]))\b)`)
	// Standard color/transfer metadata is matched as a class —
	// (bt|rec|st|smpte) plus 3-4 digits with an optional dot or space — so
	// dotless spellings (BT601, REC601, REC2020) and future standards
	// (BT1886, REC2100, ST2086) are vetoed like the bt709/st2084 literals
	// instead of arriving one review round at a time. The class is bounded:
	// two-digit numbers (REC12), five-plus-digit id-shaped tokens (BT60123),
	// and hyphenated spellings (BT-601) stay catalog-id grammar.
	// Lossless-audio sample rates get the same class treatment — (l)pcm
	// plus 3-4 digits with an optional dot or space — so PCM192, LPCM384,
	// and PCM.768 spellings are vetoed like the bt709/st2084 literals
	// instead of matching the builtin amateur pattern. The bound mirrors
	// the color class because it mirrors the collision: two-digit numbers
	// (PCM12, PCM96) and five-plus-digit id-shaped tokens (PCM00123) stay
	// catalog-id grammar, and hyphenated spellings (PCM-192) do too. Real
	// pcm-series ids are prefix-pinned (5pcm013, n_600pcm00103), so the
	// word-boundary anchor leaves them alone.
	// Codec rate suffixes fold into the same class — dts, flac, and opus
	// plus 3-4 digits with an optional dot or space — because their bare
	// literals stop at the word boundary before the digits: DTS768, FLAC192,
	// and OPUS192 sample-rate/bitrate spellings otherwise satisfy the
	// builtin amateur pattern as replacement catalog ids. The identical
	// bound keeps the same collisions live: two-digit numerals (DTS24) and
	// five-plus-digit id-shaped tokens (DTS00123) stay catalog-id grammar,
	// and hyphenated spellings (DTS-768) keep the bare literals' veto. Real
	// dts-series ids are hyphenated display spellings (DTS-24) or
	// numerically prefixed content ids (189dts00087), so the separator and
	// word-boundary anchors leave them alone.
	// The Dolby Digital family folds into the rate class with both of its
	// spellings in one alternative — e?ac3 plus 3-4 digits with an optional
	// dot or space — because the bare ac3/eac3 literals stop at the word
	// boundary before the digits: AC3640, AC3448, and EAC3640 bitrate
	// spellings otherwise satisfy the builtin amateur pattern and the
	// trailing catalog grammar as replacement ids behind a separated
	// remaster id. The bound stays at the family's 3-4 digits — bitrates
	// are three digits (640, 448) and eac3's higher rates run to four —
	// and since the codec name itself ends in a digit, the id-shaped
	// tokens it covers are the ac series' fused spellings with four or
	// five digits starting with 3. No ac3 or eac3 series exists in the
	// r18.dev content-id prefix lookup, and the real ac series keeps its
	// spellings: zero-padded content ids start with 0 (ac00364 — the
	// digit after ac cannot be the class's 3), numerically prefixed
	// content ids (306ac00123) never offer the leading word boundary, and
	// hyphenated display ids (AC-3640) keep the separator's protection.
	// Shorter remainders (AC307, AC364) leave only two digits after the
	// ac3 head and stay catalog-id grammar, so the residual fused
	// spelling (AC3640) rides the same tradeoff as DTS768.
	// The VVC codec-name spelling — H.266's name, as AVC is H.264's and
	// HEVC is H.265's — joins the avc/hevc free-digit codec-name aliases:
	// VVC266, VVC1080, and other numbered spellings otherwise satisfy the
	// trailing catalog and builtin amateur grammars as replacement ids
	// (the numeric h.26x spellings H266/x266 are already covered by the
	// [hx]26 class). No vvc series exists in the r18.dev content-id
	// prefix lookup (only lvvc, whose leading letter blocks the word
	// boundary), so unlike the dts/flac/opus rate class no digit bound is
	// needed: numerically prefixed content ids (189vvc00087) and lvvc
	// series spellings (n_600lvvc123) keep matching as ids.
	// Compact source tags join the vocabulary as a class — web, remux,
	// and bluray plus a 3-4 digit resolution number — because the compact
	// spellings WEB2160, REMUX1080, and BLURAY2160 otherwise satisfy the
	// builtin amateur pattern and the trailing catalog grammar as
	// replacement catalog ids behind a separated remaster id. The bound
	// mirrors the color/audio/codec classes because it mirrors the
	// collision: resolution numbers are 3-4 digits (720, 1080, 2160), so
	// two-digit numerals (WEB24) and five-plus-digit id-shaped tokens
	// (WEB12345) stay catalog-id grammar. No web, remux, or bluray series
	// exists in the r18.dev content-id prefix lookup (only fweb and ziweb,
	// whose leading letters block the word boundary, exactly as lvvc does
	// for the vvc codec-name alias), so the word-boundary anchor leaves
	// numerically prefixed content ids (189web00087) alone and hyphenated
	// display spellings (WEB-24) keep the separator's protection. The
	// remaining probed source spellings stay id grammar on purpose: bd,
	// dvd, blu, and ray are real series in the lookup table, so BD1080
	// and DVD1080 tokens and the BLU-RAY1080 fragment (RAY1080) keep
	// matching as ids, and the hyphenated WEB-2160 rides the trailing
	// resolution grammar — a separate window this class does not touch.
	// Compound source spellings extend the source class — web plus an
	// optional single separator and the dl/rip qualifier, directly
	// followed by the 3-4 digit resolution number — because the standard
	// WEB-DL2160 spelling and its WEBRIP2160, WEB.RIP2160, and WEB
	// DL2160 siblings otherwise satisfy the builtin amateur pattern and
	// the trailing catalog grammar as replacement ids behind a separated
	// remaster id: the catalog scan splits the compound at its separator,
	// so the DL2160 fragment arrives alone while the WEB- prefix belongs
	// to it (see compoundSourceTagPrefixRegex). The compound carries the
	// class's bounds — the digit bound keeps five-digit id-shaped tokens
	// (WEB-DL12345) and two-digit numerals (WEB-DL24) out, and the
	// qualifier must directly precede the digits, so the spaced
	// resolution (WEB-DL 2160), the double-hyphen WEB-DL-2160 (which
	// rides the trailing resolution grammar like WEB-2160), and real
	// dl-series display ids (WEB-DL-24) stay id grammar. No webdl,
	// webrip, or rip series exists in the r18.dev content-id prefix
	// lookup, and the real dl series (h_952, n_600) keeps its bare
	// DL2160 spellings: the veto span extends only over a web prefix
	// with a leading word boundary, so fweb (FWEB-DL2160) and
	// numerically prefixed (189WEB-DL1) spellings keep the leading-letter
	// protection, and a DL fragment without the web prefix stays id
	// grammar.
	trailingQualityTagRegex = regexp.MustCompile(`(?i)\b(?:[hx]26[3-9]|avc\d*|aac\d*|hevc\d*|vvc\d*|ac3|dts|flac|opus|truehd|vc1|av1|mp[34]|ddp\d*|eac3|divx\d*|xvid\d*|prores\d*|yuv\d*|rgb\d*|p0(?:10|16)|mpeg\d*|vp\d+|fhd\d{2,4}|uhd\d{2,4}|hdtv|hdr\d*|bt2020|bt709|rec709|smpte\d+|pq\d+|st2084|hlg\d*|ycbcr\d*|(?:bt|rec|st|smpte)[. ]?\d{3,4}|(?:l?pcm|dts|flac|opus|e?ac3)[. ]?\d{3,4}|\d+(?:bit|point)\d+|(?:web(?:[-_. ]?(?:dl(?:rip)?|rip))?|remux|bluray)\d{3,4})\b`)
	// compoundSourceTagPrefixRegex recognizes the source-tag prefix — web
	// plus exactly one separator — ending where a trailing-catalog
	// candidate begins, so the candidate's quality veto can span the
	// compound spelling (WEB-DL2160) that the catalog scan split apart.
	// The leading boundary requirement (start of text or a
	// non-alphanumeric character before web) keeps the word-boundary
	// protections of the source class: fweb and 189web spellings never
	// extend the span.
	compoundSourceTagPrefixRegex      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(web)[-_. ]$`)
	trailingResolutionCatalogIDRegex  = regexp.MustCompile(`(?i)\b[a-z]+-(?:144|240|288|360|432|480|540|576|720|1080|2160)\b`)
	trailingResolutionQualityTagRegex = regexp.MustCompile(`(?i)^(?:fhd|uhd|hd)-(?:144|240|288|360|432|480|540|576|720|1080|2160)$`)
	// qualitySeriesNumberRegex recognizes a consumed remaster phrase that is
	// display-quality vocabulary rather than a catalog id: a quality series
	// word (FHD/UHD/HD/SD) with a 3-4 digit resolution number. The plain
	// hyphenated trailing-id bounds (two digits, selected three digits,
	// six-plus digits) double as the quality-token veto — HD-720, FHD-1080,
	// and UHD-3840 are exactly the excluded shapes — so they cannot be
	// widened wholesale. But once the leading phrase is itself recognized
	// vocabulary, the boundary is already decided by the phrase: a trailing
	// hyphenated candidate with a plain series word is the real catalog id
	// at the reserved digit counts (one, four, five), and the phrase's
	// remaster marker moves onto it.
	qualitySeriesNumberRegex = regexp.MustCompile(`(?i)^(?:fhd|uhd|hd|sd)-\d{3,4}$`)
	// trailingQualitySeriesRegex vetoes hyphenated candidates whose series
	// word is itself quality vocabulary: a trailing FHD-1080 or UHD-3840
	// behind a quality phrase is more display metadata, not a catalog id.
	trailingQualitySeriesRegex = regexp.MustCompile(`(?i)^(?:fhd|uhd|hd|sd)-`)
	// trailingPlainHyphenatedIDRegex recognizes hyphenated candidates at
	// the digit counts the conservative trailing grammar reserves for the
	// quality veto: one, four, or five digits (ABC-1, ABC-1234, ABC-12345).
	trailingPlainHyphenatedIDRegex = regexp.MustCompile(`(?i)[a-z]{1,}-(?:\d|\d{4,5})\b`)
	remasterPartLabelRegex         = regexp.MustCompile(`(?i)\b(?:part|pt|disc|vol|cd)-?\d{1,2}\b`)
	resolutionTokenRegex           = regexp.MustCompile(`(?i)^\d{3,4}x\d{3,4}$`)
	resolutionTailRegex            = regexp.MustCompile(`(?i)^[-_.\s]?(?:\d{3,4}[pi]|\d{3,4}x\d{3,4}|(?:144|240|288|360|432|480|540|576|720))(?:\D|$)`)
	framerateTokenRegex            = regexp.MustCompile(`(?i)^\d{3,4}[pi](?:\d{2,3})?$`)
	remasterMarkerTailRegex        = regexp.MustCompile(`(?i)(?:ez)?(?:hd|ai|h)$`)
	rawTokenRegex                  = regexp.MustCompile(`[A-Za-z0-9]+`)
	fusedPartLabelTailRegex        = regexp.MustCompile(`(?i)(?:cd|disc|disk|pt|part)\d{1,2}$`)
	strongRawTokenRegex            = regexp.MustCompile(`(?i)^(?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[ez]?(?:hd|ai|h))$`)
	zeroPaddedRawTokenRegex        = regexp.MustCompile(`(?i)^(?:t28|[a-z]+)0\d{3,4}[a-z]{0,3}$`)
	weakWordYearRegex              = regexp.MustCompile(`(?i)^[a-z]{4,}\d{4,5}[a-z]{0,3}$`)
)

func builtinMatchConflictsWithContentID(s string, pattern *regexp.Regexp) bool {
	start, end, ok := contentIDCandidate(s)
	if !ok {
		return false
	}
	match := pattern.FindStringSubmatchIndex(s)
	if len(match) <= 3 || (match[2] <= start && match[3] >= end) {
		return false
	}
	if match[2] < end && match[3] > start {
		return true
	}
	return !strings.ContainsAny(s[match[2]:match[3]], "-_")
}

// builtinQualityShadowsContentID reports whether the builtin-captured id is
// itself nothing but a quality tag (x265, FHD720): such tags must never win
// when a stronger raw content id appears elsewhere in the name.
func builtinQualityShadowsContentID(name, id string) bool {
	lower := strings.ToLower(id)
	if trailingQualityTagRegex.FindString(lower) != lower {
		return false
	}
	idText, _ := contentIDPrefixMatch(name)
	return idText != ""
}

// trailingCandidateVetoSpan returns the span a trailing-catalog candidate is
// vetted against as a quality tag: the candidate itself, or the candidate
// extended back over a compound source-tag prefix (web plus one separator)
// that directly precedes it. The catalog scan splits compound source
// spellings at their separator — WEB-DL2160 leaves the DL2160 fragment
// standing alone as an id-shaped candidate while the WEB- prefix belongs to
// it — so the veto must see the compound spelling for the vocabulary's digit
// bound to decide it as a whole. The leading boundary before the web prefix
// keeps the class's word-boundary protections: fweb (FWEB-DL2160) and
// numerically prefixed (189WEB-DL1) spellings never extend.
func trailingCandidateVetoSpan(remainder string, candidateIndex []int) string {
	spanStart := candidateIndex[0]
	if prefix := compoundSourceTagPrefixRegex.FindStringSubmatchIndex(remainder[:spanStart]); prefix != nil {
		spanStart = prefix[2]
	}
	return remainder[spanStart:candidateIndex[1]]
}

func normalizeFusedRemasterFilename(name string, builtinPattern *regexp.Regexp) string {
	m := fusedRemasterRegex.FindStringSubmatchIndex(name)
	fused := m != nil
	if m == nil {
		m = separatedRemasterRegex.FindStringSubmatchIndex(name)
	}
	if m == nil {
		return ""
	}
	if !fused {
		separatedID := name[m[2]:m[3]] + name[m[4]:m[5]]
		if m[6] >= 0 {
			separatedID += name[m[6]:m[7]]
		}
		if weakWordYearRegex.MatchString(separatedID) {
			return normalizeFusedRemasterFilename(name[m[1]:], builtinPattern)
		}
	}
	// Part labels (part-2, pt 3) are not catalog ids and must not suppress
	// the fused normalization.
	remainder := remasterPartLabelRegex.ReplaceAllString(name[m[1]:], "")
	// Compact codec/resolution tags (x265, FHD720) also match the trailing id
	// alternatives; they are tags, not replacement ids, so veto the suppression.
	for _, candidateIndex := range trailingCatalogIDRegex.FindAllStringIndex(remainder, -1) {
		candidate := remainder[candidateIndex[0]:candidateIndex[1]]
		// A compound source tag splits at its separator in the catalog
		// scan, so the fragment arrives as a standalone candidate while
		// the web prefix belongs to it: the veto spans the compound
		// spelling and the vocabulary decides it as a whole.
		if trailingQualityTagRegex.MatchString(trailingCandidateVetoSpan(remainder, candidateIndex)) {
			continue
		}
		candidateRemainder := remainder[candidateIndex[0]:]
		if normalized := normalizeFusedRemasterFilename(candidateRemainder, builtinPattern); normalized != "" {
			return normalized
		}
		if builtinPattern.FindStringIndex(candidate) != nil {
			return candidateRemainder
		}
		if idText, _ := contentIDPrefixMatch(candidate); idText != "" {
			return candidateRemainder
		}
	}
	for _, candidateIndex := range trailingResolutionCatalogIDRegex.FindAllStringIndex(remainder, -1) {
		candidate := remainder[candidateIndex[0]:candidateIndex[1]]
		if trailingResolutionQualityTagRegex.MatchString(candidate) {
			continue
		}
		if builtinPattern.FindStringIndex(candidate) != nil {
			return remainder[candidateIndex[0]:]
		}
	}
	// A quality-phrased remaster (FHD 1080 HD ABC-1234) leaves the real
	// catalog id in the remainder at the digit counts the grammars above
	// reserve for the quality veto; with the phrase itself recognized
	// vocabulary, those counts are safe for a plain series word, and the
	// phrase's marker belongs to the trailing id.
	if qualitySeriesNumberRegex.MatchString(name[m[2]:m[3]] + "-" + name[m[4]:m[5]]) {
		for _, candidateIndex := range trailingPlainHyphenatedIDRegex.FindAllStringIndex(remainder, -1) {
			candidate := remainder[candidateIndex[0]:candidateIndex[1]]
			if trailingQualityTagRegex.MatchString(candidate) || trailingQualitySeriesRegex.MatchString(candidate) {
				continue
			}
			return candidate + "-" + name[m[8]:m[9]] + remainder[candidateIndex[1]:]
		}
	}
	// A fused part label directly after the marker (rct156hdcd2) is split
	// off with a separator so the builtin marker and part detection see the
	// canonical hyphenated spelling (rct-156hd-cd2).
	rest := name[m[4]:]
	if m[10] >= 0 {
		labelStart := m[10] - m[4]
		rest = rest[:labelStart] + "-" + rest[labelStart:]
	}
	// A prefix-free compact t28 tail with a three-digit number reads as the
	// T-series release T-28123H (catalog-prefixed or separator-pinned forms
	// stay T28-123).
	if fused && strings.EqualFold(name[m[2]:m[3]], "t28") && m[5]-m[4] == 3 {
		return name[:m[2]] + "t-28" + rest
	}
	return name[:m[3]] + "-" + rest
}

func splitRemasterMarker(remainder string) (string, string) {
	remainder = strings.TrimSpace(remainder)
	m := reRemasterRemainder.FindStringSubmatchIndex(remainder)
	if m == nil {
		return "", remainder
	}
	marker := strings.ToUpper(remainder[m[2]:m[3]])
	// H/HD before codec digits stays ambiguous (H.264-class), but AI is an
	// explicit release marker and must survive a following codec tag, and a
	// resolution tag (720p, 1080i) is not a codec spelling.
	if marker != "AI" && remasterCodecTailRegex.MatchString(remainder[m[3]:]) && !resolutionTailRegex.MatchString(remainder[m[3]:]) {
		return "", remainder
	}
	return marker, remainder[m[3]:]
}

func remasterMarkerSpelling(remainder string) string {
	spelling, _ := splitRemasterMarker(remainder)
	return spelling
}

func foldRemasterMarker(spelling string) string {
	if spelling == "HD" {
		return "H"
	}
	return spelling
}

// remasterBarePartCeiling bounds the plain part numbers accepted directly
// after a consumed remaster marker: 24 and above are fps shorthands
// (24/25/30/50/60), not plausible part counts.
const remasterBarePartCeiling = 23

var bareNumericPartSuffixRegex = regexp.MustCompile(`^-\d{1,2}$`)

// isFPSLikeBarePartNumber reports whether a part detected right after a
// consumed remaster marker is a bare numeric in fps-shorthand range
// ("IPX-535-HD-60"): such numbers are quality metadata, not part numbers.
// Labeled parts (cd2, pt2) and small plain numbers stay parts.
func isFPSLikeBarePartNumber(num int, partSuffix string) bool {
	return num > remasterBarePartCeiling && bareNumericPartSuffixRegex.MatchString(partSuffix)
}

// rawTokenCandidateEnd returns the candidate end offset relative to the
// token start, or isCandidate=false when the token cannot back a content-id
// candidate. A token carrying a fused part-label tail (1rct00156hcd2) is a
// candidate only up to the label when the trimmed spelling is itself strong
// or zero-padded, so the label feeds part detection instead of malforming
// the id; the full token stays a candidate when only the whole spelling is
// strong (118cd2).
func rawTokenCandidateEnd(token string) (int, bool) {
	if isResolutionToken(token) || trailingQualityTagRegex.MatchString(token) {
		return 0, false
	}
	if loc := fusedPartLabelTailRegex.FindStringIndex(token); loc != nil {
		trimmed := token[:loc[0]]
		if strongRawTokenRegex.MatchString(trimmed) || zeroPaddedRawTokenRegex.MatchString(trimmed) {
			return loc[0], true
		}
	}
	if strongRawTokenRegex.MatchString(token) || zeroPaddedRawTokenRegex.MatchString(token) {
		return len(token), true
	}
	return 0, false
}

// contentIDPrefixMatch extracts a content-id prefix from a stem, returning the
// captured id text and the post-id remainder (which may carry part suffixes).
// contentIDCandidate locates the raw content id the name should resolve to.
// The leftmost shape match wins unless it is a weak prefixless form without a
// marker tail (a word plus a year, e.g. birthday2024). Weak forms are only
// accepted when a numeric-prefixed or marker-bearing raw id later in the name
// corroborates them.
func contentIDCandidate(s string) (start, end int, ok bool) {
	m := contentIDShapeRegex.FindStringSubmatchIndex(s)
	if m == nil {
		return 0, 0, false
	}
	id := s[m[2]:m[3]]
	if remasterMarkerTailRegex.MatchString(id) {
		return m[2], m[3], true
	}
	if isResolutionToken(id) || trailingQualityTagRegex.MatchString(id) {
		// The leftmost shape hit is a resolution or quality token;
		// it never becomes the candidate, but a strong raw id later in the
		// name still wins, as with the weak standalone-token case. Without
		// one, there is no candidate.
		for _, loc := range rawTokenRegex.FindAllStringIndex(s, -1) {
			tokEnd, isCandidate := rawTokenCandidateEnd(s[loc[0]:loc[1]])
			if !isCandidate {
				continue
			}
			return loc[0], loc[0] + tokEnd, true
		}
		return 0, 0, false
	}
	for _, loc := range rawTokenRegex.FindAllStringIndex(s, -1) {
		tokEnd, isCandidate := rawTokenCandidateEnd(s[loc[0]:loc[1]])
		if !isCandidate {
			continue
		}
		return loc[0], loc[0] + tokEnd, true
	}
	if weakWordYearRegex.MatchString(id) {
		return 0, 0, false
	}
	return m[2], m[3], true
}

func contentIDPrefixMatch(s string) (idText string, remainder string) {
	start, end, ok := contentIDCandidate(s)
	if !ok {
		return "", ""
	}
	return s[start:end], strings.TrimSpace(s[end:])
}

func matchContentIDShape(s string) string {
	idText, _ := contentIDPrefixMatch(s)
	if idText == "" {
		return ""
	}
	return strings.ToUpper(idText)
}

func isResolutionToken(idText string) bool {
	return resolutionTokenRegex.MatchString(idText) || framerateTokenRegex.MatchString(idText)
}
