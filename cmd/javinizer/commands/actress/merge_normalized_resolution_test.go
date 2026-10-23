package actress

import (
	"bytes"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"strconv"
	"strings"
	"testing"
)

func TestAuditExplicitNormalizedResolution(t *testing.T) {
	path, _ := setupMissTestDB(t)
	target := models.Actress{FirstName: "Target"}
	source := models.Actress{FirstName: "Source"}
	seedMissActresses(t, path, &target, &source)
	for i := 0; i < 40; i++ {
		root := &cobra.Command{Use: "test"}
		root.PersistentFlags().String("config", path, "")
		root.AddCommand(NewCommand())
		root.SetArgs([]string{"actress", "merge", "--target", strconv.Itoa(int(target.ID)), "--source", strconv.Itoa(int(source.ID)), "--non-interactive", "--dry-run", "--resolve", "FIRST_NAME=source"})
		out := &bytes.Buffer{}
		root.SetOut(out)
		require.NoError(t, root.Execute())
		if !strings.Contains(out.String(), `first_name: target="Target" source="Source" result="Source"`) {
			t.Fatalf("explicit decision lost on iteration %d: %s", i, out.String())
		}
	}
}

func TestCLIMergeRejectsContradictoryNormalizedDecisions(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(NewCommand())
	root.SetArgs([]string{"actress", "merge", "--target", "1", "--source", "2", "--non-interactive", "--dry-run", "--resolve", "FIRST_NAME=source,first_name=target"})
	require.ErrorContains(t, root.Execute(), "contradictory decisions")
}
