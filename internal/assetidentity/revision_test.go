package assetidentity

import (
	"testing"
)

func TestFromBytesRevisionIsStableAndJavaScriptSafe(t *testing.T) {
	first := FromBytes([]byte("poster-a"))
	same := FromBytes([]byte("poster-a"))
	different := FromBytes([]byte("poster-b"))
	if first != same {
		t.Fatalf("same bytes must have the same identity: %#v != %#v", first, same)
	}
	if first.Fingerprint == different.Fingerprint {
		t.Fatal("different bytes must have different fingerprints")
	}
	if first.Revision >= 1<<53 || different.Revision >= 1<<53 {
		t.Fatalf("revision must fit JavaScript safe integers: %d %d", first.Revision, different.Revision)
	}
}

func TestValidFingerprint(t *testing.T) {
	if !ValidFingerprint("") {
		t.Fatal("empty fingerprint is the legacy-compatible absent value")
	}
	if !ValidFingerprint(FromBytes([]byte("x")).Fingerprint) {
		t.Fatal("a SHA-256 digest must validate")
	}
	for _, value := range []string{"short", "zz" + string(make([]byte, 62)), "0"} {
		if ValidFingerprint(value) {
			t.Fatalf("malformed fingerprint validated: %q", value)
		}
	}
}
