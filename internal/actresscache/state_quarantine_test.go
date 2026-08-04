package actresscache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenStateQuarantineRenameFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("corrupt-mid-line\n{}\n"), 0o600))
	original := stateRename
	defer func() { stateRename = original }()
	sentinel := errors.New("rename denied")
	stateRename = func(string, string) error { return sentinel }
	_, err := openState(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.True(t, strings.Contains(err.Error(), "quarantine"))
}
