//go:build !windows

package fsutil

import (
	"errors"
	"testing"
)

// TestMoveFileNoReplaceVerifiedTakeCompletedClassStripped drives the
// take-completed strip scenario (assertVerifiedTakeCompletedClassStripped)
// through the POSIX hard-link take leg: forceVerifiedTakeFallbackLeg lands
// the take hop on publishNoReplaceFallback, and the wedged staged-unlink
// seam then makes the take report completed-with-residue — exactly the
// internal-hop shape the strip guard re-states. Windows links nothing (the
// take publishes through MoveFileEx), so no fixture exists there.
func TestMoveFileNoReplaceVerifiedTakeCompletedClassStripped(t *testing.T) {
	forceVerifiedTakeFallbackLeg(t)
	originalRemove := publishNoReplaceRemove
	publishNoReplaceRemove = func(name string) error { return errors.New("simulated unlink refusal") }
	t.Cleanup(func() { publishNoReplaceRemove = originalRemove })
	assertVerifiedTakeCompletedClassStripped(t)
}
