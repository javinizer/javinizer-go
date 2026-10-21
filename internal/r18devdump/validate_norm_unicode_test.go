package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNormConsistency_UnicodeAndNullableValues(t *testing.T) {
	const spaces = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	for _, tc := range []struct {
		name      string
		dvd, norm any
		valid     bool
	}{
		{"ascii", " ab-c 01 ", "ABC01", true},
		{"unicode_case", "é-αı01", "ÉΑI01", true},
		{"unicode_whitespace", "ab" + spaces + "-01", "AB01", true},
		{"fullwidth_case_not_ascii_fold", "ａｂ-０１", "ＡＢ０１", true},
		{"combining_marks_not_composed", "e\u0301-01", "E\u030101", true},
		{"zero_width_space_preserved", "ab\u200b-01", "AB\u200b01", true},
		{"nonspace_control_preserved", "ab\x00-01", "AB\x0001", true},
		{"invalid_utf8_matches_go", "ab\xff-01", "AB\ufffd01", true},
		{"null_both", nil, nil, true},
		{"null_dvd_empty_norm", nil, "", true},
		{"empty_dvd_null_norm", "", nil, true},
		{"empty_both", "", "", true},
		{"unicode_whitespace_only_null_norm", spaces, nil, true},
		{"ascii_agreeing_unicode_case_is_corrupt", "é-01", "é01", false},
		{"ascii_agreeing_unicode_space_is_corrupt", "AB\u00a0-01", "AB\u00a001", false},
		{"wrong_unicode_norm", "é-01", "É02", false},
		{"missing_norm", "é-01", nil, false},
		{"empty_wrong_norm", "é-01", "", false},
		{"null_dvd_orphan_norm", nil, "ORPHAN", false},
		{"empty_dvd_orphan_norm", "", "ORPHAN", false},
		{"stored_norm_must_not_be_renormalized", "ab-01", "ab-01", false},
		{"invalid_utf8_wrong_norm", "ab\xff-01", "AB\xff01", false},
		{"matching_blob_norm", "ab-01", []byte("AB01"), false},
		{"empty_blob_norm", "", []byte{}, false},
		{"matching_integer_norm", "123", int64(123), false},
		{"matching_real_norm", "1.25", float64(1.25), false},
		{"source_blob_text_norm", []byte("ab-01"), "AB01", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			require.NoError(t, err)
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			// No affinity: malformed sidecars can preserve numeric/BLOB storage
			// classes that sql.NullString alone would silently turn into text.
			_, err = db.Exec("CREATE TABLE videos(dvd_id, dvd_id_norm)")
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO videos VALUES (?, ?)", tc.dvd, tc.norm)
			require.NoError(t, err)
			err = validateNormConsistency(context.Background(), db)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrDumpInvalid)
				require.Contains(t, err.Error(), "1 videos whose dvd_id_norm disagrees")
			}
		})
	}
}

func TestValidateSidecar_ImportedUnicodeDVDIDs(t *testing.T) {
	var dump strings.Builder
	dump.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	ids := []string{"é-01", "ab\u00a0-02", "ａｂ-０３", "\u3000abc-04\u3000", "αı-05"}
	// The five mismatches in the complete official 2026-10-20 dump all end
	// in U+3000 IDEOGRAPHIC SPACE. Keep their exact published DVD spellings.
	ids = append(ids, "FA-108\u3000", "HS-105\u3000", "MVD-84\u3000", "MVD-85\u3000", "VF-49\u3000")
	for i, id := range ids {
		fmt.Fprintf(&dump, "unicode%03d\t%s\n", i, id)
	}
	dump.WriteString("\\.\n")
	path := filepath.Join(t.TempDir(), "unicode.db")
	_, err := Import(context.Background(), strings.NewReader(dump.String()), path, ImportOptions{
		SourceURL: "https://example.invalid/unicode.sql", RequireAllRows: true,
	})
	require.NoError(t, err)
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	for i, id := range ids {
		cid, err := store.LookupByDVDID(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("unicode%03d", i), cid)
	}
}

func TestValidateSidecar_OfficialUnicodeDVDIDProjection(t *testing.T) {
	data, err := os.ReadFile("testdata/unicode_normalization_2026-10-20.sql")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "official-unicode.db")
	result, err := Import(context.Background(), strings.NewReader(string(data)), path, ImportOptions{
		SourceURL:      "https://r18dotdev.s3.eu-west-1.wasabisys.com/dumps/r18dotdev_dump_2026-10-20.sql.gz",
		RequireAllRows: true,
	})
	require.NoError(t, err)
	require.EqualValues(t, 5, result.Rows)
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	defer store.Close()
	for _, tc := range []struct{ contentID, display string }{
		{"49fa108", "FA-108"}, {"49hs105", "HS-105"}, {"49mvd84", "MVD-84"},
		{"49mvd85", "MVD-85"}, {"49vf49", "VF-49"},
	} {
		var source, norm string
		require.NoError(t, store.db.QueryRow("SELECT dvd_id, dvd_id_norm FROM videos WHERE content_id = ?", tc.contentID).Scan(&source, &norm))
		require.Equal(t, tc.display+"\u3000", source, "preserve the exact source DVD ID")
		require.Equal(t, strings.ReplaceAll(tc.display, "-", ""), norm)
		for _, query := range []string{tc.display, tc.display + "\u3000"} {
			cid, err := store.LookupByDVDID(context.Background(), query)
			require.NoError(t, err)
			require.Equal(t, tc.contentID, cid)
			movie, err := store.LookupMovie(context.Background(), query)
			require.NoError(t, err)
			require.Equal(t, tc.contentID, movie.ContentID)
			require.Equal(t, source, movie.DVDID)
		}
	}
}

type normValidationFaultDriver struct{ mode string }
type normValidationFaultConn struct{ mode string }
type normValidationFaultRows struct {
	mode string
	read bool
}

func (d *normValidationFaultDriver) Open(string) (driver.Conn, error) {
	return &normValidationFaultConn{mode: d.mode}, nil
}
func (*normValidationFaultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused prepare")
}
func (*normValidationFaultConn) Close() error { return nil }
func (*normValidationFaultConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unused transaction")
}
func (c *normValidationFaultConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.mode == "query" {
		return nil, errors.New("synthetic norm query failure")
	}
	return &normValidationFaultRows{mode: c.mode}, nil
}
func (r *normValidationFaultRows) Columns() []string {
	if r.mode == "scan" {
		return []string{"dvd_id"}
	}
	return []string{"dvd_id", "dvd_id_norm", "norm_type"}
}
func (*normValidationFaultRows) Close() error { return nil }
func (r *normValidationFaultRows) Next(dest []driver.Value) error {
	if !r.read {
		r.read = true
		dest[0] = "AB-01"
		if r.mode != "scan" {
			dest[1] = "AB01"
			dest[2] = "text"
		}
		return nil
	}
	if r.mode == "iterate" {
		return errors.New("synthetic norm iteration failure")
	}
	return io.EOF
}

func TestValidateNormConsistency_Faults(t *testing.T) {
	for _, mode := range []string{"query", "scan", "iterate", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			name := "norm-validation-" + mode
			sql.Register(name, &normValidationFaultDriver{mode: mode})
			db, err := sql.Open(name, "")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err = validateNormConsistency(ctx, db)
			require.ErrorIs(t, err, ErrDumpInvalid)
			require.Contains(t, err.Error(), "norm consistency probe")
			if mode == "scan" || mode == "iterate" {
				require.Contains(t, err.Error(), mode)
			}
		})
	}
}
