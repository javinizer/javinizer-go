package fsutil

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

// CopyFileFs copies a file within the afero filesystem, creating destination directories as needed.
func CopyFileFs(fs afero.Fs, src, dst string) error {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}

	return copyFileDataFs(fs, src, dst)
}

// MoveFileFs moves a file within the afero filesystem, falling back to copy-and-remove across devices.
func MoveFileFs(fs afero.Fs, src, dst string) error {
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}

	err := fs.Rename(src, dst)
	if err == nil {
		return nil
	}

	if !isCrossDeviceError(err) {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return crossDeviceMoveFs(fs, src, dst)
}

func crossDeviceMoveFs(fs afero.Fs, src, dst string) error {
	if err := copyFileDataFs(fs, src, dst); err != nil {
		// The copy leg never writes to dst directly (staging only), so there is
		// NOTHING of ours at dst to "clean up": removing it could delete a
		// pre-existing foreign file (#224). Keep both, surface the failure.
		return fmt.Errorf("failed to copy file across devices: %w", err)
	}

	if err := fs.Remove(src); err != nil {
		// dst was fully published via the bound replace; the source remove is
		// the only failed step — keep BOTH objects rather than deleting the
		// published destination, and surface the ambiguity (#224).
		return fmt.Errorf("failed to remove source after cross-device copy: %w", err)
	}

	return nil
}

func copyFileDataFs(fs afero.Fs, src, dst string) error {
	srcFile, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// Stage dest-adjacent with O_EXCL (a predictable O_TRUNC name could
	// truncate a racing peer's staged bytes), stream through the open handle,
	// and publish through the bound discipline — a substitute planted on the
	// staged name is never published nor unbound-removed (#224). Publish keeps
	// REPLACE semantics (authorized overwrite); no-clobber uses the separate
	// NoReplace composites.
	staged, handle, sErr := CreateExclusiveStagingFile(fs, dst, ".mvstg", noreplaceOrdinal.Add(1), stagingFileMode())
	if sErr != nil {
		return fmt.Errorf("failed to create destination: %w", sErr)
	}

	if _, err := io.Copy(handle, srcFile); err != nil {
		DiscardFailedExclusiveStaging(fs, staged, handle)
		return fmt.Errorf("failed to copy data: %w", err)
	}

	stagedIdentity := stagingIdentity(handle)

	p := StagedPublish{
		FS:          fs,
		Publish:     ReplaceFile,
		Staged:      staged,
		Handle:      handle,
		Dest:        dst,
		Suffix:      ".mvstg",
		NextOrdinal: nextNoReplaceOrdinal,
	}
	if err := PublishStagedBound(p); err != nil {
		// Same discard discipline as the no-replace composites: the old
		// implementation removed its temp on failure; keep that cleanliness
		// without ever deleting a possibly-foreign staged name.
		discardStagedAfterFailedPublish(fs, staged, stagedIdentity, err)
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return nil
}
