package history

// --- Additional miss-line coverage for reverter.go ---
// Focuses on: in-place renamed with sourcePath != OriginalPath (file rename within dir),
// canonicalize error in MkdirAll, NFO paths with empty NFOPath and MovieID,
// RevertBatch all-already-reverted, RevertScrape no-matching-ops

// After dir rename back, the file still needs a rename within the restored dir.
