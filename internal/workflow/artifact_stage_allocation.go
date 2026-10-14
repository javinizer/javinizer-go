package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/afero"
)

func allocateArtifactStage(fs afero.Fs, finalRoot string) (string, error) {
	parent := filepath.Dir(finalRoot)
	sweepArtifactStaging(fs, parent)
	var root string
	var err error
	if err = fs.MkdirAll(parent, 0o755); err != nil {
		err = fmt.Errorf("create artifact staging parent: %w", err)
	} else if root, err = afero.TempDir(fs, parent, artifactStageDirPrefix); err != nil {
		err = fmt.Errorf("create artifact staging area: %w", err)
	}
	if err == nil {
		return root, nil
	}
	if (!errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EROFS)) || filepath.Clean(finalRoot) == filepath.Clean(parent) {
		return "", err
	}
	if mkdirErr := fs.MkdirAll(finalRoot, 0o755); mkdirErr != nil {
		return "", errors.Join(err, fmt.Errorf("create artifact staging destination: %w", mkdirErr))
	}
	sweepArtifactStaging(fs, finalRoot)
	root, fallbackErr := afero.TempDir(fs, finalRoot, artifactStageDirPrefix)
	if fallbackErr != nil {
		return "", errors.Join(err, fmt.Errorf("create artifact staging area within destination: %w", fallbackErr))
	}
	return root, nil
}
