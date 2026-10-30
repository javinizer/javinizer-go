package organizer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestOrganizePostSanitizePathBudgetPreservesSourceAndAllowsExactFit(t *testing.T) {
	for _, tc := range []struct {
		name, format, title, folder string
	}{
		{"missing studio uses whole movie ID", "<STUDIO>", "Title", "ABC-001"},
		{"sanitization expands colon", "<TITLE>", "A:B", "A -B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "a.mp4")
			dest := filepath.Join(root, "output")
			payload := []byte("owned video bytes must survive path-budget refusal")
			require.NoError(t, os.WriteFile(source, payload, 0600))
			before, err := os.Stat(source)
			require.NoError(t, err)

			// The raw rendering fits three bytes (or is empty), but the
			// sanitized folder/fallback does not. Account for the actual
			// absolute temp path rather than assuming platform path lengths.
			overhead := len(filepath.Join(dest, "X", "a.mp4")) - 1
			limit := overhead + 3
			finalPath := filepath.Join(dest, tc.folder, "a.mp4")
			require.Greater(t, len(finalPath), limit)
			cfg := organizer.Config{
				FolderFormat:  tc.format,
				MaxPathLength: limit,
				OperationMode: operationmode.OperationModeOrganize,
			}
			cmd := organizer.OrganizeCmd{
				Match:     models.FileMatchInfo{Path: source, Name: "a.mp4", MovieID: "ABC-001"},
				Movie:     &models.Movie{ID: "ABC-001", Title: tc.title},
				DestDir:   dest,
				MoveFiles: false,
			}
			result, err := organizer.NewOrganizer(afero.NewOsFs(), &cfg, nil, nil).Organize(context.Background(), cmd)
			require.Nil(t, result)
			require.EqualError(t, err, fmt.Sprintf("path validation failed: path length %d exceeds limit %d", len(finalPath), limit))
			after, err := os.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			require.Equal(t, before.Mode(), after.Mode())
			require.Equal(t, before.ModTime(), after.ModTime())
			got, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, payload, got)
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Len(t, entries, 1, "refused planning must not create output or staging artifacts")
			require.Equal(t, "a.mp4", entries[0].Name())

			// A fresh organizer with exactly enough budget must complete a
			// real copy, demonstrating that the refusal is recoverable.
			cfg.MaxPathLength = len(finalPath)
			result, err = organizer.NewOrganizer(afero.NewOsFs(), &cfg, nil, nil).Organize(context.Background(), cmd)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, finalPath, result.NewPath)
			require.Equal(t, filepath.Dir(finalPath), result.FolderPath)
			got, err = os.ReadFile(finalPath)
			require.NoError(t, err)
			require.Equal(t, payload, got)
			got, err = os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, payload, got)
			after, err = os.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after), "successful copy must preserve its source")
		})
	}
}
