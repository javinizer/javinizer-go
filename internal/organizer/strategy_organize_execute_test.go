package organizer

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLinker tracks link calls for testing
type mockLinker struct {
	hardlinkErr    error
	symlinkErr     error
	copyErr        error
	hardlinkCalled bool
	symlinkCalled  bool
	copyCalled     bool
}

func (m *mockLinker) symlink(oldname, newname string) error {
	m.symlinkCalled = true
	return m.symlinkErr
}

func (m *mockLinker) hardlink(oldname, newname string) error {
	m.hardlinkCalled = true
	return m.hardlinkErr
}

func (m *mockLinker) copyFile(fs afero.Fs, src, dst string) error {
	m.copyCalled = true
	return m.copyErr
}

func TestOrganizeStrategy_Execute_MoveFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	t.Run("moves file to target", func(t *testing.T) {
		require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))
		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  true,
			LinkMode:   LinkModeNone,
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err)
		assert.True(t, result.Moved)
		assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", result.NewPath)
	})

	t.Run("no-op when WillMove is false", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetPath: "/source/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   false,
			moveFiles:  true,
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err)
		assert.False(t, result.Moved)
	})
}

func TestOrganizeStrategy_Execute_LinkModes(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}

	t.Run("hard link mode", func(t *testing.T) {
		ml := &mockLinker{}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)
		require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeHard,
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err)
		assert.True(t, result.Moved)
		assert.True(t, ml.hardlinkCalled)
	})

	t.Run("soft link mode", func(t *testing.T) {
		ml := &mockLinker{}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)
		require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeSoft,
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err)
		assert.True(t, result.Moved)
		assert.True(t, ml.symlinkCalled)
	})

	t.Run("copy mode (LinkModeNone)", func(t *testing.T) {
		ml := &mockLinker{}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)
		require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeNone,
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err)
		assert.True(t, result.Moved)
		assert.True(t, ml.copyCalled)
	})

	t.Run("invalid link mode returns error", func(t *testing.T) {
		ml := &mockLinker{}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkMode("invalid"),
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported link mode")
	})

	t.Run("conflicts detected returns error", func(t *testing.T) {
		ml := &mockLinker{}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeSoft,
			Conflicts:  []string{"target already exists"},
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts detected")
	})
}

func TestOrganizeStrategy_Execute_HardlinkErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}

	t.Run("hardlink cross-device error", func(t *testing.T) {
		ml := &mockLinker{hardlinkErr: syscall.EXDEV}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeHard,
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "same filesystem")
	})

	t.Run("hardlink permission error", func(t *testing.T) {
		ml := &mockLinker{hardlinkErr: os.ErrPermission}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeHard,
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("hardlink generic error", func(t *testing.T) {
		ml := &mockLinker{hardlinkErr: errors.New("link failed")}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeHard,
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create hard link")
	})
}

func TestOrganizeStrategy_Execute_SymlinkErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}

	t.Run("symlink permission error", func(t *testing.T) {
		ml := &mockLinker{symlinkErr: os.ErrPermission}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeSoft,
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("symlink generic error", func(t *testing.T) {
		ml := &mockLinker{symlinkErr: errors.New("symlink failed")}
		strategy := newOrganizeStrategy(fs, cfg, nil, ml)

		plan := &OrganizePlan{
			SourcePath: "/source/ABC-123.mp4",
			TargetDir:  "/dest/ABC-123",
			TargetPath: "/dest/ABC-123/ABC-123.mp4",
			TargetFile: "ABC-123.mp4",
			WillMove:   true,
			moveFiles:  false,
			LinkMode:   LinkModeSoft,
		}
		_, err := strategy.Execute(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create soft link")
	})
}

func TestOrganizeStrategy_Execute_CopyError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{copyErr: errors.New("copy failed")}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  false,
		LinkMode:   LinkModeNone,
	}
	_, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy file")
}

func TestOrganizeStrategy_Execute_MkdirErrorReadOnly(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  true,
	}
	_, err := strategy.Execute(plan)
	assert.Error(t, err)
}

func TestOrganizeStrategy_Execute_MoveError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	plan := &OrganizePlan{
		SourcePath: "/nonexistent/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  true,
	}
	_, err := strategy.Execute(plan)
	assert.Error(t, err)
}

func TestStrategyFromType(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}

	t.Run("default strategy", func(t *testing.T) {
		s := ResolveStrategy(fs, cfg, nil, nil)
		assert.NotNil(t, s)
	})
}
