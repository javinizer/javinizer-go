package workflow

import "testing"

// stagedSiblingIndex selects the first sibling whose bytes were staged: fault
// windows pinned to the staged publication plane. Multipart video siblings
// admitted under move-flow deferral carry no staged bytes (codex P2,
// PRRT_kwDORn9KaM6rLI28) and must not be addressed here.
func stagedSiblingIndex(t *testing.T, s *artifactStage) int {
	t.Helper()
	for i := range s.siblings {
		if !s.siblings[i].deferred {
			return i
		}
	}
	t.Fatal("expected at least one staged (non-deferred) sibling")
	return -1
}
