package r18devdump

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func indexOfString(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// TestContentIDCandidates_Order pins the canonical-first ordering that both the
// dump lookup and the r18dev HTTP resolver must agree on (see
// fix-r18dev-dump-null-dvdid-lookup spec parity scenario).
func TestContentIDCandidates_Order(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string // expected leading candidates, in order
		mustNot []string
	}{
		{
			name:  "LULU-441 zero-padded digital first, mono after",
			input: "LULU-441",
			want:  []string{"lulu00441", "lulu441"},
		},
		{
			name:  "ABF-030 canonical prefix wins over compilation prefix",
			input: "ABF-030",
			want:  []string{"118abf00030", "118abf030", "436abf00030", "436abf030"},
		},
		{
			name:  "SAN-457 118 prefix leads, PPV underscore prefixes last",
			input: "SAN-457",
			want:  []string{"118san00457", "118san457"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContentIDCandidates(tt.input)
			if len(got) < len(tt.want) {
				t.Fatalf("ContentIDCandidates(%q) = %v, too few", tt.input, got)
			}
			for i, w := range tt.want {
				assert.Equal(t, w, got[i], "candidate %d", i)
			}
		})
	}
}

func TestContentIDCandidates_ContainsPPVPrefix(t *testing.T) {
	got := ContentIDCandidates("SAN-457")
	std := indexOfString(got, "118san00457")
	ppv := indexOfString(got, "h_796san00457")
	if ppv < 0 {
		t.Fatalf("ContentIDCandidates(SAN-457) = %v, missing PPV candidate", got)
	}
	assert.Greater(t, ppv, std, "PPV candidates come after standard prefixes")
}

func TestContentIDCandidates_RejectsEmptyAndUnsplitable(t *testing.T) {
	assert.Nil(t, ContentIDCandidates(""))
	assert.Nil(t, ContentIDCandidates("   "))
	assert.Nil(t, ContentIDCandidates("abc"))
	assert.Nil(t, ContentIDCandidates("123"))
}

func TestContentIDCandidates_AccentInsensitiveDirectContentID(t *testing.T) {
	// Direct content-id-style input stays in the candidate set so a
	// content_id query can resolve a present-without-dvd_id row.
	got := ContentIDCandidates("lulu00441")
	assert.Equal(t, "lulu00441", got[0])
}

func TestContentIDCandidates_ContentIDShapedIdentityLeads(t *testing.T) {
	// A content-id-shaped input honors its own row FIRST: searching an exact
	// prefixed content_id must not reorder it behind canonical variants
	// (Codex review P0: "436abf00030" must not resolve to 118abf00030's row).
	for _, in := range []string{"118ipx00535", "436abf00030", "lulu00441"} {
		got := ContentIDCandidates(in)
		assert.Equal(t, in, got[0], "content-id-shaped input %q must lead its own candidate list", in)
	}
	// 436abf00030 still keeps the canonical product in the list for parity.
	got := ContentIDCandidates("436abf00030")
	if indexOfString(got, "118abf00030") < 0 {
		t.Fatalf("canonical variant missing from 436abf00030 candidates: %v", got)
	}
}

func TestContentIDCandidates_HyphenatedPaddedDisplayIDKeepsCanonicalOrder(t *testing.T) {
	// Regression (Codex review round 3): ABF-00030 is a display id with an
	// already-zero-padded number — the normalized form begins with a letter and
	// must not be treated as content-id-shaped, or "abf00030" would outrank
	// the canonical 118-prefixed product.
	got := ContentIDCandidates("ABF-00030")
	assert.Equal(t, "118abf00030", got[0])
	assert.Equal(t, "118abf030", got[1])
	assert.Equal(t, "abf00030", got[len(got)-1], "identity variant trails canonical candidates")
}

func TestContentIDCandidates_DisplayIDKeepsGeneratedOrder(t *testing.T) {
	// Display ids keep generation order even though the identity form equals
	// one of the generated variants (deduped, not moved up front).
	got := ContentIDCandidates("LULU-441")
	assert.Equal(t, "lulu00441", got[0])
	assert.Equal(t, "lulu441", got[1])
}

func TestContentIDCandidates_WhitespaceTolerant(t *testing.T) {
	for _, in := range []string{" LULU-441 ", "LULU 441", "\tLULU-441\n"} {
		got := ContentIDCandidates(in)
		if len(got) == 0 || got[0] != "lulu00441" {
			t.Errorf("ContentIDCandidates(%q) = %v, want lulu00441 first", in, got)
		}
	}
}

func TestSplitSeriesAndNumber_AndClassifiers(t *testing.T) {
	assert.True(t, isAlpha("abcXYZ"))
	assert.False(t, isAlpha("ab1"))
	assert.False(t, isAlpha(""))
	assert.True(t, isDigit("0123"))
	assert.False(t, isDigit("12a"))
	assert.False(t, isDigit(""))

	// Dash form.
	s, n := SplitSeriesAndNumber("ABF-123")
	assert.Equal(t, "ABF", s)
	assert.Equal(t, "123", n)

	// Dash guard rejects, regex fallback also rejects.
	s, n = SplitSeriesAndNumber("1ABC-12")
	assert.Equal(t, "", s)
	assert.Equal(t, "", n)

	// Regex fallback path.
	s, n = SplitSeriesAndNumber("118abf00030")
	assert.Equal(t, "abf", s)
	assert.Equal(t, "00030", n)

	// No digits at all.
	s, n = SplitSeriesAndNumber("plainword")
	assert.Equal(t, "", s)
}

func TestContentIDCandidates_NumberOverflowRejected(t *testing.T) {
	assert.Nil(t, ContentIDCandidates("ABC-99999999999999999999"), "number exceeding int range must not expand")
}

func TestContentIDCandidates_UnderscorePrefixedContentID(t *testing.T) {
	assert.Equal(t, []string{"h_086mesu00103"}, ContentIDCandidates("h_086mesu00103"))
}
