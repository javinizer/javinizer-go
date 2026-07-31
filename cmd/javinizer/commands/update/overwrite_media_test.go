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
