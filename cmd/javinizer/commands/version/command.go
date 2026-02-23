package version

import (
	"fmt"

	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

// NewCommand creates the version command.
func NewCommand() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show build and release version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if short {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), appversion.Short())
				return err
			}

			_, err := fmt.Fprintln(cmd.OutOrStdout(), appversion.Info())
			return err
		},
	}

	cmd.Flags().BoolVarP(&short, "short", "s", false, "show only the short version")
	return cmd
}
