package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Direct stage calls with a pre-cancelled context deterministically cover
// every stage's transport-error branch (the first DB touch fails instantly).
func TestValidateStages_CancelledContext(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stages := []struct {
		name string
		fn   func(context.Context, *sql.DB) error
	}{
		{"provenance", validateProvenance},
		{"noNullMetaKeys", validateNoNullMetaKeys},
		{"logicalKeys", validateLogicalKeys},
		{"normConsistency", validateNormConsistency},
		{"columnTypes", validateColumnTypes},
		{"structure", validateStructure},
		{"indexes", validateIndexes},
		{"nonEmpty", validateNonEmpty},
		{"integrity", validateIntegrity},
	}
	for _, s := range stages {
		t.Run(s.name, func(t *testing.T) {
			err := s.fn(ctx, db)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDumpInvalid)
		})
	}
}

func TestOpenContext_CancelledPing(t *testing.T) {
	// Needs an openable database so Open succeeds and the cancelled-context
	// ping is the failing operation.
	path := importFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := OpenContext(ctx, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping")
}

func TestOpenContext_MissingFilePassthrough(t *testing.T) {
	// Open (mode=ro) fails cleanly for a missing file; OpenContext surfaces it.
	_, err := OpenContext(context.Background(), filepath.Join(t.TempDir(), "missing", "none.db"))
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrDumpInvalid), "Open passthrough keeps its raw error shape")
}

func TestValidateStructure_MissingTableAndColumnPath(t *testing.T) {
	// Empty database: first required table is simply absent.
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "empty.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	err = validateStructure(context.Background(), db)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "missing required table")

	// Column probe failure at the column stage (first column query).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = validateColumns(ctx, db, "videos", []string{"content_id"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "column probe")
}

// Videos as a VIEW stays readable (single-row count never fails), so the
// error path for a column probe uses a cancelled context; advisory locking
// semantics differ per driver so we keep it ctx-driven.
func TestValidateSidecar_CorruptIntegrityRejected(t *testing.T) {
	path := importFixture(t)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(data), 5000)
	// Flip bytes deep inside the database to break integrity for quick_check.
	tampered := append([]byte{}, data...)
	for i := 4096; i < 4160; i++ {
		tampered[i] ^= 0xFF
	}
	dst := filepath.Join(t.TempDir(), "corrupt.db")
	require.NoError(t, os.WriteFile(dst, tampered, 0o600))
	_, err = ValidateSidecar(context.Background(), dst)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.True(t, strings.Contains(err.Error(), "integrity") || strings.Contains(err.Error(), "quick_check") || strings.Contains(err.Error(), "unreadable") || strings.Contains(err.Error(), "probe"), err.Error())
}
