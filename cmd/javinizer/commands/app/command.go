package app

import (
	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/spf13/cobra"
)

// NewCommand creates the `app` subcommand, which launches the desktop GUI
// (a native window over the embedded API server + Web UI). It is a thin
// wrapper around desktop.Run; the heavy lifting lives in internal/desktop.
//
// In a normal CLI build (without the `desktop` tag), desktop.Run returns an
// error explaining that desktop mode is not built. In a desktop build
// (`-tags desktop -X internal/desktop.BuildDesktop=1`), it opens the window.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Launch the Javinizer desktop app",
		Long: `Launch the Javinizer desktop app: a native window (via Wails) over the
embedded API server and Web UI. All CLI and TUI subcommands remain available.

This command only works in desktop builds. In a CLI build it prints an error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			configFile, _ := cmd.Flags().GetString("config")
			return desktop.Run(desktop.Options{ConfigFile: configFile})
		},
	}

	return cmd
}
