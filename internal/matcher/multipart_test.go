package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectPartSuffix(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		wantNum     int
		wantSuf     string
		wantPattern string
	}{
		// Explicit patterns - always multipart
		{"IPX-535-pt1", "IPX-535", 1, "-pt1", PatternExplicit},
		{"IPX-535PT2", "IPX-535", 2, "-pt2", PatternExplicit},
		{"IPX-535-part1", "IPX-535", 1, "-part1", PatternExplicit},
		{"IPX-535part2", "IPX-535", 2, "-part2", PatternExplicit},
		{"IPX-535 pt1", "IPX-535", 1, "-pt1", PatternExplicit},
		{"IPX-535_part3", "IPX-535", 3, "-part3", PatternExplicit},
		{"PRED-151-1", "PRED-151", 1, "-1", PatternExplicit},
		{"PRED-151-2", "PRED-151", 2, "-2", PatternExplicit},

		// Disc parts: cd/disc/disk + 1-2 digits - EXPLICIT
		{"ABC-123-CD1", "ABC-123", 1, "-cd1", PatternExplicit},
		{"ABC-123-cd2", "ABC-123", 2, "-cd2", PatternExplicit},
		{"ABC-123-CD3", "ABC-123", 3, "-cd3", PatternExplicit},
		{"ABC-123-CD4", "ABC-123", 4, "-cd4", PatternExplicit},
		{"ABC-123-CD5", "ABC-123", 5, "-cd5", PatternExplicit},
		{"ABC-123-CD6", "ABC-123", 6, "-cd6", PatternExplicit},
		{"ABC-123-CD7", "ABC-123", 7, "-cd7", PatternExplicit},
		{"IPX-535_CD2", "IPX-535", 2, "-cd2", PatternExplicit},
		{"IPX-535.disc2", "IPX-535", 2, "-disc2", PatternExplicit},
		{"IPX-535-disk3", "IPX-535", 3, "-disk3", PatternExplicit},
		{"IPX-535 CD1", "IPX-535", 1, "-cd1", PatternExplicit},
		{"IPX-535-cd1-4k", "IPX-535", 1, "-cd1", PatternExplicit},
		{"IPX-535-disc-1", "IPX-535", 1, "-disc1", PatternExplicit},
		{"IPX-535-cd12", "IPX-535", 12, "-cd12", PatternExplicit},
		{"IPX-535cd1", "IPX-535", 1, "-cd1", PatternExplicit},
		{"IPX-535CD2", "IPX-535", 2, "-cd2", PatternExplicit},

		// Ambiguous letter patterns - need directory context validation
		{"MDB-087A", "MDB-087", 1, "-A", PatternLetter},
		{"MDB-087-b", "MDB-087", 2, "-B", PatternLetter},
		{"ABP-123c", "ABP-123", 3, "-C", PatternLetter},
		{"IPX-535-D", "IPX-535", 4, "-D", PatternLetter},
		{"IPX-535-Z", "IPX-535", 26, "-Z", PatternLetter},
		{"ABW-121-C", "ABW-121", 3, "-C", PatternLetter}, // Chinese subtitle case

		// Letter + trailing content (quality/resolution tag) - ambiguous, need directory validation
		{"SVFLA-001a-4k", "SVFLA-001", 1, "-A", PatternLetter},
		{"SVFLA-001b-1080p", "SVFLA-001", 2, "-B", PatternLetter},
		{"IPX-535a-4k-60", "IPX-535", 1, "-A", PatternLetter},
		{"IPX-535b-4k-60", "IPX-535", 2, "-B", PatternLetter},
		{"IPX-535-C-1", "IPX-535", 1, "-1", PatternTrailing},
		{"IPX-535-C-2", "IPX-535", 2, "-2", PatternTrailing},
		{"SVFLA-001a-4k-HDR", "SVFLA-001", 1, "-A", PatternLetter},
		{"SVFLA-001b-4k-h265", "SVFLA-001", 2, "-B", PatternLetter},
		{"SVFLA-001a-1080", "SVFLA-001", 1, "-A", PatternLetter},
		{"SVFLA-001b-1080", "SVFLA-001", 2, "-B", PatternLetter},
		{"SVFLA-001a-[4k]", "SVFLA-001", 1, "-A", PatternLetter},
		{"SVFLA-001b-[4k]", "SVFLA-001", 2, "-B", PatternLetter},

		// No pattern
		{"ABC-123", "ABC-123", 0, "", PatternNone},
		{"IPX-535 no suffix", "IPX-535", 0, "", PatternNone},
		{"IPX-535-4k", "IPX-535", 0, "", PatternNone},
		{"IPX-535-1080p", "IPX-535", 0, "", PatternNone},
		{"IPX-535-FHD", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cd", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cdx", "IPX-535", 0, "", PatternNone},
		{"IPX-535-disc", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cdk1", "IPX-535", 0, "", PatternNone},
		{"IPX-535-discz", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cd1x", "IPX-535", 0, "", PatternNone},
		{"IPX-535-hdcd1", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cd0", "IPX-535", 0, "", PatternNone},
		{"IPX-535-cd100", "IPX-535", 0, "", PatternNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, suf, pattern, _ := DetectPartSuffix(tt.name, tt.id)
			assert.Equal(t, tt.wantNum, num, "PartNumber mismatch")
			assert.Equal(t, tt.wantSuf, suf, "PartSuffix mismatch")
			assert.Equal(t, tt.wantPattern, pattern, "PatternType mismatch")
		})
	}
}

func TestLetterPart_OutOfRange(t *testing.T) {
	tests := []struct {
		name   string
		letter string
		ok     bool
	}{
		{"digit zero", "0", false},
		{"digit nine", "9", false},
		{"symbol at", "@", false},
		{"lowercase a", "a", true},
		{"lowercase z", "z", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, suf, ok := letterPart(tt.letter)
			assert.Equal(t, tt.ok, ok)
			if !ok {
				assert.Equal(t, 0, n)
				assert.Equal(t, "", suf)
			}
		})
	}
}

func TestDetectPartSuffix_PatternConstants(t *testing.T) {
	// Verify pattern constants are correct
	assert.Equal(t, "explicit", PatternExplicit)
	assert.Equal(t, "letter", PatternLetter)
	assert.Equal(t, "", PatternNone)
}
