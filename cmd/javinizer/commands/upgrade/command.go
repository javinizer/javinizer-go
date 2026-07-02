package upgrade

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
)

// runUpgrade is the upgrade implementation. It is a package-level variable so
// tests can swap it for a stub, covering the command's RunE branches without
// touching the network. Production calls the real update.Upgrade.
var runUpgrade = update.Upgrade

// SetRunUpgrade swaps the upgrade implementation and returns the previous one,
// so callers can restore it with `defer restore := SetRunUpgrade(stub); defer SetRunUpgrade(restore)`.
// Intended for tests; production code should not call it.
func SetRunUpgrade(fn func(context.Context, update.UpgradeOptions) (*update.UpgradeResult, error)) func(context.Context, update.UpgradeOptions) (*update.UpgradeResult, error) {
	prev := runUpgrade
	runUpgrade = fn
	return prev
}

// NewCommand creates the upgrade subcommand that self-updates the javinizer
// binary to the latest GitHub release.
func NewCommand() *cobra.Command {
	var check bool
	var force bool
	var prerelease bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the javinizer binary to the latest release",
		Long: `Download and install the latest javinizer release from GitHub, replacing the
running binary in place. The asset for the current OS/arch is verified against
the release checksums before the swap.

By default only the latest stable release is considered. Use --prerelease to
upgrade to the newest release even if it is a prerelease (e.g. v1.1.0-rc1).

If javinizer was installed via Homebrew or Scoop, this command detects that and
tells you to use the package manager instead, since self-replacing would break
its bookkeeping.

Note: 'javinizer upgrade' updates the program itself; 'javinizer update' updates
metadata for your existing files. They are different commands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runUpgrade(cmd.Context(), update.UpgradeOptions{
				CurrentVersion: version.Short(),
				CheckOnly:      check,
				Force:          force,
				PreRelease:     prerelease,
				Out:            cmd.OutOrStdout(),
			})
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Upgrade failed: %v\n", err)
				if result != nil && result.Handoff {
					return nil
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&check, "check", "c", false, "check for an update without installing")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if already at the latest version")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "consider prereleases (e.g. v1.1.0-rc1) when finding the latest release")
	return cmd
}
