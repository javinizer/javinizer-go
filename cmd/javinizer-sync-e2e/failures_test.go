package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// These tests deliberately corrupt only disposable local fixture state. They
// exercise real filesystem and SQLite errors, not fabricated coverage counters.
func privatePlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires native private directories and artifact publication")
	}
}
func fixtureDB(t *testing.T, root string) *database.DB {
	t.Helper()
	for _, name := range []string{"ready", "stale", "future", "missing"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts", name), 0700))
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "names.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	return db
}
func fixtureRunEnv(t *testing.T, root string) {
	t.Helper()
	old := os.Args
	os.Args = []string{"fixture"}
	t.Cleanup(func() { os.Args = old })
	t.Setenv("JAVINIZER_SYNC_E2E_ROOT", root)
	t.Setenv("JAVINIZER_SYNC_E2E_MODE", "disabled")
	t.Setenv("JAVINIZER_SYNC_E2E_PORT", "18081")
	t.Setenv("JAVINIZER_SYNC_E2E_PAUSE_AFTER", "-1")
}
func TestFixtureRootFailureBoundaries(t *testing.T) {
	privatePlatform(t)
	t.Run("missing", func(t *testing.T) {
		require.ErrorContains(t, validateRoot(filepath.Join(t.TempDir(), "absent")), "private directory")
	})
	t.Run("unclean", func(t *testing.T) {
		require.ErrorContains(t, validateRoot(t.TempDir()+"/../fixture"), "absolute and clean")
	})
	for _, kind := range []string{"missing", "directory", "same-size-invalid", "unreadable"} {
		t.Run("marker-"+kind, func(t *testing.T) {
			root := testRoot(t)
			p := filepath.Join(root, ".javinizer-sync-e2e")
			switch kind {
			case "missing":
				require.NoError(t, os.Remove(p))
			case "directory":
				require.NoError(t, os.Remove(p))
				require.NoError(t, os.Mkdir(p, 0700))
			case "same-size-invalid":
				require.NoError(t, os.WriteFile(p, make([]byte, len(disposableMarker)), 0600))
			case "unreadable":
				require.NoError(t, os.Chmod(p, 0000))
				t.Cleanup(func() { _ = os.Chmod(p, 0600) })
				_, err := os.ReadFile(p)
				if err == nil {
					t.Skip("current process can read mode-0000 files")
				}
				require.ErrorIs(t, err, os.ErrPermission)
			}
			require.Error(t, validateRoot(root))
		})
	}
	t.Run("public", func(t *testing.T) {
		root := testRoot(t)
		require.NoError(t, os.Chmod(root, 0755))
		require.ErrorContains(t, validateRoot(root), "private directory")
	})
	t.Run("root-symlink", func(t *testing.T) {
		root := testRoot(t)
		link := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(root, link))
		require.ErrorContains(t, validateRoot(link), "private directory")
	})
}
func TestProvisionFailureBoundaries(t *testing.T) {
	privatePlatform(t)
	for _, tc := range []struct{ name, table, condition string }{
		{"import", "actresses", "NEW.dmm_id = 700"}, {"manual", "actresses", "NEW.dmm_id = 704"}, {"guard", "actress_sync_name_guards", "1"},
		{"movie", "movies", "1"}, {"credit", "movie_credits", "1"}, {"translation", "actress_translations", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testRoot(t)
			db := fixtureDB(t, root)
			require.NoError(t, db.Exec("CREATE TRIGGER fixture_reject BEFORE INSERT ON "+tc.table+" WHEN "+tc.condition+" BEGIN SELECT RAISE(ABORT, 'fixture write rejected'); END").Error)
			require.ErrorContains(t, provision(root, db), "fixture write rejected")
			require.NoFileExists(t, filepath.Join(root, "fixture.json"))
			var count int64
			require.NoError(t, db.Table(tc.table).Count(&count).Error)
			if tc.table != "actresses" {
				require.Zero(t, count)
			}
		})
	}
	t.Run("artifact-directory", func(t *testing.T) {
		root := testRoot(t)
		db := fixtureDB(t, root)
		require.NoError(t, os.RemoveAll(filepath.Join(root, "artifacts")))
		require.NoError(t, os.WriteFile(filepath.Join(root, "artifacts"), nil, 0600))
		require.Error(t, provision(root, db))
		require.NoFileExists(t, filepath.Join(root, "fixture.json"))
	})
	t.Run("manifest-write", func(t *testing.T) {
		root := testRoot(t)
		db := fixtureDB(t, root)
		require.NoError(t, os.Mkdir(filepath.Join(root, "fixture.json"), 0700))
		require.Error(t, provision(root, db))
		require.DirExists(t, filepath.Join(root, "fixture.json"))
	})
}
func TestInspectFailureBoundaries(t *testing.T) {
	// These cases require a usable native fixture database URI. Calling inspect
	// directly bypasses run's private-root gate; Windows rejects that gate and its
	// backslash paths are not supported by this disposable POSIX fixture reader.
	if runtime.GOOS == "windows" {
		t.Skip("Windows fixture command fails closed before SQLite inspection")
	}
	t.Run("corrupt", func(t *testing.T) {
		root := testRoot(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "names.db"), []byte("not sqlite"), 0600))
		require.ErrorContains(t, inspect(root), "not a database")
	})
	t.Run("missing-schema", func(t *testing.T) {
		root := testRoot(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "names.db"), nil, 0600))
		require.ErrorContains(t, inspect(root), "no such table")
	})
	t.Run("row-evaluation", func(t *testing.T) {
		root := testRoot(t)
		db := fixtureDB(t, root)
		require.NoError(t, db.Create(&models.Actress{FirstName: "Fixture"}).Error)
		require.NoError(t, db.Exec("ALTER TABLE actresses RENAME TO fixture_actors").Error)
		require.NoError(t, db.Exec("CREATE VIEW actresses AS SELECT id, dmm_id, abs(-9223372036854775808) AS first_name, last_name, japanese_name, thumb_url, aliases, verified, origin, name_key, ambiguity_quarantined FROM fixture_actors").Error)
		require.ErrorContains(t, inspect(root), "integer overflow")
	})

	t.Run("blob-and-writer", func(t *testing.T) {
		root := testRoot(t)
		db := fixtureDB(t, root)
		require.NoError(t, db.Create(&models.Actress{FirstName: "Fixture"}).Error)
		require.NoError(t, db.Exec("UPDATE actresses SET aliases = CAST('blob alias' AS BLOB)").Error)
		output, err := os.CreateTemp(t.TempDir(), "inspect")
		require.NoError(t, err)
		t.Cleanup(func() { _ = output.Close() })
		old := os.Stdout
		os.Stdout = output
		t.Cleanup(func() { os.Stdout = old })
		require.NoError(t, inspect(root))
		b, err := os.ReadFile(output.Name())
		require.NoError(t, err)
		var data map[string][]map[string]any
		require.NoError(t, json.Unmarshal(b, &data))
		require.Equal(t, "blob alias", data["actors"][0]["aliases"])
		require.NoError(t, output.Close())
		require.ErrorIs(t, inspect(root), os.ErrClosed)
	})
}
func TestGateFilesystemFailures(t *testing.T) {
	t.Run("paused-marker", func(t *testing.T) {
		root := testRoot(t)
		require.NoError(t, os.Mkdir(filepath.Join(root, "worker.paused"), 0700))
		gate := &gatedRepository{root: root, pauseAfter: 0}
		lease, err := gate.ClaimOfflineNameSync(t.Context(), "job", "worker", time.Now(), time.Minute)
		require.Error(t, err)
		require.Nil(t, lease)
	})
	t.Run("release-stat", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires platform privileges")
		}
		root := testRoot(t)
		p := filepath.Join(root, "worker.release")
		require.NoError(t, os.Symlink(p, p))
		gate := &gatedRepository{root: root, pauseAfter: 0}
		lease, err := gate.ClaimOfflineNameSync(t.Context(), "job", "worker", time.Now(), time.Minute)
		require.Error(t, err)
		require.Nil(t, lease)
		require.FileExists(t, filepath.Join(root, "worker.paused"))
	})
	t.Run("cancel-while-waiting", func(t *testing.T) {
		root := testRoot(t)
		gate := &gatedRepository{root: root, pauseAfter: 0}
		ctx, cancel := context.WithTimeout(t.Context(), 65*time.Millisecond)
		defer cancel()
		lease, err := gate.ClaimOfflineNameSync(ctx, "job", "worker", time.Now(), time.Minute)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, lease)
	})
}
func TestRunStartupFailures(t *testing.T) {
	privatePlatform(t)
	t.Run("directory-file", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp"), nil, 0600))
		require.ErrorContains(t, run(), "fixture directories must not be symlinks")
		require.NoFileExists(t, filepath.Join(root, "names.db"))
	})
	t.Run("database-corrupt", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		require.NoError(t, os.WriteFile(filepath.Join(root, "names.db"), []byte("not sqlite"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.json"), []byte("{}"), 0600))
		require.ErrorContains(t, run(), "not a database")
	})
	t.Run("migration-schema", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		db := fixtureDB(t, root)
		require.NoError(t, db.Exec("DROP TABLE schema_migrations").Error)
		require.NoError(t, db.Exec("CREATE TABLE schema_migrations (broken TEXT)").Error)
		require.NoError(t, db.Close())
		require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.json"), []byte("{}"), 0600))
		require.Error(t, run())
	})
	t.Run("directory-stat", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		p := filepath.Join(root, "artifacts")
		require.NoError(t, os.Mkdir(p, 0700))
		require.NoError(t, os.Chmod(p, 0000))
		t.Cleanup(func() { _ = os.Chmod(p, 0700) })
		_, err := os.Stat(filepath.Join(p, "ready"))
		if !os.IsPermission(err) {
			t.Skip("current process can traverse mode-0000 directories")
		}
		require.ErrorIs(t, run(), os.ErrPermission)
		require.NoFileExists(t, filepath.Join(root, "names.db"))
	})

	t.Run("directory-create", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		require.NoError(t, os.Chmod(root, 0500))
		t.Cleanup(func() { _ = os.Chmod(root, 0700) })
		requireDirectoryWriteDenied(t, root)
		require.ErrorIs(t, run(), os.ErrPermission)
		require.NoFileExists(t, filepath.Join(root, "names.db"))
	})
	t.Run("provision-write", func(t *testing.T) {
		root := testRoot(t)
		fixtureRunEnv(t, root)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts", "ready"), 0700))
		require.NoError(t, os.Chmod(filepath.Join(root, "artifacts", "ready"), 0500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "artifacts", "ready"), 0700) })
		requireDirectoryWriteDenied(t, filepath.Join(root, "artifacts", "ready"))
		require.Error(t, run())
		require.NoFileExists(t, filepath.Join(root, "fixture.json"))
	})
	t.Run("auth-setup", func(t *testing.T) {
		root, _, _ := testFixture(t)
		fixtureRunEnv(t, root)
		p := filepath.Join(root, "auth.sessions.json")
		require.NoError(t, os.Mkdir(p, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(p, "keep"), nil, 0600))
		require.ErrorContains(t, run(), "failed to remove auth session file")
		require.FileExists(t, filepath.Join(root, "auth.credentials.json"))
		require.FileExists(t, filepath.Join(p, "keep"))
	})

	t.Run("auth-invalid", func(t *testing.T) {
		root, _, _ := testFixture(t)
		fixtureRunEnv(t, root)
		require.NoError(t, os.WriteFile(filepath.Join(root, "auth.credentials.json"), []byte("invalid"), 0600))
		require.ErrorContains(t, run(), "parse auth credential")
	})
	t.Run("bind-occupied", func(t *testing.T) {
		root, _, _ := testFixture(t)
		fixtureRunEnv(t, root)
		lc := net.ListenConfig{}
		l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer l.Close()
		t.Setenv("JAVINIZER_SYNC_E2E_PORT", strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
		err = run()
		var op *net.OpError
		require.ErrorAs(t, err, &op)
		require.Equal(t, "listen", op.Op)
		require.FileExists(t, filepath.Join(root, "auth.credentials.json"))
	})
}

func TestRunRejectsUntrustedRoot(t *testing.T) {
	fixtureRunEnv(t, "relative")
	require.ErrorContains(t, run(), "absolute and clean")
}

// A privileged runner may bypass DAC permissions. Probe before run(), which
// otherwise could start a server instead of reaching the intended error.
func requireDirectoryWriteDenied(t *testing.T, dir string) {
	t.Helper()
	f, err := os.CreateTemp(dir, "permission-probe-")
	if err == nil {
		require.NoError(t, f.Close())
		require.NoError(t, os.Remove(f.Name()))
		t.Skip("current process can write mode-0500 directories")
	}
	require.ErrorIs(t, err, os.ErrPermission)
}

func TestInspectCommandRejectsUnenforceablePrivateRoot(t *testing.T) {
	root := testRoot(t)
	info, err := os.Stat(root)
	require.NoError(t, err)
	if info.Mode().Perm()&0077 == 0 {
		t.Skip("platform enforces private fixture directories")
	}
	fixtureRunEnv(t, root)
	os.Args = []string{"fixture", "inspect"}
	path := filepath.Join(root, "names.db")
	before := []byte("existing database must not be opened or replaced")
	require.NoError(t, os.WriteFile(path, before, 0600))
	require.ErrorContains(t, run(), "private directory")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoFileExists(t, filepath.Join(root, "names.db-wal"))
	require.NoFileExists(t, filepath.Join(root, "names.db-shm"))
}
