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
			return staged, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("exclusive staging names exhausted for %s%s after %d attempts", dest, suffix, exclusiveStagingAttempts)
}
