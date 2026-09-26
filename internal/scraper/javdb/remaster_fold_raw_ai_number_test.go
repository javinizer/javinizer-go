package javdb

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fold key's rawCID flag — isRawRemasterCIDShape — is the raw-vs-
// display distinction the AI number-free exemption keys on, mirroring the
// DMM/R18 classifiers' raw-cid shape rules (r18dev's
// isRawRemasterContentIDQuery / rawRemasterCIDShapeRegex): a channel-
// prefixed cid, a catalog-digit prefix, a zero-padded five-digit number or
// a prefix-free t28 tail spells a raw content id, while any separator or a
// non-padded number spells a display id.
func TestFoldRemasterMarkerKeyRawCIDShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     string
		rawCID bool
	}{
		{"zero-padded slot number", "dv00899ai", true},
		{"uppercase cid lowercases first", "DV00899AI", true},
		{"catalog-prefixed cid", "1rct00156h", true},
		{"catalog-prefixed ai cid", "118dv00899ai", true},
		{"channel-prefixed cid", "h_003abc00123hd", true},
		{"prefix-free t28 tail", "t28123h", true},
		{"zero-padded t28 tail", "t2800123h", true},
		{"hyphenated display", "DV-818AI", false},
		{"underscore-bearing display", "T_28123_H", false},
		{"compact display spelling", "dv818ai", false},
		{"compact display h spelling", "rct156h", false},
		{"non-padded five-digit display", "abc12345h", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.rawCID, foldRemasterMarkerKey(tc.id).rawCID)
		})
	}
}

// remasterFoldMatchRank's raw AI number-free exemption: AI cid numbers are
// server slots, not display numbers (dv00899ai is DV-818AI), so a raw AI
// cid query compares series, catalog suffix and AI marker class against a
// display listing without the number — the DMM/R18 precedent's exception
// (rawDisplayMatchesCID / pageDisplayIdentityForCID), ranked below exact so
// a listing numbering the cid's own digits still outranks it. The exemption
// is scoped: raw H/HD queries keep binding the number (the round-40b/41
// rules), display AI queries keep binding it, and a raw cid listing facing
// a raw cid query still binds literally — different slots are different
// releases (r18dev's rawRemasterCIDEqual).
func TestRemasterFoldMatchRankRawAICIDNumberFree(t *testing.T) {
	target := foldRemasterMarkerKey("dv00899ai")
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("DV-818AI", target), "the finding: the slot number carries no display information")
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("DV-819AI", target), "the documented decision: any same-series AI display listing is accepted number-free")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("DV-00899AI", target), "a listing numbering the cid's own digits still outranks the number-free acceptance")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("dv00899ai", target), "the cid echoed as a listing ranks exact")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("IPX-818AI", target), "series must still match")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("DV-818H", target), "marker class must still match")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("DV-818-E-AI", target), "catalog suffix must still match")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("dv00900ai", target), "a raw cid listing is a different slot: literal binding, not number-free")
	// Raw H/HD cids keep the round-40b/41 number binding in both directions.
	hTarget := foldRemasterMarkerKey("1rct00156h")
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("RCT-156-HD", hTarget), "the padded cid number folds onto the display number")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("RCT-157-HD", hTarget), "a neighboring number is a different release")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("RCT-156-HD", foldRemasterMarkerKey("1rct00157h")), "the reverse neighboring direction is a different release too")
	// Display AI queries keep binding the number in every direction.
	display := foldRemasterMarkerKey("DV-818AI")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("DV-818AI", display))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("DV-819AI", display), "a display query cannot accept a neighboring AI listing number-free")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("dv00899ai", display), "a raw cid listing is not number-free for a display query either")
}

// The finding's scenario: a raw AI content id query (dv00899ai, whose slot
// number diverges from the display number by design) must resolve through
// its correct display listing DV-818AI instead of comparing 00899 against
// 818 and reporting not found. The single-link fallback stays disabled for
// marker queries, so the rank itself must carry the number-free identity
// check.
func TestSearchRawAICIDQueryMatchesDisplayListing(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage("DV-818AI"),
		"https://javdb.test/v/dv818ai":                remasterDetailPage("DV-818AI"),
	})
	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err, "the correct display listing must satisfy the raw AI cid's folded identity")
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID)
}

// Multi-result ordering: the number-free acceptance must still run the
// loop's best-match wiring — a foreign-series AI listing ahead of the
// correct one ranks none, and the raw AI cid query resolves through the
// same-series display listing behind it rather than missing honestly.
func TestSearchRawAICIDQuerySkipsForeignListingAndMatches(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage("IPX-100AI", "DV-818AI"),
		"https://javdb.test/v/dv818ai":                remasterDetailPage("DV-818AI"),
	})
	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err, "the same-series AI display listing behind a foreign one must resolve")
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID)
}

// The number-exemption decision, pinned: the slot number carries no
// display information, so the number-free acceptance admits a neighboring
// same-series AI display listing from a raw cid query too — the honest
// reading that mirrors the number-free semantics the DMM/R18 identity code
// applies everywhere else (any same-series AI display is equally the
// strongest available comparison). Display AI queries still number-bind.
func TestSearchRawAICIDQueryAcceptsNeighboringSameSeriesAIListing(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage("DV-819AI"),
		"https://javdb.test/v/dv819ai":                remasterDetailPage("DV-819AI"),
	})
	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err, "a same-series AI display listing satisfies the number-free identity")
	require.NotNil(t, res)
	assert.Equal(t, "DV-819AI", res.ID)
}

// The exemption never widens past the identity: a raw AI cid query still
// rejects listings of a different series, marker class or catalog suffix,
// and the single-link fallback stays disabled so the wrong detail page is
// never returned.
func TestSearchRawAICIDQueryRejectsForeignIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listed string
	}{
		{"foreign series", "IPX-818AI"},
		{"foreign marker class", "DV-818H"},
		{"foreign catalog suffix", "DV-818-E-AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=dv00899ai&f=all":                                                       remasterSearchPage(tc.listed),
				"https://javdb.test/v/" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tc.listed)): remasterDetailPage(tc.listed),
			})
			_, err := s.Search(context.Background(), "dv00899ai")
			require.Error(t, err, "%s must not satisfy the raw AI cid's identity", tc.listed)
			scraperErr, ok := models.AsScraperError(err)
			require.True(t, ok)
			assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
		})
	}
}

// The H/HD number binding survives the AI exemption: a raw H cid query
// numbering a different release keeps missing honestly (the round-41 rule
// the fold comparison enforces for H/HD spellings — 1rct00157h must not
// match RCT-156-HD).
func TestSearchRawHCIDQueryKeepsNumberBinding(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=1rct00157h&f=all": remasterSearchPage("RCT-156-HD"),
		"https://javdb.test/v/rct156hd":                remasterDetailPage("RCT-156-HD"),
	})
	_, err := s.Search(context.Background(), "1rct00157h")
	require.Error(t, err, "a neighboring number is a different release: the AI exemption never applies to H/HD cids")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}
