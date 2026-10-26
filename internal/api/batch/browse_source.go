package batch

import (
	"path/filepath"
	"strings"
)

// batchBrowseSourcePath is an optional return-location hint, not a browse access
// grant or existence guarantee. Only a complete set of original source files
// with one exact native parent is unambiguous. Do not infer a destination or a common ancestor for
// legacy or multi-directory jobs. The normal browse endpoint still validates
// access and resolves the returned path.
func batchBrowseSourcePath(files []string, totalFiles int) string {
	if totalFiles <= 0 || len(files) != totalFiles {
		return ""
	}
	parent := ""
	for _, source := range files {
		if !filepath.IsAbs(source) || filepath.Clean(source) != source || strings.ContainsRune(source, '\x00') {
			return ""
		}
		dir := filepath.Dir(source)
		// A filesystem root is not a useful, narrowly scoped return location.
		if filepath.Dir(dir) == dir || (parent != "" && dir != parent) {
			return ""
		}
		parent = dir
	}
	// Pure derivation: polling must not issue filesystem I/O against a NAS.
	// The client validates through the normal browse endpoint before handing
	// this candidate to Browse, falling back if it is missing or inaccessible.
	return parent
}
