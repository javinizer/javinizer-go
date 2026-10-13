package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func digestVerifiedInstall(fs afero.Fs, path string, installed *fsutil.BoundInstallIdentity) (string, error) {
	if installed != nil && installed.LinkTarget() != "" {
		return "", fmt.Errorf("installed object digest of %s requires a regular-file install", path)
	}
	if _, err := fsutil.ObserveVerifiedInstall(fs, path, installed); err != nil {
		return "", err
	}
	file, err := fs.Open(path)
	if err != nil {
		return "", fmt.Errorf("open installed object %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	proof := fsutil.VerifiedSourceProofFromBoundInstallIdentity(fs, installed, file)
	verify := func() error {
		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat installed object %s: %w", path, err)
		}
		return proof(path, info)
	}
	if err := verify(); err != nil {
		return "", err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("digest installed object %s: %w", path, err)
	}
	if err := verify(); err != nil {
		return "", err
	}
	if _, err := fsutil.ObserveVerifiedInstall(fs, path, installed); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
