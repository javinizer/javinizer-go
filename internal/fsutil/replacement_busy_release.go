package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/afero"
)

const replacementBusyLiveSuffix = ",released=0"

// markReplacementBusyReleased records irrevocable explicit release when a
// Windows reader prevents the ordinary take-aside. Only newly generated tokens
// have the fixed slot: legacy markers remain read-compatible but are never
// extended or truncated. One byte is either unwritten or the complete status1,
// so a short write cannot leave an ambiguous appended status prefix.
//
// The original filesystem supplies the nontruncating handle. Both the exact
// token and strong creation identity are proved through that handle; the final
// metadata check refuses a change observed during the read. A legitimate new
// claimant always creates a new object with O_EXCL. This does not promise to
// preserve arbitrary hostile concurrent edits to the *same* owned inode.
//
// visible means WriteAt reported that the status byte was written, NOT that it
// is durable. Sync/Close errors are returned even when visible is true. Such a
// complete release may already be seen by other processes and is safe because
// invocation of the owner's release capability has ended its authority.
func markReplacementBusyReleased(fs afero.Fs, path, token string, claimed *BoundInstallIdentity) (visible bool, err error) {
	if !strings.HasSuffix(token, replacementBusyLiveSuffix) {
		return false, fmt.Errorf("replacement busy token has no fixed release slot")
	}
	if claimed == nil {
		return false, fmt.Errorf("replacement busy release requires creation identity")
	}
	device, inode, strong := claimed.strongIdentity()
	if !strong {
		return false, fmt.Errorf("replacement busy release requires strong creation identity")
	}
	handle, err := fs.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("open owned busy marker for release: %w", err)
	}
	defer func() {
		if closeErr := handle.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close released busy marker: %w", closeErr))
		}
	}()
	info, err := handle.Stat()
	if err != nil {
		return false, fmt.Errorf("stat owned busy marker for release: %w", err)
	}
	// Windows may finalize the original write timestamp after its handle closes.
	// Rebind only metadata, never the creation-bound kernel identity; exact bytes
	// are still required below before any write.
	observed := newBoundInstallIdentity(info, device, inode, true)
	if err := VerifiedSourceProofFromBoundInstallIdentity(fs, observed, handle)(path, info); err != nil {
		return false, fmt.Errorf("prove owned busy marker for release: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(handle, int64(len(token)+1)))
	if err != nil {
		return false, fmt.Errorf("read owned busy marker for release: %w", err)
	}
	if string(content) != token {
		return false, fmt.Errorf("owned busy marker token changed before release")
	}
	current, err := handle.Stat()
	if err != nil {
		return false, fmt.Errorf("recheck owned busy marker for release: %w", err)
	}
	if err := VerifiedSourceProofFromBoundInstallIdentity(fs, observed, handle)(path, current); err != nil {
		return false, fmt.Errorf("recheck owned busy marker identity for release: %w", err)
	}
	n, writeErr := handle.WriteAt([]byte{'1'}, int64(len(token)-1))
	if n != 1 {
		return false, fmt.Errorf("write busy marker release status: %w", errors.Join(io.ErrShortWrite, writeErr))
	}
	visible = true
	if writeErr != nil {
		err = fmt.Errorf("write busy marker release status: %w", writeErr)
	}
	if syncErr := handle.Sync(); syncErr != nil {
		err = errors.Join(err, fmt.Errorf("sync released busy marker: %w", syncErr))
	}
	return visible, err
}
