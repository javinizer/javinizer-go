package update_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverwriteExistingMediaFlag_DefaultsFalse(t *testing.T) {
	cmd := update.NewCommand()
	flag := cmd.Flags().Lookup("overwrite-existing-media")
	require.NotNil(t, flag)
	value, err := cmd.Flags().GetBool("overwrite-existing-media")
	require.NoError(t, err)
	assert.False(t, value)
}

func TestRun_RejectsOverwriteWhenDownloadsDisabled(t *testing.T) {
	cmd := update.NewCommand()
	require.NoError(t, cmd.Flags().Set("overwrite-existing-media", "true"))
	require.NoError(t, cmd.Flags().Set("download", "false"))

	err := update.Run(cmd, []string{t.TempDir()}, "")
	assert.EqualError(t, err, "--overwrite-existing-media requires --download=true")
}
