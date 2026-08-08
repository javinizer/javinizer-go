package database

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWithEditTxHandlesNilDB(t *testing.T) {
	var nilDB *DB
	err := nilDB.WithEditTx(context.Background(), func(u EditUnit) error { return nil })
	require.ErrorIs(t, err, gorm.ErrInvalidDB)
}

func TestWithEditTxRollsBackOnUserError(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	boom := errors.New("client roll")
	err = db.WithEditTx(context.Background(), func(u EditUnit) error { return boom })
	require.ErrorIs(t, err, boom)

	require.NoError(t, db.WithEditTx(context.Background(), func(u EditUnit) error {
		require.NotNil(t, u.Movies)
		require.NotNil(t, u.Actresses)
		require.NotNil(t, u.Jobs)
		return nil
	}))
	assert.True(t, true)
}
