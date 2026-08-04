package legacycsvsource

import "testing"

func TestSourceName(t *testing.T) {
	s := New()
	if s.Name() != "legacy-jvthumbs" {
		t.Fatalf("expected 'legacy-jvthumbs', got %q", s.Name())
	}
}
