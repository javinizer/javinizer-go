//go:build windows

package workflow

import "os"

const artifactSweepKeyReadFlags = 0

// secureArtifactSweepSecret is unset on Windows: there is no POSIX owner/mode
// model to enforce. Privacy still depends on the deployment keeping the cache
// private with its ACL; this hook does not verify Windows ACLs.
var secureArtifactSweepSecret func(path string, info os.FileInfo) error
