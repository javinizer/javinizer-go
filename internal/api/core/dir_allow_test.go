package core

import (
	"github.com/spf13/afero"
)

// isDirAllowed is a test helper that delegates to PathValidator.IsDirAllowed.
// This replaces the previous local copy that duplicated the allowlist/denylist logic.
func isDirAllowed(dir string, allow, deny []string) bool {
	v := NewPathValidator(afero.NewOsFs(), allow, deny)
	return v.IsDirAllowed(dir)
}

// canonicalizePath is a test helper that delegates to PathValidator.canonicalizePath
// using the OS filesystem. This replaces the previous local copy that was in
// dir_allow_test.go.
func canonicalizePath(absPath string) (string, error) {
	v := NewPathValidator(afero.NewOsFs(), nil, nil)
	return v.canonicalizePath(absPath)
}
