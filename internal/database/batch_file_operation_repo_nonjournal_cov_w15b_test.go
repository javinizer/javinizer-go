package database

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (coverage) — UpdateNonJournalFields'
// raw-CONN legs (pool acquisition, BEGIN, guarded status UPDATE, suppressed
// re-read, COMMIT) cannot be wedged from outside a real sqlite pool, so these
// tests run the REAL mattn driver behind a query-inspecting conn wrapper whose
// fail-oracle replays the exact failure orderings the coverage gate needs.
// Everything between the injected failures executes against live sqlite, so
// the transactional posture (rollback of the column write on a status-update
// failure, no-op contract on a suppressed reverted row) is asserted for real.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/javinizer/javinizer-go/internal/models"
)

var errW15BOracle = errors.New("w15b injected query failure")

// w15bOracle fails queries matching its predicate with errW15BOracle.
type w15bOracle struct {
	fail func(query string) bool
}

func (o *w15bOracle) check(query string) error {
	if o.fail != nil && o.fail(query) {
		return errW15BOracle
	}
	return nil
}

// w15bFailConn embeds the live sqlite conn (Prepare/Close/Begin promote) and
// overrides only the context exec/query entry points database/sql dispatches
// directly to the driver.
type w15bFailConn struct {
	driver.Conn
	oracle *w15bOracle
}

func (c *w15bFailConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.oracle.check(query); err != nil {
		return nil, err
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *w15bFailConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.oracle.check(query); err != nil {
		return nil, err
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

// w15bFailConnector hands every pooled conn the shared oracle.
type w15bFailConnector struct {
	dsn    string
	oracle *w15bOracle
}

func (c *w15bFailConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&sqlite3.SQLiteDriver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &w15bFailConn{Conn: conn, oracle: c.oracle}, nil
}

func (c *w15bFailConnector) Driver() driver.Driver { return &sqlite3.SQLiteDriver{} }

// w15bConnPoolWrapper makes a *sql.DB invisible to gorm's DB() (no *sql.DB
// type match, no GetDBConnector) while fully functional through the
// gorm.ConnPool surface — the UpdateNonJournalFields pool-acquisition leg's
// refusal trigger.
type w15bConnPoolWrapper struct{ real *sql.DB }

func (w w15bConnPoolWrapper) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return w.real.PrepareContext(ctx, query)
}

func (w w15bConnPoolWrapper) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return w.real.ExecContext(ctx, query, args...)
}

func (w w15bConnPoolWrapper) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return w.real.QueryContext(ctx, query, args...)
}

func (w w15bConnPoolWrapper) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return w.real.QueryRowContext(ctx, query, args...)
}

// w15bOracleRepo builds a repo over shared-cache in-memory sqlite whose every
// driver query passes through the oracle; install the predicate AFTER setup
// (migrations and seed rows run clean). wrapPool hides the *sql.DB type from
// gorm's DB() for the pool-acquisition refusal leg.
func w15bOracleRepo(t *testing.T) (*BatchFileOperationRepository, *w15bOracle) {
	return w15bOracleRepoWrap(t, false)
}

func w15bOracleRepoWrap(t *testing.T, wrapPool bool) (*BatchFileOperationRepository, *w15bOracle) {
	t.Helper()
	oracle := &w15bOracle{}
	dsn := fmt.Sprintf("file:w15b_oracle_%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	sqlDB := sql.OpenDB(&w15bFailConnector{dsn: dsn, oracle: oracle})
	t.Cleanup(func() { _ = sqlDB.Close() })

	gdb, err := gorm.Open(&sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	require.NoError(t, err)
	d := &DB{DB: gdb, dsn: dsn, fs: afero.NewOsFs()}
	require.NoError(t, d.RunMigrationsOnStartup(context.Background()))
	if wrapPool {
		pool := w15bConnPoolWrapper{real: sqlDB}
		gdb.ConnPool = pool
		gdb.Statement.ConnPool = pool // gorm.DB() prefers the statement's pool
	}
	return NewBatchFileOperationRepository(d), oracle
}

func w15bRow(revertStatus models.RevertStatusEnum) *models.BatchFileOperation {
	return &models.BatchFileOperation{
		BatchJobID:   "job-w15b",
		MovieID:      "W15B-001",
		NewPath:      "/dst/lib/W15B-001.mkv",
		RevertStatus: revertStatus,
	}
}

// (a) the sql.DB acquisition leg: a ConnPool that no longer type-matches
// *sql.DB makes gorm's DB() refuse before any driver call — the live pool
// behind it proves nothing else about the repository changed.
func TestW15BUpdateNonJournalFields_SQLHandleUnacquirable(t *testing.T) {
	repo, _ := w15bOracleRepoWrap(t, true)

	err := repo.UpdateNonJournalFields(context.Background(), w15bRow(models.RevertStatusApplied))
	require.ErrorContains(t, err, "update non-journal fields")
	require.ErrorIs(t, err, gorm.ErrInvalidDB)
}

// (b) the Conn(ctx) leg: a closed pool hands out no connection.
func TestW15BUpdateNonJournalFields_ClosedPoolConnRefused(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = repo.UpdateNonJournalFields(context.Background(), w15bRow(models.RevertStatusApplied))
	require.ErrorContains(t, err, "update non-journal fields")
	require.ErrorContains(t, err, "database is closed", "database/sql refuses to hand out a conn from a closed pool")
}

// (c) BEGIN IMMEDIATE fails: the rollback defer also runs clean through the
// same conn, and the wrapped error names the update.
func TestW15BUpdateNonJournalFields_BeginimmediateWedged(t *testing.T) {
	repo, oracle := w15bOracleRepo(t)
	oracle.fail = func(query string) bool { return strings.HasPrefix(query, "BEGIN IMMEDIATE") }

	err := repo.UpdateNonJournalFields(context.Background(), w15bRow(models.RevertStatusApplied))
	require.ErrorIs(t, err, errW15BOracle)
	require.ErrorContains(t, err, "update non-journal fields")
}

// (d) the guarded status UPDATE fails: the tx rolls back — the column write
// from statement (a) never commits either.
func TestW15BUpdateNonJournalFields_StatusUpdateWedgedRollsBackColumns(t *testing.T) {
	repo, oracle := w15bOracleRepo(t)
	ctx := context.Background()
	op := w15bRow(models.RevertStatusApplied)
	require.NoError(t, repo.Create(ctx, op))

	oracle.fail = func(query string) bool {
		return strings.Contains(query, "SET revert_status")
	}
	mutated := *op
	mutated.NewPath = "/dst/never-committed.mkv"
	err := repo.UpdateNonJournalFields(ctx, &mutated)
	require.ErrorIs(t, err, errW15BOracle)
	require.ErrorContains(t, err, "update non-journal fields")

	oracle.fail = nil
	row, rerr := repo.FindByID(ctx, op.ID)
	require.NoError(t, rerr)
	require.Equal(t, "/dst/lib/W15B-001.mkv", row.NewPath, "the column write rolled back with the tx")
}

// (e) the suppressed-row re-read fails: 0-affected rows re-reads the STORED
// status inside the tx, and that scan failing surfaces as the update error.
func TestW15BUpdateNonJournalFields_SuppressedRereadWedged(t *testing.T) {
	repo, oracle := w15bOracleRepo(t)
	ctx := context.Background()
	op := w15bRow(models.RevertStatusApplied)
	require.NoError(t, repo.Create(ctx, op))
	require.NoError(t, repo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted))

	oracle.fail = func(query string) bool {
		return strings.HasPrefix(query, "SELECT revert_status FROM")
	}
	mutated := *op
	mutated.RevertStatus = models.RevertStatusApplied // suppressed by the reverted guard
	err := repo.UpdateNonJournalFields(ctx, &mutated)
	require.ErrorIs(t, err, errW15BOracle)
	require.ErrorContains(t, err, "update non-journal fields")

	oracle.fail = nil
	row, rerr := repo.FindByID(ctx, op.ID)
	require.NoError(t, rerr)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the wedged re-read rolled the tx back; the row stays reverted")
}

// (f) COMMIT fails: committed flips back false, the deferred ROLLBACK
// releases the write lock, and the caller sees the wrapped commit error.
func TestW15BUpdateNonJournalFields_CommitWedged(t *testing.T) {
	repo, oracle := w15bOracleRepo(t)
	ctx := context.Background()
	op := w15bRow(models.RevertStatusApplied)
	require.NoError(t, repo.Create(ctx, op))

	oracle.fail = func(query string) bool { return query == "COMMIT" }
	err := repo.UpdateNonJournalFields(ctx, op)
	require.ErrorIs(t, err, errW15BOracle)
	require.ErrorContains(t, err, "update non-journal fields")
}
