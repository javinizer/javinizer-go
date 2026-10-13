package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type artifactMarkerFailureFS struct {
	afero.Fs
	root string
	mode string
}

func (f *artifactMarkerFailureFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(path, flag, mode)
	if err == nil && strings.Contains(path, ".jv-stage.artifact-marker.") {
		return &artifactMarkerFailureFile{File: file, fs: f}, nil
	}
	return file, err
}

type artifactMarkerFailureFile struct {
	afero.File
	fs *artifactMarkerFailureFS
}

func (f *artifactMarkerFailureFile) Write(body []byte) (int, error) {
	switch f.fs.mode {
	case "write error":
		n, _ := f.File.Write(body[:len(body)/2])
		return n, errors.New("marker write denied")
	case "short write":
		return f.File.Write(body[:len(body)-1])
	}
	return f.File.Write(body)
}

func (f *artifactMarkerFailureFile) Close() error {
	if err := f.File.Close(); err != nil {
		return err
	}
	switch f.fs.mode {
	case "close error":
		return errors.New("marker close denied")
	case "root replaced after write":
		info, err := f.fs.Fs.Stat(f.fs.root)
		if err != nil {
			return err
		}
		if err := f.fs.Fs.Rename(f.fs.root, f.fs.root+".retained"); err != nil {
			return err
		}
		if err := f.fs.Fs.MkdirAll(f.fs.root, 0o700); err != nil {
			return err
		}
		stamp := info.ModTime().Add(time.Second)
		return f.fs.Fs.Chtimes(f.fs.root, stamp, stamp)
	}
	return nil
}

func TestArtifactStageBoundMarkerFailureRetainsOriginal(t *testing.T) {
	for _, failure := range []string{"unknown identity", "write error", "short write", "close error", "root replaced after write"} {
		t.Run(failure, func(t *testing.T) {
			base := afero.NewMemMapFs()
			root := "/stage"
			require.NoError(t, base.MkdirAll(root, 0o700))
			path := filepath.Join(root, artifactStageManifestName)
			require.NoError(t, afero.WriteFile(base, path, []byte("original marker"), 0o600))
			identity := artifactStageDirectoryIdentity(base, root)
			if failure == "unknown identity" {
				identity = artifactStageDirIdentity{}
			}
			fs := &artifactMarkerFailureFS{Fs: base, root: root, mode: failure}
			err := writeArtifactStageBoundMarker(fs, root, path, []byte("replacement marker"), identity)
			require.Error(t, err)
			if failure == "root replaced after write" {
				path = filepath.Join(root+".retained", artifactStageManifestName)
			}
			body, err := afero.ReadFile(base, path)
			require.NoError(t, err)
			assert.Equal(t, "original marker", string(body))
		})
	}
}
