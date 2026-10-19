package r18devdump

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSidecarBinaryIndexNames(t *testing.T) {
	for _, spelling := range []string{"binary", `"binary"`, "[binary]", "`BiNaRy`", "'binary'", "BINARY", "inherited"} {
		t.Run(spelling, func(t *testing.T) {
			path := importFixture(t)
			indexColumn := "dvd_id_norm COLLATE " + spelling
			if spelling == "inherited" {
				path = lookupCollationFixture(t, `dvd_id_norm TEXT COLLATE "binary"`, "")
				indexColumn = "dvd_id_norm"
			}
			alterFixture(t, path, `DROP INDEX idx_videos_dvd_id_norm`, `CREATE INDEX idx_videos_dvd_id_norm ON videos(`+indexColumn+`)`)
			store, err := Open(path)
			require.NoError(t, err)
			var id, parent, unused int
			var plan string
			require.NoError(t, store.db.QueryRow(`EXPLAIN QUERY PLAN SELECT content_id FROM videos WHERE dvd_id_norm = ?`, "IPX535").Scan(&id, &parent, &unused, &plan))
			require.Contains(t, plan, "SEARCH videos USING INDEX idx_videos_dvd_id_norm")
			require.NoError(t, store.Close())
			store, err = ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			require.NoError(t, store.Close())
		})
	}
}

func TestValidateCompoundPrimaryKeyCollations(t *testing.T) {
	for _, withoutRowID := range []bool{false, true} {
		for _, coll := range []string{`"NOCASE"`, "[RTRIM]", "binary", `"BiNaRy"`} {
			t.Run(fmt.Sprintf("%t-%s", withoutRowID, coll), func(t *testing.T) {
				db, err := sql.Open("sqlite3", ":memory:")
				require.NoError(t, err)
				db.SetMaxOpenConns(1)
				defer db.Close()
				ddl := `CREATE TABLE video_actresses(content_id TEXT, actress_id TEXT, PRIMARY KEY(content_id, actress_id COLLATE ` + coll + `))`
				if withoutRowID {
					ddl += " WITHOUT ROWID"
				}
				_, err = db.Exec(ddl)
				require.NoError(t, err)
				err = checkLogicalKey(context.Background(), db, "video_actresses", []string{"content_id", "actress_id"})
				if strings.Contains(strings.ToUpper(coll), "BINARY") {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrDumpInvalid)
				}
			})
		}
	}
}

func lookupCollationFixture(t *testing.T, columnDDL, primaryKey string) string {
	t.Helper()
	path := importFixture(t)
	schema := regexp.MustCompile(`dvd_id_norm\s+TEXT`).ReplaceAllString(tableSchema[derivedVideoTable].create, columnDDL)
	if primaryKey != "" {
		schema = strings.Replace(schema, "PRIMARY KEY", "", 1)
		end := strings.LastIndex(schema, ")")
		schema = schema[:end] + ", " + primaryKey + schema[end:]
	}
	alterFixture(t, path, `ALTER TABLE videos RENAME TO videos_old`, schema,
		`INSERT INTO videos SELECT * FROM videos_old`, `DROP TABLE videos_old`,
		`CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm COLLATE BINARY)`)
	return path
}

func TestValidateSidecarEffectiveLookupCollations(t *testing.T) {
	for _, collation := range []string{`NOCASE`, `"NOCASE"`, `'NOCASE'`, `[NOCASE]`, "`NOCASE`", `/* comment */ NOCASE`} {
		t.Run(collation, func(t *testing.T) {
			path := lookupCollationFixture(t, `dvd_id_norm TEXT COLLATE `+collation, "")
			store, err := ValidateSidecar(context.Background(), path)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorIs(t, err, ErrDumpInvalid)
			require.Contains(t, err.Error(), "COLLATE")
		})
	}
	for _, pk := range []string{`PRIMARY KEY (content_id COLLATE NOCASE)`, `CONSTRAINT "p,k" PRIMARY KEY (content_id COLLATE "NOCASE")`, `PRIMARY KEY (content_id COLLATE [RTRIM])`} {
		t.Run(pk, func(t *testing.T) {
			path := lookupCollationFixture(t, `dvd_id_norm TEXT`, pk)
			store, err := ValidateSidecar(context.Background(), path)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorIs(t, err, ErrDumpInvalid)
		})
	}
}

func TestValidateSidecarCompatibleCollationSyntax(t *testing.T) {
	for _, ddl := range []string{
		`dvd_id_norm TEXT COLLATE "BINARY"`,
		`"dvd_id_norm" TEXT COLLATE [BINARY]`,
		`dvd_id_norm TEXT DEFAULT 'COLLATE NOCASE'`,
		`dvd_id_norm TEXT DEFAULT 'comma,( COLLATE NOCASE )'`,
		`dvd_id_norm TEXT /* COLLATE NOCASE */`,
		`dvd_id_norm TEXT -- COLLATE NOCASE
`,
		`dvd_id_norm TEXT DEFAULT ('escaped '' quote, (' || ')')`,
		`dvd_id_norm TEXT CHECK(length(dvd_id_norm COLLATE NOCASE) > 0)`,
		`dvd_id_norm TEXT COLLATE NOCASE COLLATE BINARY`,
	} {
		t.Run(ddl, func(t *testing.T) {
			path := lookupCollationFixture(t, ddl, "")
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			defer store.Close()
			var id, parent, unused int
			var plan string
			require.NoError(t, store.db.QueryRow(`EXPLAIN QUERY PLAN SELECT content_id FROM videos WHERE dvd_id_norm = ?`, "IPX535").Scan(&id, &parent, &unused, &plan))
			require.Contains(t, plan, "SEARCH videos USING INDEX idx_videos_dvd_id_norm")
		})
	}
}

func TestDDLQuotedAndCommentDelimiters(t *testing.T) {
	require.Equal(t, `a TEXT DEFAULT 'x,(y)', "b,c" TEXT, PRIMARY KEY (a, "b,c")`,
		ddlBody(`CREATE TABLE "t(" /* ignored ( */ (a TEXT DEFAULT 'x,(y)', "b,c" TEXT, PRIMARY KEY (a, "b,c"))`))
	require.Equal(t, []string{`a TEXT DEFAULT 'x,(y)'`, ` "b,c" TEXT`, ` PRIMARY KEY (a, "b,c")`},
		splitColumnsDDL(`a TEXT DEFAULT 'x,(y)', "b,c" TEXT, PRIMARY KEY (a, "b,c")`))
	for _, ddl := range []string{`"content_id" TEXT COLLATE /* comment */ "NOCASE"`, "`content_id` TEXT COLLATE -- comment\n[NOCASE]"} {
		name, coll, ok := segmentColumnAndCollation(ddl)
		require.Equal(t, "content_id", name)
		require.Equal(t, "NOCASE", coll)
		require.True(t, ok)
	}
	for _, ddl := range []string{`content_id TEXT /* COLLATE NOCASE`, "content_id TEXT -- COLLATE NOCASE", `content_id TEXT DEFAULT 'COLLATE NOCASE'`} {
		_, _, ok := segmentColumnAndCollation(ddl)
		require.False(t, ok)
	}
	for _, ddl := range []string{`"unterminated`, `[]`, `''`, `"a""b"`, "`a``b`"} {
		_, end, _, quoted := ddlToken(ddl, 0)
		require.Equal(t, len(ddl), end)
		require.True(t, quoted)
	}
	require.Equal(t, "x", unquoteIdent("x"))
	require.Empty(t, ddlBody(`CREATE TABLE t (missing`))
	require.Empty(t, ddlBody(`CREATE TABLE t )`))
}
