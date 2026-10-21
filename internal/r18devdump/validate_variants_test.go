package r18devdump

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// variantValidationFixture deliberately omits the importer's partial unique
// index. Uploaded sidecars are read-only: validation must prove the stored
// tuples themselves are unique, including tuples whose service is NULL.
func variantValidationFixture(t *testing.T, primaryKey, contentType, serviceType, values string) string {
	t.Helper()
	path := importFixture(t)
	columns := make([]string, 0, len(tableSchema[derivedVideoTable].columns)+2)
	for _, column := range tableSchema[derivedVideoTable].columns {
		typ := "TEXT"
		switch column {
		case contentIDColumn:
			typ = contentType
		case "service_code":
			typ = serviceType
		case "runtime_mins":
			typ = "INTEGER"
		}
		columns = append(columns, column+" "+typ)
	}
	columns = append(columns, "extra_key TEXT")
	if primaryKey != "" {
		columns = append(columns, primaryKey)
	}
	alterFixture(t, path,
		"CREATE TABLE videos_variant ("+strings.Join(columns, ", ")+")",
		"INSERT INTO videos_variant (content_id, service_code, dvd_id_norm, extra_key) VALUES "+values,
		"DROP TABLE videos",
		"ALTER TABLE videos_variant RENAME TO videos",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)",
	)
	return path
}

func TestValidateSidecar_ServiceVariantsAndLegacyKeys(t *testing.T) {
	for _, tc := range []struct {
		name, key, contentType, serviceType, values string
	}{
		{"legacy_null_service", "PRIMARY KEY(content_id)", "TEXT", "TEXT", "('raw001', NULL, '', 'a')"},
		{"legacy_empty_service", "PRIMARY KEY(content_id)", "TEXT", "TEXT", "('raw001', '', '', 'a')"},
		{"legacy_named_service", "PRIMARY KEY(content_id)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a')"},
		{"different_services", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a'), ('raw001', 'rental', '', 'b')"},
		{"service_null_empty_named", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', NULL, '', 'a'), ('raw001', '', '', 'b'), ('raw001', 'digital', '', 'c')"},
		{"case_distinct_content", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a'), ('RAW001', 'digital', '', 'b')"},
		{"case_distinct_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a'), ('raw001', 'DIGITAL', '', 'b')"},
		{"explicit_binary", `PRIMARY KEY(content_id COLLATE "BINARY", service_code COLLATE [BINARY])`, `TEXT COLLATE "binary"`, "TEXT COLLATE [BINARY]", "('raw001', 'digital', '', 'a'), ('raw001', 'rental', '', 'b')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := variantValidationFixture(t, tc.key, tc.contentType, tc.serviceType, tc.values)
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			var indexCount int
			require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('idx_videos_content_id_fold', 'idx_videos_missing_service')").Scan(&indexCount))
			require.Zero(t, indexCount, "validation must not silently migrate a copied sidecar")
			require.NoError(t, store.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
		})
	}
}

func TestValidateSidecar_ServiceVariantMalformedKeys(t *testing.T) {
	for _, tc := range []struct {
		name, key, contentType, serviceType, values, want string
	}{
		{"duplicate_null_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', NULL, '', 'a'), ('raw001', NULL, '', 'b')", "duplicate content_id+service_code"},
		{"null_content", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "(NULL, 'digital', '', 'a')", "NULL content_id"},
		{"null_both", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "(NULL, NULL, '', 'a')", "NULL content_id"},
		{"blank_content", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('', 'digital', '', 'a')", "noncanonical"},
		{"padded_content_null_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "(' raw001', NULL, '', 'a')", "noncanonical"},
		{"control_content_null_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw' || char(0) || '001', NULL, '', 'a')", "noncanonical"},
		{"blob_content", "PRIMARY KEY(content_id, service_code)", "BLOB", "TEXT", "(x'726177303031', 'digital', '', 'a')", "non-TEXT content_id+service_code"},
		{"integer_content", "PRIMARY KEY(content_id, service_code)", "BLOB", "TEXT", "(123, 'digital', '', 'a')", "non-TEXT content_id+service_code"},
		{"blob_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT", "('raw001', x'6469676974616c', '', 'a')", "non-TEXT content_id+service_code"},
		{"integer_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "BLOB", "('raw001', 123, '', 'a')", "non-TEXT content_id+service_code"},
		{"real_service", "PRIMARY KEY(content_id, service_code)", "TEXT", "BLOB", "('raw001', 1.25, '', 'a')", "non-TEXT content_id+service_code"},
		{"legacy_blob_service", "PRIMARY KEY(content_id)", "TEXT", "TEXT", "('raw001', x'6469676974616c', '', 'a')", "non-TEXT service_code"},
		{"legacy_integer_service", "PRIMARY KEY(content_id)", "TEXT", "BLOB", "('raw001', 123, '', 'a')", "non-TEXT service_code"},
		{"reordered_key", "PRIMARY KEY(service_code, content_id)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a')", "primary key does not start with content_id"},
		{"no_primary_key", "", "TEXT", "TEXT", "('raw001', 'digital', '', 'a')", "primary key does not start with content_id"},
		{"no_key_distinct_services", "", "TEXT", "TEXT", "('raw001', 'digital', '', 'a'), ('raw001', 'rental', '', 'b')", "duplicate content_id"},
		{"extra_key_cannot_hide_duplicate_tuple", "PRIMARY KEY(content_id, service_code, extra_key)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a'), ('raw001', 'digital', '', 'b')", "duplicate content_id"},
		{"service_column_nocase", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT COLLATE NOCASE", "('raw001', 'digital', '', 'a')", "service_code declares COLLATE NOCASE"},
		{"service_column_rtrim", "PRIMARY KEY(content_id, service_code)", "TEXT", "TEXT COLLATE RTRIM", "('raw001', 'digital', '', 'a')", "service_code declares COLLATE RTRIM"},
		{"legacy_service_column_nocase", "PRIMARY KEY(content_id)", "TEXT", "TEXT COLLATE NOCASE", "('raw001', NULL, '', 'a')", "service_code declares COLLATE NOCASE"},
		{"service_pk_nocase", "PRIMARY KEY(content_id, service_code COLLATE NOCASE)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a')", "primary key has non-BINARY COLLATE"},
		{"content_pk_rtrim", "PRIMARY KEY(content_id COLLATE RTRIM, service_code)", "TEXT", "TEXT", "('raw001', 'digital', '', 'a')", "primary key has non-BINARY COLLATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := variantValidationFixture(t, tc.key, tc.contentType, tc.serviceType, tc.values)
			store, err := ValidateSidecar(context.Background(), path)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorIs(t, err, ErrDumpInvalid)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCheckVideoLogicalKey_WithoutRowID(t *testing.T) {
	for _, collation := range []string{"BINARY", "NOCASE"} {
		t.Run(collation, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			require.NoError(t, err)
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			_, err = db.Exec("CREATE TABLE videos(content_id TEXT, service_code TEXT, PRIMARY KEY(content_id, service_code COLLATE " + collation + ")) WITHOUT ROWID")
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO videos VALUES ('raw001', 'digital'), ('raw001', 'rental')")
			require.NoError(t, err)
			err = checkVideoLogicalKey(context.Background(), db)
			if collation == "BINARY" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrDumpInvalid)
				require.Contains(t, err.Error(), "primary key has non-BINARY COLLATE")
			}
		})
	}
}

func TestCheckVideoLogicalKey_QueryFailures(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec("CREATE TABLE videos(content_id TEXT PRIMARY KEY)")
	require.NoError(t, err)
	err = checkVideoLogicalKey(context.Background(), db)
	require.ErrorIs(t, err, ErrDumpInvalid)
	require.Contains(t, err.Error(), "service_code type probe")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = checkVideoLogicalKey(ctx, db)
	require.ErrorIs(t, err, ErrDumpInvalid)
	require.Contains(t, err.Error(), "primary-key probe")
}
