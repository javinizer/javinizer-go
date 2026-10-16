package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func previewDatabaseSnapshot(t *testing.T, db *DB) string {
	t.Helper()
	var tables []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").Scan(&tables).Error)
	out := map[string]any{}
	for _, table := range tables {
		var rows []map[string]interface{}
		require.NoError(t, db.Raw(fmt.Sprintf("SELECT * FROM %q ORDER BY rowid", table)).Find(&rows).Error)
		out[table] = rows
	}
	data, err := json.Marshal(out)
	require.NoError(t, err)
	return string(data)
}

func TestPreviewTxReconciliationLeavesEveryTableUnchanged(t *testing.T) {
	db := newCreditTestDB(t)
	a := models.Actress{FirstName: "Original", Origin: "scraper"}
	b := models.Actress{FirstName: "Edited", Origin: "scraper"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	movie, err := NewMovieRepository(db).Upsert(t.Context(), &models.Movie{ContentID: "PREVIEW", ID: "PREVIEW", Title: "Title", Actresses: []models.Actress{a}})
	require.NoError(t, err)
	original := movie.Clone()
	before := previewDatabaseSnapshot(t, db)
	candidate := movie.Clone()
	candidate.Actresses[0].FirstName, candidate.Credits = "Edited", nil
	var projection *models.Movie
	err = db.WithPreviewTx(t.Context(), func(u EditUnit) error {
		if err := u.Actresses.RenameNameFields(t.Context(), a.ID, "Edited", "", ""); err != nil {
			return err
		}
		projection, err = u.Movies.Upsert(t.Context(), candidate)
		return err
	})
	require.NoError(t, err)
	require.True(t, projection.Credits[0].Actress.AmbiguityQuarantined)
	require.Equal(t, before, previewDatabaseSnapshot(t, db), "includes identities, aliases, collisions, translations, generations, envelopes and sqlite_sequence")
	require.Equal(t, original, movie)
}

func newPreviewProbeDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(&Config{DSN: filepath.Join(t.TempDir(), "preview.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Exec("CREATE TABLE preview_probe (id INTEGER PRIMARY KEY, value TEXT)").Error)
	return db
}

func TestPreviewTxErrorsAndPanicAlwaysRollBack(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db := newPreviewProbeDB(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			boom := errors.New("preview failed")
			run := func() error {
				return db.WithPreviewTx(ctx, func(u EditUnit) error {
					require.NoError(t, u.Movies.(*MovieRepository).GetDB().Exec("INSERT INTO preview_probe VALUES (1, 'tentative')").Error)
					switch mode {
					case "error":
						return boom
					case "panic":
						panic(boom)
					case "cancel":
						cancel()
					}
					return nil
				})
			}
			if mode == "panic" {
				require.PanicsWithValue(t, boom, func() { _ = run() })
			} else if mode == "error" {
				require.ErrorIs(t, run(), boom)
			} else if mode == "cancel" {
				require.ErrorIs(t, run(), context.Canceled)
			} else {
				require.NoError(t, run())
			}
			var count int64
			require.NoError(t, db.Table("preview_probe").Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

type previewInvalidPool struct{ gorm.ConnPool }

func TestPreviewTxRejectsUnavailableTransactions(t *testing.T) {
	fn := func(EditUnit) error { t.Fatal("callback must not run"); return nil }
	var absent *DB
	require.ErrorIs(t, absent.WithPreviewTx(t.Context(), fn), gorm.ErrInvalidDB)
	db := newPreviewProbeDB(t)
	broken := db.WithContext(t.Context())
	broken.Statement.ConnPool = previewInvalidPool{}
	require.ErrorIs(t, (&DB{DB: broken}).WithPreviewTx(t.Context(), fn), gorm.ErrInvalidDB)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.ErrorIs(t, (&DB{DB: tx}).WithPreviewTx(t.Context(), fn), gorm.ErrInvalidDB)
	require.NoError(t, tx.Rollback().Error)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, db.WithPreviewTx(ctx, fn), context.Canceled)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(c any) error {
		c.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, a, _, _ string) int {
			if op == sqlite3.SQLITE_TRANSACTION && a == "BEGIN" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	require.Error(t, db.WithPreviewTx(t.Context(), fn))
}

type previewCloseFaultDriver struct{ closes *atomic.Int32 }
type previewCloseFaultConn struct {
	*sqlite3.SQLiteConn
	closes *atomic.Int32
}

func (d previewCloseFaultDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite3.SQLiteDriver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &previewCloseFaultConn{SQLiteConn: c.(*sqlite3.SQLiteConn), closes: d.closes}, nil
}
func (c *previewCloseFaultConn) Close() error {
	c.closes.Add(1)
	_ = c.SQLiteConn.Close()
	return errors.New("injected close failure after physical close")
}

var previewDriverCounter atomic.Uint64

func TestPreviewTxRollbackFailureDiscardsDirtyConnection(t *testing.T) {
	for _, closeFault := range []bool{false, true} {
		t.Run(fmt.Sprint("close-fault=", closeFault), func(t *testing.T) {
			var closes atomic.Int32
			path := filepath.Join(t.TempDir(), "rollback.db")
			dialector := sqlite.Open(path)
			if closeFault {
				name := fmt.Sprintf("preview-close-fault-%d", previewDriverCounter.Add(1))
				sql.Register(name, previewCloseFaultDriver{closes: &closes})
				dialector = sqlite.Dialector{DriverName: name, DSN: path}
			}
			gdb, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			db := &DB{DB: gdb}
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.Exec("CREATE TABLE preview_probe (id INTEGER PRIMARY KEY, value TEXT)").Error)
			pool, err := gdb.DB()
			require.NoError(t, err)
			pool.SetMaxOpenConns(1)
			conn, err := pool.Conn(t.Context())
			require.NoError(t, err)
			require.NoError(t, conn.Raw(func(c any) error {
				var sqliteConn *sqlite3.SQLiteConn
				if closeFault {
					sqliteConn = c.(*previewCloseFaultConn).SQLiteConn
				} else {
					sqliteConn = c.(*sqlite3.SQLiteConn)
				}
				sqliteConn.RegisterAuthorizer(func(op int, a, _, _ string) int {
					if op == sqlite3.SQLITE_TRANSACTION && a == "ROLLBACK" {
						return sqlite3.SQLITE_DENY
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			}))
			require.NoError(t, conn.Close())
			err = db.WithPreviewTx(t.Context(), func(u EditUnit) error {
				return u.Movies.(*MovieRepository).GetDB().Exec("INSERT INTO preview_probe VALUES (1, 'tentative')").Error
			})
			require.ErrorContains(t, err, "rollback movie preview")
			if closeFault {
				require.Equal(t, int32(1), closes.Load())
			}
			require.NoError(t, db.Exec("INSERT INTO preview_probe VALUES (2, 'later real write')").Error)
			var ids []int
			require.NoError(t, db.Table("preview_probe").Order("id").Pluck("id", &ids).Error)
			require.Equal(t, []int{2}, ids, "later writes must never commit the rejected preview")
		})
	}
}

func TestPreviewTxLockHonorsRequestDeadline(t *testing.T) {
	db := newPreviewProbeDB(t)
	require.NoError(t, db.Exec("INSERT INTO preview_probe VALUES (1, 'original')").Error)
	writer := db.Begin()
	require.NoError(t, writer.Error)
	require.NoError(t, writer.Exec("UPDATE preview_probe SET value='writer' WHERE id=1").Error)
	defer writer.Rollback()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := db.WithPreviewTx(ctx, func(u EditUnit) error {
		return u.Movies.(*MovieRepository).GetDB().Exec("UPDATE preview_probe SET value='preview' WHERE id=1").Error
	})
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second, "a preview must not wait for the connection's longer busy timeout")
}

func TestPreviewTxRestoresOrDiscardsConnectionSettings(t *testing.T) {
	for _, mode := range []string{"success", "read error", "setup error", "restore error"} {
		t.Run(mode, func(t *testing.T) {
			db := newPreviewProbeDB(t)
			pool, err := db.DB.DB()
			require.NoError(t, err)
			pool.SetMaxOpenConns(1)
			conn, err := pool.Conn(t.Context())
			require.NoError(t, err)
			var original int
			require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&original))
			var callbackFinished atomic.Bool
			require.NoError(t, conn.Raw(func(c any) error {
				c.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, a, b, _ string) int {
					if op == sqlite3.SQLITE_PRAGMA && a == "busy_timeout" {
						if (mode == "read error" && b == "") || (mode == "setup error" && b == "50") || (mode == "restore error" && callbackFinished.Load()) {
							return sqlite3.SQLITE_DENY
						}
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			}))
			require.NoError(t, conn.Close())
			err = db.WithPreviewTx(t.Context(), func(u EditUnit) error {
				if mode == "read error" || mode == "setup error" {
					t.Fatal("setup failure must not run the callback")
				}
				var timeout int
				require.NoError(t, u.Movies.(*MovieRepository).GetDB().Raw("PRAGMA busy_timeout").Scan(&timeout).Error)
				require.Equal(t, 50, timeout)
				callbackFinished.Store(true)
				return u.Movies.(*MovieRepository).GetDB().Exec("INSERT INTO preview_probe VALUES (1, 'tentative')").Error
			})
			if mode == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if mode == "read error" {
				conn, err := pool.Conn(t.Context())
				require.NoError(t, err)
				require.NoError(t, conn.Raw(func(c any) error { c.(*sqlite3.SQLiteConn).RegisterAuthorizer(nil); return nil }))
				require.NoError(t, conn.Close())
			}
			var timeout int
			require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&timeout).Error)
			require.Equal(t, original, timeout)
			require.NoError(t, db.Exec("INSERT INTO preview_probe VALUES (2, 'real')").Error)
			var ids []int
			require.NoError(t, db.Table("preview_probe").Order("id").Pluck("id", &ids).Error)
			require.Equal(t, []int{2}, ids)
		})
	}
}
