package artifactstore

import (
	"strings"
	"testing"
)

func TestDigestBoundsBeforeDecode(t *testing.T) {
	for _, test := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", false}, {"short", strings.Repeat("a", 63), false}, {"long", strings.Repeat("a", 65), false},
		{"lowercase", strings.Repeat("ab", 32), true}, {"digits", strings.Repeat("09", 32), true},
		{"uppercase", strings.Repeat("AB", 32), false}, {"invalid", strings.Repeat("a", 63) + "g", false},
		{"oversized-hex", strings.Repeat("ab", 1<<20), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validDigest(test.value); got != test.valid {
				t.Fatalf("valid=%v want%v", got, test.valid)
			}
		})
	}
	// The oversized valid-hex input is constructed once, outside measurement.
	// Rejection must not allocate a decoded copy proportional to caller input.
	oversized := strings.Repeat("ab", 1<<20)
	if allocations := testing.AllocsPerRun(20, func() {
		if validDigest(oversized) {
			panic("oversized digest accepted")
		}
	}); allocations != 0 {
		t.Fatalf("oversized rejection allocated: %v", allocations)
	}
}
