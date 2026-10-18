package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type openedProofOpaqueFile struct{ afero.File }
type openedProofOpaqueInfo struct{ os.FileInfo }

func (openedProofOpaqueInfo) Sys() interface{} { return nil }

func TestBoundInstallProofUsesOpenedDescriptor(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	own, foreign := filepath.Join(dir, "owned"), filepath.Join(dir, "foreign")
	require.NoError(t, afero.WriteFile(base, own, []byte("owneddata"), 0o600))
	require.NoError(t, afero.WriteFile(base, foreign, []byte("foreign!!"), 0o600))
	info, err := base.Stat(own)
	require.NoError(t, err)
	device, inode, strong := BoundObjectIdentity(base, own, info)
	if !strong {
		t.Skip("native strong identity unavailable")
	}
	installed := newBoundInstallIdentity(info, device, inode, strong)
	require.NoError(t, base.Chtimes(foreign, info.ModTime(), info.ModTime()))
	file, err := base.Open(own)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	current, err := file.Stat()
	require.NoError(t, err)
	require.NoError(t, VerifiedSourceProofFromBoundInstallIdentity(base, installed, file)(own, current))
	foreignFile, err := base.Open(foreign)
	require.NoError(t, err)
	defer func() { _ = foreignFile.Close() }()
	foreignInfo, err := foreignFile.Stat()
	require.NoError(t, err)
	require.Error(t, VerifiedSourceProofFromBoundInstallIdentity(base, installed, foreignFile)(own, foreignInfo))
	opaque := &openedProofOpaqueFile{File: file}
	err = VerifiedSourceProofFromBoundInstallIdentity(base, installed, opaque)(own, openedProofOpaqueInfo{FileInfo: current})
	require.ErrorIs(t, err, ErrPublishCompleted)
}
