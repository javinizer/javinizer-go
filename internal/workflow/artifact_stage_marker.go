package workflow

import (
	"fmt"
	"io"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func writeArtifactStageBoundMarker(fs afero.Fs, root, path string, body []byte, identity artifactStageDirIdentity) error {
	if !artifactStageDirMatches(fs, root, identity) {
		return fmt.Errorf("staging directory identity changed before marker creation %s", path)
	}
	temporary, file, err := fsutil.CreateExclusiveStagingFileModeBestEffort(fs, path, ".jv-stage.artifact-marker", 0, 0o600)
	if err != nil {
		return err
	}
	if !artifactStageDirMatches(fs, root, identity) {
		_ = file.Close()
		return fmt.Errorf("staging directory identity changed while opening marker %s", path)
	}
	written, err := file.Write(body)
	if err != nil {
		_ = file.Close()
		return err
	}
	if written != len(body) {
		_ = file.Close()
		return io.ErrShortWrite
	}
	if err := file.Close(); err != nil {
		return err
	}
	if !artifactStageDirMatches(fs, root, identity) {
		return fmt.Errorf("staging directory identity changed before marker publication %s", path)
	}
	return fs.Rename(temporary, path)
}
