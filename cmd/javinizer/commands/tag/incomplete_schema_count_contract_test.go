package tag_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This deliberately incompatible owned catalog models an interrupted/manual
// schema change. It is not a supported schema: readable names must not make
// failed per-tag counts look like zero, and listing must not repair source data.
func TestTagNamesRemainVisibleWhenLegacySchemaCannotCountMovies(t *testing.T) {
	dir, err := filepath.Abs(t.TempDir())
	require.NoError(t, err)
	t.Setenv("JAVINIZER_DB", filepath.Join(dir, "catalog.sqlite"))
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "catalog.sqlite")
	cfg.Database.LogLevel = "silent"
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	cfg, err = config.LoadOrCreate(path)
	require.NoError(t, err)
	configBefore, err := os.ReadFile(path)
	require.NoError(t, err)
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	movies := database.NewMovieRepository(deps.DB)
	tags := database.NewMovieTagRepository(deps.DB)
	for _, id := range []string{"owned-tag-count-a", "owned-tag-count-b"} {
		require.NoError(t, movies.Create(t.Context(), &models.Movie{ContentID: id, ID: id, Title: id}))
	}
	require.NoError(t, tags.AddTag(t.Context(), "owned-tag-count-a", "Alpha"))
	require.NoError(t, tags.AddTag(t.Context(), "owned-tag-count-b", "Alpha"))
	require.NoError(t, tags.AddTag(t.Context(), "owned-tag-count-a", "Beta"))
	pool, err := deps.DB.DB.DB()
	require.NoError(t, err)
	snapshot := func(query string) [][]string {
		t.Helper()
		rows, err := pool.QueryContext(t.Context(), query)
		require.NoError(t, err)
		defer rows.Close()
		columns, err := rows.Columns()
		require.NoError(t, err)
		var out [][]string
		for rows.Next() {
			values := make([]sql.NullString, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			require.NoError(t, rows.Scan(dest...))
			line := make([]string, len(values))
			for i, v := range values {
				line[i] = fmt.Sprintf("%t:%s", v.Valid, v.String)
			}
			out = append(out, line)
		}
		require.NoError(t, rows.Err())
		return out
	}
	const schemaQuery = "SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name"
	const movieQuery = "SELECT * FROM movies ORDER BY content_id"
	tagQuery := func(column string) string {
		return "SELECT id," + column + ",tag,created_at,updated_at FROM movie_tags ORDER BY id"
	}
	originalSchema := snapshot(schemaQuery)
	// SQLite may quote a restored identifier during ALTER TABLE; compare its
	// semantic spelling while retaining every other schema byte and index.
	canonicalSchema := func(rows [][]string) [][]string {
		out := make([][]string, len(rows))
		for i, row := range rows {
			out[i] = append([]string(nil), row...)
			for j, v := range out[i] {
				out[i][j] = strings.ReplaceAll(v, `"movie_id"`, "movie_id")
			}
		}
		return out
	}
	originalTags := snapshot(tagQuery("movie_id"))
	originalMovies := snapshot(movieQuery)
	require.Len(t, originalTags, 3)
	require.NoError(t, deps.DB.Exec("ALTER TABLE movie_tags RENAME COLUMN movie_id TO fixture_legacy_movie_id").Error)
	restored := false
	t.Cleanup(func() {
		if !restored {
			require.NoError(t, deps.DB.Exec("ALTER TABLE movie_tags RENAME COLUMN fixture_legacy_movie_id TO movie_id").Error)
		}
	})
	brokenSchema := snapshot(schemaQuery)
	require.NotEqual(t, originalSchema, brokenSchema)
	require.Equal(t, originalTags, snapshot(tagQuery("fixture_legacy_movie_id")), "renaming preserves every source row")
	names, err := tags.GetUniqueTagsList(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha", "Beta"}, names)
	_, err = tags.GetMoviesWithTag(t.Context(), "Alpha")
	require.ErrorContains(t, err, "no such column: mt.movie_id")

	logPath := filepath.Join(dir, "count-warning.jsonl")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "warn", Format: "json", Output: logPath}))
	// This serial package uses the documented default logger after each capture;
	// no API promises arbitrary previous singleton restoration.
	t.Cleanup(func() { logging.CloseLogger(); require.NoError(t, logging.InitLogger(nil)) })
	output := executeTagCommand(t, path, "tags")
	logging.CloseLogger()
	require.Contains(t, output, "=== All Tags ===")
	require.Contains(t, output, "Total: 2 unique tags")
	require.Equal(t, 2, strings.Count(output, "(error)"))
	for _, name := range names {
		require.Contains(t, output, fmt.Sprintf("  - %-30s (error)", name))
	}
	require.NotContains(t, output, "(0 movies)")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	var warnings []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if strings.HasPrefix(event.Message, "Failed to count movies for tag '") {
			require.Equal(t, "warning", event.Level)
			require.Contains(t, event.Message, "no such column: mt.movie_id")
			warnings = append(warnings, event.Message)
		}
	}
	require.Len(t, warnings, 2)
	for i, name := range names {
		require.Contains(t, warnings[i], "tag '"+name+"'")
	}
	require.Equal(t, brokenSchema, snapshot(schemaQuery))
	require.Equal(t, originalTags, snapshot(tagQuery("fixture_legacy_movie_id")))
	require.Equal(t, originalMovies, snapshot(movieQuery))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, configBefore, after)

	require.NoError(t, deps.DB.Exec("ALTER TABLE movie_tags RENAME COLUMN fixture_legacy_movie_id TO movie_id").Error)
	restored = true
	require.Equal(t, canonicalSchema(originalSchema), canonicalSchema(snapshot(schemaQuery)), "restore column and index definitions, allowing SQLite identifier quoting")
	require.NoError(t, logging.InitLogger(nil))
	retry := executeTagCommand(t, path, "tags")
	require.NotContains(t, retry, "(error)")
	require.Contains(t, retry, fmt.Sprintf("  - %-30s (2 movies)", "Alpha"))
	require.Contains(t, retry, fmt.Sprintf("  - %-30s (1 movies)", "Beta"))
	require.Equal(t, originalTags, snapshot(tagQuery("movie_id")))
	require.Equal(t, originalMovies, snapshot(movieQuery))
}
