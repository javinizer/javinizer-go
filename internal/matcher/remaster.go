package matcher

import (
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// tagDelimiterClass is the closed, non-alphanumeric delimiter set shared by
// every marker-tail boundary (the fused, separated, and long-number
// normalizations plus the splitRemasterMarker remainder) and by the
// content-id shape's remainder introducer: the ASCII separator family —
// hyphen, underscore, dot, whitespace, and the square and round brackets —
// plus the curly braces and CJK bracket pairs (〈〉《》「」『』【】〔〕) that
// scene names ({1080p}) and JP-sourced names (【1080p】) wrap tags in. A tag
// may sit directly against the remaster marker, so a boundary that accepts
// one delimiter must accept the whole family: rejecting the brace forms
// fails the marker-tail boundary, so a remastered release collapses to its
// base id (ABC-123-HD{1080p} matched ABC-123) and the tier-2 content-id
// shape loses the candidate the same way (1rct00156h{1080p} matched
// nothing). The class stays bounded and non-alphanumeric rather than a \W
// catch-all so a marker-bearing letter still fails the boundary and rejects
// the marker (ABC-123-HDA keeps the base id, not a marker A). Fullwidth
// ASCII delimiters （）｛｝ fold to their halfwidth members via
// foldFullwidthASCII (the fold covers U+FF01-U+FF5E and the ideographic
// space only), but the CJK brackets live outside that range and take
// explicit membership here.
const tagDelimiterClass = `[-_.\s[\](){}【】「」『』《》〈〉〔〕]`

// codecProfileHeadAlternation and codecProfileWordAlternation are the two
// halves of the codec-profile compound vocabulary: the audio codecs that
// carry separated profile spellings (DTS-HD, DTS-HD MA, AAC-LC,
// TRUEHD-ATMOS) and the profile words that ride behind them. The halves
// are shared by the quality vocabulary's codec-profile alternative and the
// veto-span prefix recognizer (compoundCodecProfileTagPrefixRegex) so the
// two grammars cannot drift apart. The head set stops at the audio codecs
// with profile spellings — flac, opus, and pcm carry none — and the word
// set stops at the profiles those codecs spell: DTS's HD/MA/X/HRA, AAC's
// LC/HE/SBR, and the EX/ATMOS family the Dolby spellings carry. The heads
// dts, aac, and dd are real series in the r18.dev content-id prefix
// lookup, and so are the profile words hd, lc, ma, he, x, and ex; the
// bounds documented at each use keep their id shapes intact.
const (
	codecProfileHeadAlternation = `dts|aac|ac3|eac3|truehd|dd|ddp`
	codecProfileWordAlternation = `hd|ma|x|lc|he|ex|hra|sbr|atmos`
	// fusedCodecProfileHeadAlternation is the codec-profile head subset
	// that carries the fused, separator-free profile spelling
	// (DTSHD192, AACLC192, TRUEHDATMOS768): every head except dd, whose
	// fused compounds collide with real series in the r18.dev content-id
	// prefix lookup (ddex, ddma, ddxx) and so fail the round-21 fused
	// reasoning that clears the rest — see the codec-profile comment at
	// the quality vocabulary.
	fusedCodecProfileHeadAlternation = `dts|aac|ac3|eac3|truehd|ddp`
)

var (
	// Volume suffixes ride the part-label groups (vol joins
	// cd/disc/disk/pt/part): a fused vol after the remaster marker
	// (RCT156HDvol2, RCT-156HDvol2) flows to the same part detection as
	// cd2 instead of failing the marker boundary (compact form: no
	// match) or parsing as the raw id 156HDVOL2. The round-6 decision
	// excluded vol from these groups because DetectPartSuffix had no vol
	// grammar (reDiscPart covered cd/disc/disk, reNumericPart pt/part);
	// reDiscPart now accepts vol, and no bare vol series exists in the
	// r18.dev content-id prefix lookup (evol/gvol/qvol/zvol/vola/vold
	// keep their leading letters), so the marker-boundary acceptance is
	// safe and the suffix feeds part detection instead of the id.
	fusedRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)((?:\d{1,3}|\d{6}))([ez]?)(hd|ai|h)(?:(cd|disc|disk|pt|part|vol)(\d{1,2}))?(?:$|` + tagDelimiterClass + `)`)
	// The separated grammar accepts the E/Z catalog suffix with a separator
	// on BOTH sides of it — the number may separate it from the marker
	// either way (IPX-535Z-HD, IPX-535-ZHD, IPX-535-Z-HD, IPX.535.Z.HD) —
	// matching the scraper-side identity parsers (parseRemasterTail /
	// displayIdentityTuple), which strip a display id's separators before
	// reading series, number, suffix and marker: permitting the separator
	// only after the suffix read the separated spellings as the base id
	// (IPX-535) with no marker, so the matcher and the scraper disagreed
	// about the same filename. The normalization splices the pre-suffix
	// separator out (see normalizeFusedRemasterFilename) so the canonical
	// spelling keeps the suffix fused to the number (IPX-535Z-HD), the
	// shape the built-in tier parses and the scraper search spellings
	// share. The capture layout stays (series, number, E/Z suffix, marker,
	// part label, part digits) so the caller's index handling stays uniform
	// across the three grammars; the suffix group no longer participates
	// when the spelling carries no suffix, which every caller guards with
	// m[6] >= 0.
	separatedRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(t28|[a-z]+)[._\s]+(\d{1,6})(?:[-._\s]?([ez]))?[-._\s]?(hd|ai|h)(?:(cd|disc|disk|pt|part|vol)(\d{1,2}))?(?:$|` + tagDelimiterClass + `)`)
	// The compact 4-5-digit display number rides its own grammar beside
	// the legacy fused one: the separated spelling (ABC.1234.HD) and the
	// round-11 scraper classifier decision (zero-padding is the raw-cid
	// evidence; a non-padded compact number is a display id) both
	// canonicalize ABC1234HD/ABC12345AI, so the fused tier must too or the
	// content-id fallback returns the raw spelling with no marker. The
	// bounds reconcile the raw surfaces the legacy 1-3/6-digit grammar
	// never reaches: the number must be non-padded (a leading [1-9] —
	// abc01234h and the zero-padded marker-bearing raw ids keep tier-2),
	// and the series word must carry 3+ letters — the 1-2-letter
	// short-prefix family (AC3640H; the real ac series keeps its fused
	// spellings) and the t28 tail (t28123h is a raw cid per the same
	// classifier, and prefix-free t28 numbers keep the T-series special
	// case) stay on their existing paths. Four-plus-letter series pass
	// this regex but bail as word-years in the caller (vacation2024hd)
	// unless the caller's catalog-series discriminator (see
	// catalogSeriesReleaseNumber) recognizes a catalog release number
	// (MIAA1234HD), exactly like the separated spelling.
	fusedLongNumberRemasterRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([a-z]{3,})((?:[1-9]\d{3,4}))([ez]?)(hd|ai|h)(?:(cd|disc|disk|pt|part|vol)(\d{1,2}))?(?:$|` + tagDelimiterClass + `)`)
	// The marker-tail remainder accepts the E/Z catalog suffix with a
	// separator on BOTH sides of it (IPX-535-Z-HD leaves "-Z-HD" after the
	// built-in tier's id capture; IPX-535-ZH leaves "-ZH"), mirroring the
	// both-sides separator the separated remaster grammar carries and the
	// scraper identity parsers' separator-stripped tail parse: the built-in
	// pattern cannot capture a suffix the separators split from the number,
	// so without this slot the hyphenated spelling resolves to the base id
	// (IPX-535) with no marker. The suffix letter is captured so
	// splitRemasterMarker can hand it to the caller to ride onto the id the
	// same way the folded marker does (IPX-535 + Z + H -> IPX-535ZH); the
	// marker stays the first alternative that can match alone ("-HD",
	// ".HD", " HD" keep their existing parse with no suffix).
	reRemasterRemainder    = regexp.MustCompile(`(?i)^[-._\s]?(?:([ez])[-._\s]?)?(HD|AI|H)(?:(cd|disc|disk|pt|part|vol)\d{1,2})?(?:$|` + tagDelimiterClass + `)`)
	remasterCodecTailRegex = regexp.MustCompile(`(?i)^[-_.\s]?\d{3}(?:\D|$)`)
	contentIDShapeRegex    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[A-Za-z]{0,3}))(?:` + tagDelimiterClass + `(.*)$|(?:cd|disc|disk|pt|part|vol)\d{1,2}$|$)`)
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
	// TrueHD joins the rate class with the same fold — the codec name
	// is letters-only like the dts/flac/opus members, and its bare
	// literal stops at the word boundary before the digits, so the
	// numbered sample-rate spelling (TRUEHD192) otherwise satisfies the
	// builtin amateur pattern and the trailing catalog grammar as a
	// replacement catalog id behind a separated remaster id, on both
	// entry points. No truehd series exists in the r18.dev content-id
	// prefix lookup, and the class keeps the rate members' 3-4 digit
	// bound: two-digit numerals (TRUEHD24) stay catalog-id grammar,
	// five-digit zero-padded tokens (TRUEHD00123) keep the raw-id path,
	// and hyphenated spellings (TRUEHD-768) keep the bare literals' veto.
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
	// The Blu-ray/DVD rip spellings extend the source class with a
	// required qualifier — bd, br, or dvd plus an optional single
	// separator and rip, directly followed by the 3-4 digit resolution
	// number — because the compact BDRIP1080, BRRIP1080, and DVDRIP480
	// spellings and their BD-RIP1080, BD.RIP1080, and BD RIP1080
	// siblings otherwise satisfy the builtin amateur pattern and the
	// trailing catalog grammar as replacement ids behind a separated
	// remaster id. The qualifier is required where web's is optional
	// because the heads are real series: bd, bdr, br, and dvd all exist
	// in the r18.dev content-id prefix lookup, and the round-12
	// decision keeps their bare resolution spellings (BD1080, BDR1080,
	// DVD480) in catalog-id grammar. The rip compound is a longer token
	// than every real head it extends — bdrip, brrip, dvdrip, and rip
	// are not series in the lookup, and no real bd, bdr, br, or dvd id
	// carries a rip fragment (the real dvd family tops out at heads
	// like dvdes and dvdzm, all shorter than dvdrip) — so the
	// word-boundary shapes differ: bare BD1080, BDR1080, and DVD480
	// tokens, hyphenated display ids (BR-616, DVD-480), and numerically
	// prefixed content ids (3bd00108, 155bdr00108, 150dvd00123) keep
	// matching as ids while the compound BDRIP1080 and DVDRIP480
	// spellings become metadata. The digit bound rides the class —
	// five-plus-digit fragments stay id grammar (BD-RIP12345 leaves
	// RIP12345 standing, like WEB-DL12345 leaves DL12345), and
	// two-digit numerals (BD-RIP24) already fail the amateur pattern's
	// own digit bound — and the hyphenated, dotted, and spaced siblings
	// ride the compound span (see compoundSourceTagPrefixRegex) exactly
	// like the web compounds.
	// Codec-profile compounds join the vocabulary as a class — an audio
	// codec head (dts, aac, ac3, eac3, truehd, dd, or ddp) plus one
	// separator and one or more profile words (DTS-HD, DTS-HD MA,
	// AAC-LC, TRUEHD-ATMOS), directly followed by the 3-4 digit rate —
	// because the profile-bearing spellings split at their separator in
	// the catalog scan: DTS-HD192 and AAC-LC192 leave the HD192/LC192
	// fragments standing alone as id-shaped candidates while the
	// codec-profile prefix belongs to them, so the veto span extends
	// back over the prefix (see compoundCodecProfileTagPrefixRegex) and
	// the vocabulary decides the compound as a whole, exactly as it
	// does for the web source compounds. The bounds mirror the class:
	// the profile word must be letters directly behind the head's one
	// separator, so hyphenated display ids (AAC-1086) and zero-padded
	// fused ids (DTS00123) of the real dts/aac/dd series keep their id
	// grammar, and the rate rides the class's 3-4 digit bound, so
	// five-plus-digit id-shaped fragments (DTS-HD12345) stay outside the
	// compound. The profile words hd, lc, ma, he, x, and ex are real
	// series too, and their bare id shapes (HD1080, LC1234, X192) keep
	// matching as ids: without a codec head the veto span never extends,
	// exactly as a DL2160 fragment without the web prefix stays id
	// grammar. The dts/aac/ac3/eac3/truehd/ddp heads are already bare
	// literals in this vocabulary, so an extended span over them vetoes
	// by those literals as well — the same veto DTS-24 rides today —
	// while the dd head (no bare literal of its own) leans on the
	// compound's own bounds.
	// Fused codec-profile spellings join the same family — the profile
	// words ride directly behind the head with no separator
	// (DTSHD192, AACLC192, TRUEHDATMOS768), so the token never splits in
	// the catalog scan and arrives whole as an id-shaped candidate that
	// satisfies both the trailing catalog grammar and the builtin amateur
	// pattern: the vocabulary must decide it directly, exactly as it
	// decides the separated compound as a span. The bounds mirror the
	// family's — the words are the shared profile vocabulary (one or
	// more, like the separated compound's word iterations) and the rate
	// rides the 3-4 digit bound — so five-plus-digit id-shaped tokens
	// (DTSHD12345) and two-digit numerals (DTSHD24) stay id grammar. The
	// dd head carries no fused variant: its compounds collide with real
	// series in the r18.dev content-id prefix lookup — ddex (n_726),
	// ddma (111, h_175), and ddxx (111) — so the round-21 fused
	// reasoning that clears the other heads (the compound is a longer
	// letter-run than every real head it extends, and dtshd, dtsma,
	// dtsx, dtshdma, dtshra, aaclc, aache, aacsbr, truehdatmos,
	// ddpatmos, and ddatmos are all absent from the lookup) fails for
	// it: the bare DDEX192 keeps id grammar while its separated sibling
	// DD-EX448 — a spelling no real ddex id uses, since display ids
	// hyphenate between series and number and raw ids ride the n_726
	// prefix — stays vetoed. The word-boundary anchors keep the class's
	// protections: xdts (XDTSHD192) and numerically prefixed
	// (189dts00087) spellings never match, and the digit-bearing
	// ac3/eac3 compounds cannot collide with a lookup series at all
	// because content-id series are letter-runs.
	trailingQualityTagRegex = regexp.MustCompile(`(?i)\b(?:[hx]26[3-9]|avc\d*|aac\d*|hevc\d*|vvc\d*|ac3|dts|flac|opus|truehd|vc1|av1|mp[34]|ddp\d*|eac3|divx\d*|xvid\d*|prores\d*|yuv\d*|rgb\d*|p0(?:10|16)|mpeg\d*|vp\d+|fhd\d{2,4}|uhd\d{2,4}|hdtv|hdr\d*|bt2020|bt709|rec709|smpte\d+|pq\d+|st2084|hlg\d*|ycbcr\d*|(?:bt|rec|st|smpte)[. ]?\d{3,4}|(?:l?pcm|dts|flac|opus|e?ac3|truehd)[. ]?\d{3,4}|\d+(?:bit|point)\d+|(?:web(?:[-_. ]?(?:dl(?:rip)?|rip))?|remux|bluray|(?:bd|br|dvd)[-_. ]?rip)\d{3,4}|` + `(?:` + codecProfileHeadAlternation + `)[-_. ](?:` + codecProfileWordAlternation + `)(?:[-_. ]?(?:` + codecProfileWordAlternation + `))*\d{3,4}|(?:` + fusedCodecProfileHeadAlternation + `)(?:` + codecProfileWordAlternation + `)+\d{3,4})\b`)
	// compoundSourceTagPrefixRegex recognizes the source-tag prefix —
	// web, bd, br, or dvd plus exactly one separator — ending where a
	// trailing-catalog candidate begins, so the candidate's quality veto
	// can span the compound spelling (WEB-DL2160, BD-RIP1080,
	// DVD-RIP1080) that the catalog scan split apart. The bd, br, and
	// dvd heads are real series, but the span extension stays inert for
	// them: the extended span must still match the quality vocabulary,
	// and no real bd, bdr, br, or dvd id carries a rip fragment. The
	// leading boundary requirement (start of text or a non-alphanumeric
	// character before the head) keeps the word-boundary protections of
	// the source class: fweb, xbd, and 189web spellings never extend the
	// span, and the near-miss heads with leading letters stay protected
	// too — dvdp and dvdes are real series, so DVDP-RIP1080 keeps its
	// RIP1080 fragment in id grammar the way XBD-RIP1080 does.
	compoundSourceTagPrefixRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(web|bd|br|dvd)[-_. ]$`)
	// compoundCodecProfileTagPrefixRegex recognizes the codec-profile
	// prefix — an audio codec head plus zero or more separated profile
	// words and exactly one trailing separator — ending where a
	// trailing-catalog candidate begins, so the candidate's quality
	// veto can span the profile-bearing compound spelling (DTS-HD192,
	// AAC-LC192, TRUEHD-ATMOS768) that the catalog scan split apart:
	// the fragment after the separator (HD192, LC192) arrives alone as
	// an id-shaped candidate while the DTS-HD/AAC-LC prefix belongs to
	// it, exactly as the DL2160 fragment belongs to its WEB- prefix.
	// The profile words may ride inside the prefix instead — DTS-HD
	// MA768 leaves the MA768 fragment standing — so the iterations cover
	// both layouts. The leading boundary before the head keeps the
	// class's word-boundary protections: xdts (XDTS-HD192) and
	// numerically prefixed (189dts1) spellings never extend the span,
	// and a profile-word fragment without a codec head (HD1080, LC1234
	// — the hd and lc series are real) stays id grammar.
	compoundCodecProfileTagPrefixRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(` + codecProfileHeadAlternation + `)(?:[-_. ](?:` + codecProfileWordAlternation + `))*[-_. ]$`)
	trailingResolutionCatalogIDRegex   = regexp.MustCompile(`(?i)\b[a-z]+-(?:144|240|288|360|432|480|540|576|720|1080|2160)\b`)
	trailingResolutionQualityTagRegex  = regexp.MustCompile(`(?i)^(?:fhd|uhd|hd)-(?:144|240|288|360|432|480|540|576|720|1080|2160)$`)
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
	explicitMarkerTailRegex        = regexp.MustCompile(`(?i)(?:ez)?(?:hd|ai)$`)
	rawTokenRegex                  = regexp.MustCompile(`[A-Za-z0-9]+`)
	fusedPartLabelTailRegex        = regexp.MustCompile(`(?i)(?:cd|disc|disk|pt|part|vol)\d{1,2}$`)
	strongRawTokenRegex            = regexp.MustCompile(`(?i)^(?:\d+(?:t28|[A-Za-z]+)\d+[A-Za-z]{0,3}|(?:t28|[A-Za-z]+)\d{4,5}[ez]?(?:hd|ai|h))$`)
	zeroPaddedRawTokenRegex        = regexp.MustCompile(`(?i)^(?:t28|[a-z]+)0\d{3,4}[a-z]{0,3}$`)
	weakWordYearRegex              = regexp.MustCompile(`(?i)^[a-z]{4,}\d{4,5}[a-z]{0,3}$`)
	// weakWordYearSeriesRegex captures the leading series word of a weak
	// word-year id — weakWordYearRegex guarantees a word-first shape — so
	// the raw-cid surfaces can apply the round-24 display-case axis to the
	// same token the fused normalization's word-year guard rejected.
	weakWordYearSeriesRegex = regexp.MustCompile(`(?i)^[a-z]+`)
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
// extended back over a compound source-tag prefix (web, bd, br, or dvd
// plus one separator) or a codec-profile prefix (an audio codec head plus its
// profile words and one separator) that directly precedes it. The catalog
// scan splits
// compound source spellings at their separator — WEB-DL2160 leaves the
// DL2160 fragment standing alone as an id-shaped candidate while the
// WEB- prefix belongs to it — so the veto must see the compound
// spelling for the vocabulary's digit bound to decide it as a whole.
// The leading boundary before the prefix keeps the class's
// word-boundary protections: fweb (FWEB-DL2160), xbd (XBD-RIP1080),
// and numerically prefixed (189WEB-DL1, 189dts1) spellings never extend.
// The two prefix recognizers are checked independently against the
// candidate's own start: a codec head never sits inside a web compound's
// head, so at most one of them matches the same text ending, and the
// codec-profile span covers the profile word riding on either side of
// the split — DTS-HD192 leaves HD192 standing, and DTS-HD MA768 leaves
// MA768 standing the same way.
func trailingCandidateVetoSpan(remainder string, candidateIndex []int) string {
	spanStart := candidateIndex[0]
	if prefix := compoundSourceTagPrefixRegex.FindStringSubmatchIndex(remainder[:spanStart]); prefix != nil {
		spanStart = prefix[2]
	}
	if prefix := compoundCodecProfileTagPrefixRegex.FindStringSubmatchIndex(remainder[:candidateIndex[0]]); prefix != nil {
		spanStart = prefix[2]
	}
	return remainder[spanStart:candidateIndex[1]]
}

// fusedRemasterSubmatchIndex returns the submatch index of the leftmost
// compact remaster spelling in name: the legacy grammar (1-3- and 6-digit
// numbers on any series word or the t28 tail) or the long-number display
// grammar (non-padded 4-5 digits on a 3+-letter series). Both grammars
// share the capture layout (series, number, E/Z suffix, marker, part
// label, part digits), so the caller's index handling is uniform. The
// leftmost match wins so an earlier display-number spelling is not
// displaced by a later legacy spelling.
func fusedRemasterSubmatchIndex(name string) []int {
	m := fusedRemasterRegex.FindStringSubmatchIndex(name)
	long := fusedLongNumberRemasterRegex.FindStringSubmatchIndex(name)
	if long != nil && (m == nil || long[0] < m[0]) {
		return long
	}
	return m
}

// catalogSeriesReleaseNumber reports whether a 4-5-digit number riding a
// 4+-letter series word is a catalog release number rather than a prose
// year phrase, on two axes a word-year never satisfies jointly. The series
// must be a real catalog series — present in the r18.dev content-id prefix
// lookup — and the built-in matcher itself must support the series+number
// spelling as a catalog id, which bounds the series to the amateur
// alternative's 3-6 letters and the number to its 3-4 digits (MIAA1234,
// ABCD1234; five-digit numbers and 7+-letter words like BIRTHDAY2024 have
// no builtin series shape). Both axes are case-insensitive — the lookup
// key lowercases the series word and the builtin pattern carries (?i) —
// so a lowercase spelling of a real catalog series (miaa1234hd,
// miaa.1234.hd) bypasses the word-year bail exactly as its uppercase
// sibling does: the round-24 case axis was meant to separate prose words
// from catalog series, but a real catalog series spelled lowercase is
// still a catalog series, so the discriminator is series-hood, not case.
// Prose year phrases fail at least one axis: non-series words (birthday,
// sample, vacation — none is a series in the lookup) fail the catalog axis
// in every casing, even when the builtin pattern would match the spelling
// (sample is six letters and sample2024 already matches the amateur
// alternative), and words or numbers beyond the builtin series shapes
// (BIRTHDAY2024, MIAA12345) fail the builtin-support axis and keep the raw
// tier-2 path. The builtin match must span the whole series+number so a
// partial hit inside a longer word cannot masquerade as catalog support.
func catalogSeriesReleaseNumber(series, number string, builtinPattern *regexp.Regexp) bool {
	seriesNumber := series + number
	loc := builtinPattern.FindStringIndex(seriesNumber)
	if loc == nil || loc[0] != 0 || loc[1] != len(seriesNumber) {
		return false
	}
	_, ok := r18devdump.ContentIDPrefixLookup[strings.ToLower(series)]
	return ok
}

// proseWordYearID reports whether a compact marker-tail id — the shape the
// raw-cid surfaces would accept as a marker-bearing token — is a lowercase
// prose word-year phrase (vacation2024hd, birthday2024ai) rather than a raw
// content id. The predicate is the one the fused normalization's word-year
// guard uses (weakWordYearRegex), bounded for the raw-cid surfaces: a
// zero-padded number is raw-cid evidence (the round-11 classifier —
// mide00968h and abeauty00123hd keep their raw ids), and a display-cased
// series word — the round-24 display-case axis, retired from the bypass by
// the round-36a series-hood fix but kept for the raw tier — marks a
// catalog spelling rather than prose (BIRTHDAY2024HD and MIAA12345HD keep
// the raw tier-2 id, and catalog-supported spellings — MIAA1234HD and its
// lowercase sibling miaa1234hd — canonicalize in the fused normalization
// before this tier ever runs).
func proseWordYearID(id string) bool {
	if !weakWordYearRegex.MatchString(id) || zeroPaddedRawTokenRegex.MatchString(id) {
		return false
	}
	series := weakWordYearSeriesRegex.FindString(id)
	return series != strings.ToUpper(series)
}

func normalizeFusedRemasterFilename(name string, builtinPattern *regexp.Regexp) string {
	m := fusedRemasterSubmatchIndex(name)
	fused := m != nil
	if m == nil {
		m = separatedRemasterRegex.FindStringSubmatchIndex(name)
	}
	if m == nil {
		return ""
	}
	// Word-year guard: a 4-5-digit number on a 4+-letter word is a year,
	// not a catalog number, on both separator-bearing surfaces (Vacation
	// 2024 HD) and the compact long-number grammar (vacation2024hd). The
	// legacy compact numbers (1-3, 6 digits) never collide with a year and
	// keep their guard-free path. The exception is a real catalog series
	// the built-in matcher itself supports: MIAA1234HD is a display release
	// number, not a word plus a year, so the guard bypasses only the
	// shapes catalogSeriesReleaseNumber accepts — in any casing, since the
	// discriminator is series-hood rather than display case (miaa1234hd and
	// miaa.1234.hd canonicalize too) — and every other word-year spelling
	// keeps the bail.
	if n := m[5] - m[4]; n >= 4 && n <= 5 {
		series := name[m[2]:m[3]]
		matchedID := series + name[m[4]:m[5]]
		if m[6] >= 0 {
			matchedID += name[m[6]:m[7]]
		}
		if weakWordYearRegex.MatchString(matchedID) && !catalogSeriesReleaseNumber(series, name[m[4]:m[5]], builtinPattern) {
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
		// A compound source tag or codec-profile spelling splits at its
		// separator in the catalog scan, so the fragment arrives as a
		// standalone candidate while the prefix belongs to it: the veto
		// spans the compound spelling and the vocabulary decides it as a
		// whole.
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
	// A separator between the number and the E/Z catalog suffix
	// (IPX-535-Z-HD, IPX.535.Z.HD) is spliced out of the remainder so the
	// suffix stays fused to the number in the canonical spelling
	// (IPX-535Z-HD) the built-in tier parses — the same separator-stripped
	// tail the scraper identity parsers read from a display id. The suffix
	// group participates with at most one separator in front (see
	// separatedRemasterRegex), so the splice removes that one character —
	// the separator sitting immediately before the captured suffix letter —
	// and the part-label offset below shifts with it; the fused grammars
	// never carry the separator, so their spellings are untouched.
	rest := name[m[4]:]
	suffixSep := 0
	if m[6] > m[5] {
		sepAt := m[6] - m[4] - 1
		rest = rest[:sepAt] + rest[sepAt+1:]
		suffixSep = 1
	}
	// A fused part label directly after the marker (rct156hdcd2) is split
	// off with a separator so the builtin marker and part detection see the
	// canonical hyphenated spelling (rct-156hd-cd2).
	if m[10] >= 0 {
		labelStart := m[10] - m[4] - suffixSep
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

// splitRemasterMarker splits a marker-bearing remainder into the folded
// marker spelling, the E/Z catalog suffix letter riding in front of the
// marker, and the post-marker remainder. The separated catalog-suffix
// spelling on the hyphenated family (IPX-535-Z-HD leaves "-Z-HD" after
// the built-in tier's id) is parsed here — the hyphenated series-number
// form never enters the separated remaster grammar, and the built-in
// pattern cannot capture a suffix the separator splits from the number,
// so the suffix letter rides onto the caller's id the same way the folded
// marker does (IPX-535 + Z + H -> IPX-535ZH). A remainder with no marker
// keeps its empty spelling, empty suffix, and the remainder verbatim.
func splitRemasterMarker(remainder string) (string, string, string) {
	remainder = strings.TrimSpace(remainder)
	m := reRemasterRemainder.FindStringSubmatchIndex(remainder)
	if m == nil {
		return "", "", remainder
	}
	marker := strings.ToUpper(remainder[m[4]:m[5]])
	// H/HD before codec digits stays ambiguous (H.264-class), but AI is an
	// explicit release marker and must survive a following codec tag, and a
	// resolution tag (720p, 1080i) is not a codec spelling.
	if marker != "AI" && remasterCodecTailRegex.MatchString(remainder[m[5]:]) && !resolutionTailRegex.MatchString(remainder[m[5]:]) {
		return "", "", remainder
	}
	catalogSuffix := ""
	if m[2] >= 0 {
		catalogSuffix = strings.ToUpper(remainder[m[2]:m[3]])
	}
	return marker, catalogSuffix, remainder[m[5]:]
}

func remasterMarkerSpelling(remainder string) (string, string) {
	spelling, catalogSuffix, _ := splitRemasterMarker(remainder)
	return spelling, catalogSuffix
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
// strong (118cd2). A trimmed spelling that ends in an explicit remaster
// marker (156HDvol2) is display-id debris — the number, marker, and part
// label tail of a hyphenated release name — and backs no candidate at any
// length: the builtin tier's marker handling owns it, so it never displaces
// the display id as a raw content id. A lowercase prose word-year — the
// same token the fused normalization's word-year guard rejected — is not a
// candidate at any length either (see proseWordYearID), so the marker-tail
// fallback and the token scan cannot disagree about the phrase.
func rawTokenCandidateEnd(token string) (int, bool) {
	if isResolutionToken(token) || trailingQualityTagRegex.MatchString(token) {
		return 0, false
	}
	if loc := fusedPartLabelTailRegex.FindStringIndex(token); loc != nil {
		trimmed := token[:loc[0]]
		if (strongRawTokenRegex.MatchString(trimmed) && !proseWordYearID(trimmed)) || zeroPaddedRawTokenRegex.MatchString(trimmed) {
			return loc[0], true
		}
		if explicitMarkerTailRegex.MatchString(trimmed) {
			return 0, false
		}
	}
	if (strongRawTokenRegex.MatchString(token) && !proseWordYearID(token)) || zeroPaddedRawTokenRegex.MatchString(token) {
		return len(token), true
	}
	return 0, false
}

// remasterPartLabelDebris reports whether a raw token or content-id shape
// match is display-id debris rather than a raw content id: a number and
// explicit remaster marker directly followed by a fused part label
// (156HDvol2 — the tail of "RCT-156-HD-vol2"). The marker is restricted to
// the explicit HD/AI spellings because bare H ends real series spellings in
// the r18.dev content-id prefix lookup (hcd, hhcd, hpt, lhpt, qhcd), so
// ids like 300hcd12 keep their id grammar; no series ends in hd or ai
// directly before a part label, and the label's 1-2 digit bound keeps the
// zero-padded numbers of real numerically prefixed ids out.
func remasterPartLabelDebris(token string) bool {
	loc := fusedPartLabelTailRegex.FindStringIndex(token)
	if loc == nil {
		return false
	}
	return explicitMarkerTailRegex.MatchString(token[:loc[0]])
}

// contentIDPrefixMatch extracts a content-id prefix from a stem, returning the
// captured id text and the post-id remainder (which may carry part suffixes).
// contentIDCandidate locates the raw content id the name should resolve to.
// The leftmost shape match wins unless it is a weak prefixless form without a
// marker tail (a word plus a year, e.g. birthday2024) or a lowercase prose
// word-year whose marker tail is a quality phrase rather than release
// metadata (vacation2024hd). Weak forms are only accepted when a
// numeric-prefixed or marker-bearing raw id later in the name corroborates
// them.
func contentIDCandidate(s string) (start, end int, ok bool) {
	m := contentIDShapeRegex.FindStringSubmatchIndex(s)
	if m == nil {
		return 0, 0, false
	}
	id := s[m[2]:m[3]]
	// A marker tail does not corroborate a prose word-year: the compact
	// word/year/quality phrase (vacation2024hd) is rejected by the fused
	// normalization's word-year guard, and this raw-marker fallback must not
	// re-accept the same token as a content id. The token flows on like the
	// weak prefixless form below: a genuine raw id later in the name still
	// wins, and without one there is no candidate.
	if remasterMarkerTailRegex.MatchString(id) && !proseWordYearID(id) {
		return m[2], m[3], true
	}
	if isResolutionToken(id) || trailingQualityTagRegex.MatchString(id) || remasterPartLabelDebris(id) {
		// The leftmost shape hit is a resolution token, quality tag, or
		// marker+part-label debris (156HDvol2); it never becomes the
		// candidate, but a strong raw id later in the name still wins, as
		// with the weak standalone-token case. Without one, there is no
		// candidate.
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
