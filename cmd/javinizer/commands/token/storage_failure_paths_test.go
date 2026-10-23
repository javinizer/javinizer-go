package token

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTokenStorageBootstrapFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("preserved"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(blocker, "database.sqlite")
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	for _, tc := range []struct {
		name string
		run  func(*testing.T) error
	}{
		{"create", func(t *testing.T) error {
			result, err := RunCreate(newCreateCommand(), nil, path)
			require.Nil(t, result)
			return err
		}},
		{"revoke", func(t *testing.T) error {
			result, err := RunRevoke(&cobra.Command{}, []string{"missing"}, path)
			require.Nil(t, result)
			return err
		}},
		{"list", func(t *testing.T) error {
			result, err := RunList(&cobra.Command{}, nil, path)
			require.Nil(t, result)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			require.ErrorContains(t, err, "failed to initialize dependencies: failed to create database directory:")
			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, blocker, pathErr.Path)
			got, err := os.ReadFile(blocker)
			require.NoError(t, err)
			require.Equal(t, "preserved", string(got))
		})
	}
}

var errFinalTokenWrite = errors.New("final token output rejected")

// Discard successful writes, including ephemeral plaintext token material.
type finalTokenWriteFailure struct{ remaining int }

func (w *finalTokenWriteFailure) Write(p []byte) (int, error) {
	if w.remaining == 0 {
		return 0, errFinalTokenWrite
	}
	w.remaining--
	return len(p), nil
}

func TestTokenFinalOutputFailureDoesNotUndoPersistence(t *testing.T) {
	path := setupMiss4DB(t)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	cmd := newCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "output-fixture"))
	writer := &finalTokenWriteFailure{remaining: 8}
	cmd.SetOut(writer)
	require.ErrorIs(t, runCreate(cmd, path), errFinalTokenWrite)
	require.Zero(t, writer.remaining)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := database.NewApiTokenRepository(db)
	active, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "output-fixture", active[0].Name)
	revoke := &cobra.Command{}
	writer = &finalTokenWriteFailure{remaining: 4}
	revoke.SetOut(writer)
	require.ErrorIs(t, runRevoke(revoke, path, active[0].ID), errFinalTokenWrite)
	require.Zero(t, writer.remaining)
	persisted, err := repo.FindByID(context.Background(), active[0].ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt)
	remaining, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestTokenListPropagatesRowFlushFailure(t *testing.T) {
	path := setupMiss4DB(t)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	create := newCreateCommand()
	// Names are unrestricted CLI strings. An empty line flushes tabwriter's
	// buffered header while it processes this valid row, rather than at Flush.
	require.NoError(t, create.Flags().Set("name", "first\n\nsecond"))
	created, err := RunCreate(create, nil, path)
	require.NoError(t, err)
	require.NotNil(t, created)
	list := &cobra.Command{}
	list.SetOut(&finalTokenWriteFailure{})
	require.ErrorIs(t, runList(list, path), errFinalTokenWrite)
	entries, err := RunList(&cobra.Command{}, nil, path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "first\n\nsecond", entries[0].Name)
}
