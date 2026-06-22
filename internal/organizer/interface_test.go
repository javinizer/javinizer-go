package organizer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOrganizerWithLinker(fs afero.Fs, cfg *Config, engine template.EngineInterface, m matcher.MatcherInterface, l linker) *Organizer {
	if engine == nil {
		engine = template.NewEngine()
	}
	if l == nil {
		l = OSLinker{}
	}
	return &Organizer{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: newSubtitleHandler(fs, cfg.SubtitleExtensions),
		matcher:         m,
		linker:          l,
	}
}

func TestOrganizerInterface(t *testing.T) {
	newOrganizer := func() (*Organizer, afero.Fs, *testutil.MovieBuilder) {
		fs := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}
		org := newOrganizerWithLinker(fs, cfg, nil, nil, &MemLinker{})
		return org, fs, testutil.NewMovieBuilder().WithID("IPX-535").WithTitle("Beautiful Day")
	}

	t.Run("happy-path move", func(t *testing.T) {
		org, fs, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		sourcePath := "/source/IPX-535.mp4"
		require.NoError(t, fs.MkdirAll(filepath.Dir(sourcePath), 0o755))
		require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))

		result, err := iface.Organize(context.Background(), OrganizeCmd{
			Match: models.FileMatchInfo{
				Path:      sourcePath,
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
				MovieID:   movie.ID,
			},
			Movie:     movie,
			DestDir:   "/dest",
			MoveFiles: true,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Moved)
		assert.True(t, result.ShouldGenerateMetadata)
		assert.Equal(t, filepath.ToSlash("/dest/IPX-535/IPX-535.mp4"), filepath.ToSlash(result.NewPath))
		exists, err := afero.Exists(fs, result.NewPath)
		require.NoError(t, err)
		assert.True(t, exists)
		exists, err = afero.Exists(fs, sourcePath)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("copy", func(t *testing.T) {
		org, fs, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		sourcePath := "/source/IPX-535.mp4"
		require.NoError(t, fs.MkdirAll(filepath.Dir(sourcePath), 0o755))
		require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))

		result, err := iface.Organize(context.Background(), OrganizeCmd{
			Match: models.FileMatchInfo{
				Path:      sourcePath,
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
				MovieID:   movie.ID,
			},
			Movie:     movie,
			DestDir:   "/dest",
			MoveFiles: false,
			LinkMode:  LinkModeNone,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Moved)
		exists, err := afero.Exists(fs, sourcePath)
		require.NoError(t, err)
		assert.True(t, exists)
		exists, err = afero.Exists(fs, result.NewPath)
		require.NoError(t, err)
		assert.True(t, exists)

		content, err := afero.ReadFile(fs, result.NewPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("video"), content)
	})

	t.Run("dry-run", func(t *testing.T) {
		org, fs, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		sourcePath := "/source/IPX-535.mp4"
		require.NoError(t, fs.MkdirAll(filepath.Dir(sourcePath), 0o755))
		require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))

		result, err := iface.Organize(context.Background(), OrganizeCmd{
			Match: models.FileMatchInfo{
				Path:      sourcePath,
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
				MovieID:   movie.ID,
			},
			Movie:     movie,
			DestDir:   "/dest",
			MoveFiles: true,
			DryRun:    true,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Moved)
		assert.True(t, result.ShouldGenerateMetadata)
		exists, err := afero.Exists(fs, sourcePath)
		require.NoError(t, err)
		assert.True(t, exists)
		exists, err = afero.Exists(fs, result.NewPath)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("context cancellation", func(t *testing.T) {
		org, _, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := iface.Organize(ctx, OrganizeCmd{Movie: movie})
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, result)
	})

	t.Run("validation failure", func(t *testing.T) {
		org, _, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		result, err := iface.Organize(context.Background(), OrganizeCmd{
			Match: models.FileMatchInfo{
				Path:      "/source/missing.mp4",
				Name:      "missing.mp4",
				Extension: ".mp4",
				MovieID:   movie.ID,
			},
			Movie:     movie,
			DestDir:   "/dest",
			MoveFiles: true,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "organization validation failed")
		assert.Nil(t, result)
	})

	t.Run("conflict detection", func(t *testing.T) {
		org, fs, movieBuilder := newOrganizer()
		var iface OrganizerInterface = org
		movie := movieBuilder.Build()

		sourcePath := "/source/IPX-535.mp4"
		targetPath := "/dest/IPX-535/IPX-535.mp4"
		require.NoError(t, fs.MkdirAll(filepath.Dir(sourcePath), 0o755))
		require.NoError(t, fs.MkdirAll(filepath.Dir(targetPath), 0o755))
		require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))
		require.NoError(t, afero.WriteFile(fs, targetPath, []byte("existing"), 0o644))

		result, err := iface.Organize(context.Background(), OrganizeCmd{
			Match: models.FileMatchInfo{
				Path:      sourcePath,
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
				MovieID:   movie.ID,
			},
			Movie:     movie,
			DestDir:   "/dest",
			MoveFiles: true,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "organization validation failed")
		assert.Contains(t, err.Error(), filepath.FromSlash(targetPath))
		assert.Nil(t, result)
	})
}
