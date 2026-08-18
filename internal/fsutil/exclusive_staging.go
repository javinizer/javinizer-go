package fsutil

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/afero"
)

const exclusiveStagingAttempts = 64

// CreateExclusiveStagingFile creates a destination-adjacent staging file and
// returns its chosen path and open handle. Callers provide the next process
// ordinal so the existing hexadecimal `.rstr.<n>`/`.dlrstr.<n>` naming grammar
// remains unchanged; these transient names are not `.dlbak` ownership markers
// consumed by replacement_sweep_p3.go.
func CreateExclusiveStagingFile(fs afero.Fs, dest, suffix string, start uint64, mode os.FileMode) (string, afero.File, error) {
	for attempt := uint64(0); attempt < exclusiveStagingAttempts; attempt++ {
		ordinal := start + attempt
		staged := dest + suffix + "." + strconv.FormatUint(ordinal, 16)
		file, err := fs.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err == nil {
			// The kernel narrows `mode` by the process umask at O_CREATE time
			// (e.g. 0666 becomes 0644 under umask 0077) and nothing else
			// re-applies it, so a restore staging for permissive media would
			// silently narrow its permission bits. Re-assert the exact
			// requested perms while the name is exclusively ours; see
			// replacements_posix.go / replacements_windows.go for the
			// strict-vs-best-effort split.
			if cerr := restoreStagingMode(fs, staged, mode.Perm()); cerr != nil {
				_ = file.Close()
				_ = fs.Remove(staged)
				return "", nil, fmt.Errorf("apply exclusive staging mode for %s: %w", staged, cerr)
			}
			return staged, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("exclusive staging names exhausted for %s%s after %d attempts", dest, suffix, exclusiveStagingAttempts)
}
