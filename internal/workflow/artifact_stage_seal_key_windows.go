//go:build windows

package workflow

import "os"

const artifactSweepKeyNoFollow = 0

// secureArtifactSweepSecret has no POSIX owner/mode model to enforce on
// Windows: the key lives in the per-user profile, protected by its ACL.
func secureArtifactSweepSecret(string, *os.File, os.FileInfo) error { return nil }
