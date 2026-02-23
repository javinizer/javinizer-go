package version_test

import (
	"bytes"
	"strings"
	"testing"

	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand_Default(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	origBuildDate := appversion.BuildDate
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
		appversion.BuildDate = origBuildDate
	}()

	appversion.Version = "v1.2.3"
	appversion.Commit = "abcdef123456"
	appversion.BuildDate = "2026-02-23T00:00:00Z"

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(out.String())
	assert.Contains(t, output, "javinizer v1.2.3")
	assert.Contains(t, output, "commit: abcdef123456")
	assert.Contains(t, output, "built: 2026-02-23T00:00:00Z")
}

func TestVersionCommand_Short(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
	}()

	appversion.Version = "dev"
	appversion.Commit = "abcdef123456"

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--short"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(out.String())
	assert.Equal(t, appversion.Short(), output)
}
