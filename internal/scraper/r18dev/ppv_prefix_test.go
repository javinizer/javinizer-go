package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

func TestGenerateContentIDVariations_PPVPrefix_SAN457(t *testing.T) {
	vars := r18devdump.ContentIDCandidates("SAN-457")
	want := "h_796san00457"
	found := false
	for _, v := range vars {
		if v == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("r18devdump.ContentIDCandidates(SAN-457) = %v, missing PPV content_id %q (h_796 prefix dropped)", vars, want)
	}
	t.Logf("SAN-457 variations: %v", vars)
}

func TestContentIDPrefixLookup_HasPPVPrefixes(t *testing.T) {
	prefixes, ok := r18devdump.ContentIDPrefixLookup["san"]
	if !ok {
		t.Fatal("san series missing from prefix lookup")
	}
	wantPrefix := "h_796"
	found := false
	for _, p := range prefixes {
		if p == wantPrefix {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("r18devdump.ContentIDPrefixLookup[\"san\"] = %v, missing PPV prefix %q", prefixes, wantPrefix)
	}
}

func TestContentIDToID_PPVUnderscore(t *testing.T) {
	got := contentIDToID("h_796san00457")
	if got != "SAN-457" {
		t.Fatalf("contentIDToID(h_796san00457) = %q, want SAN-457", got)
	}
}
