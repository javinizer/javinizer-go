//go:build windows

package workflow

import "os"

const artifactSweepKeyNoFollow = 0

// secureArtifactSweepSecret is unset on Windows: there is no POSIX owner/mode
// model to enforce, and the key lives in the per-user profile protected by its
// ACL.
var secureArtifactSweepSecret func(path string, f *os.File, info os.FileInfo) error
