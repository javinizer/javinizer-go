package tag

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCanceledTagCommandsPreserveAssociations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "database.sqlite")
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	cfg.Logging.Output = "stdout"
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	repo := database.NewMovieTagRepository(deps.DB)
	require.NoError(t, repo.AddTag(context.Background(), "TEST-001", "kept"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string, string) error
		args []string
		want string
	}{
		{"add", runAdd, []string{"TEST-001", "new"}, "failed to add tag 'new': context canceled"},
		{"list movie", runList, []string{"TEST-001"}, "failed to get tags: resolve movie tag key TEST-001: context canceled"},
		{"list all", runList, nil, "failed to list tags: find movie tags: context canceled"},
		{"remove one", runRemove, []string{"TEST-001", "kept"}, "failed to remove tag: context canceled"},
		{"remove all", runRemove, []string{"TEST-001"}, "failed to remove tags: context canceled"},
		{"search", runSearch, []string{"kept"}, "failed to search: find movies with tag kept: context canceled"},
		{"tags", runAllTags, nil, "failed to list tags: find unique tags: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			err := tc.run(cmd, tc.args, path)
			require.ErrorIs(t, err, context.Canceled)
			require.EqualError(t, err, tc.want)
			tags, err := repo.GetTagsForMovie(context.Background(), "TEST-001")
			require.NoError(t, err)
			require.Equal(t, []string{"kept"}, tags)
		})
	}
}
