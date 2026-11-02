package database_test

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// GORM's Connection callback lends one *sql.Conn, not ownership of its pool.
// Wrapping that public scoped handle must not turn Close into pool disposal.
func TestDBCloseRejectsBorrowedConnectionWithoutDisposingOwner(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "owner.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE connection_probe (id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Connection(func(conn *gorm.DB) error {
		borrowed := &database.DB{DB: conn}
		require.ErrorIs(t, borrowed.Close(), gorm.ErrInvalidDB)
		return borrowed.Exec("INSERT INTO connection_probe VALUES (1, 'still borrowed')").Error
	}))
	require.NoError(t, db.Exec("INSERT INTO connection_probe VALUES (2, 'still owned')").Error)
	var values []string
	require.NoError(t, db.Table("connection_probe").Order("id").Pluck("value", &values).Error)
	require.Equal(t, []string{"still borrowed", "still owned"}, values)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, pool.PingContext(t.Context()))
}
