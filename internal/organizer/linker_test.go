package organizer

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemLinker_Symlink(t *testing.T) {
	m := &MemLinker{}
	err := m.symlink("/old", "/new")
	require.NoError(t, err)
	assert.Len(t, m.Links, 1)
	assert.Equal(t, "soft", m.Links[0].Kind)
	assert.Equal(t, "/old", m.Links[0].OldName)
	assert.Equal(t, "/new", m.Links[0].NewName)
}

func TestMemLinker_Hardlink(t *testing.T) {
	m := &MemLinker{}
	err := m.hardlink("/old", "/new")
	require.NoError(t, err)
	assert.Len(t, m.Links, 1)
	assert.Equal(t, "hard", m.Links[0].Kind)
	assert.Equal(t, "/old", m.Links[0].OldName)
	assert.Equal(t, "/new", m.Links[0].NewName)
}

func TestMemLinker_MultipleLinks(t *testing.T) {
	m := &MemLinker{}
	_ = m.symlink("/a", "/b")
	_ = m.hardlink("/c", "/d")
	assert.Len(t, m.Links, 2)
	assert.Equal(t, "soft", m.Links[0].Kind)
	assert.Equal(t, "hard", m.Links[1].Kind)
}

func TestMemLinker_CopyFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("hello"), 0644))

	m := &MemLinker{}
	err := m.copyFile(fs, "/src.txt", "/dst.txt")
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestOSLinker_Symlink(t *testing.T) {
	// Just verify the function exists and has correct type signature
	var l linker = OSLinker{}
	assert.NotNil(t, l)
}

func TestOSLinker_Hardlink(t *testing.T) {
	var l linker = OSLinker{}
	assert.NotNil(t, l)
}
