package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type fixtureActor struct {
	ID         uint   `json:"id"`
	DMMID      int    `json:"dmm_id"`
	Name       string `json:"name"`
	BeforeName string `json:"before_name"`
	AfterName  string `json:"after_name"`
	SourceID   string `json:"source_id"`
	SourceKey  string `json:"source_key"`
}
type fixtureDescription struct {
	Actors      map[string]fixtureActor `json:"actors"`
	Digests     map[string]string       `json:"digests"`
	SourceCount int                     `json:"source_count"`
	MovieID     string                  `json:"movie_id"`
}

func provision(root string, db *database.DB) error {
	ctx := context.Background()
	repo := database.NewActressRepository(db)
	f := fixtureDescription{Actors: map[string]fixtureActor{}, Digests: map[string]string{}, SourceCount: 105, MovieID: "sync-browser-film"}
	var dump strings.Builder
	dump.WriteString("COPY public.derived_actress (id, name_kanji, name_romaji) FROM stdin;\n")
	for i, name := range []string{"managed", "managed2", "editable", "unchanged", "manual", "guarded", "unicode"} {
		before := "旧名" + name
		after := "新名" + name
		if name == "unchanged" {
			before = "同じ名前"
			after = before
		}
		if name == "unicode" {
			after = "髙橋さくら・𠮷野🌸"
		}
		actor := models.Actress{DMMID: 700 + i, FirstName: "Fixture " + name, JapaneseName: before, Aliases: "Keep local alias metadata"}
		var err error
		if name == "manual" {
			err = repo.Create(ctx, &actor)
		} else {
			err = repo.ImportUpsert(ctx, &actor)
		}
		if err != nil {
			return err
		}
		if name == "guarded" {
			if err := repo.ProtectOfflineSyncName(ctx, actor.ID); err != nil {
				return err
			}
		}
		id := "r18-" + name
		key, err := actresscache.SourceKey("r18dev", id)
		if err != nil {
			return err
		}
		f.Actors[name] = fixtureActor{ID: actor.ID, DMMID: actor.DMMID, Name: actor.FirstName, BeforeName: before, AfterName: after, SourceID: id, SourceKey: key}
		fmt.Fprintf(&dump, "%s\t%s\tUntrusted source alias %s\n", id, after, name)
	}
	dump.WriteString("r18-same-b\t同じ名前\tDuplicate-looking source\nr18-missing-name\t\\N\tMissing Japanese name\n")
	for i := 0; i < 96; i++ {
		fmt.Fprintf(&dump, "r18-page-%03d\t資料名%03d\tPagination fixture %03d\n", i, i, i)
	}
	dump.WriteString("\\.\n")
	limits := offline.DefaultLimits()
	observed := time.Now().UTC().Add(-time.Minute)
	for _, v := range []struct {
		name   string
		offset time.Duration
	}{{"ready", 0}, {"stale", -48 * time.Hour}, {"future", 24 * time.Hour}} {
		snapshot, receipt, err := r18dump.Convert(ctx, strings.NewReader(dump.String()), actresscache.Provenance{SourceURL: "https://fixtures.invalid/offline-name-sync.sql", ParserRevision: "sync-browser-fixture-v1", ObservedAt: observed.Add(v.offset)}, limits.Source)
		if err != nil {
			return err
		}
		store, err := artifactstore.Open(filepath.Join(root, "artifacts", v.name), limits.Store)
		if err != nil {
			return err
		}
		result, err := store.Publish(ctx, bytes.NewReader(snapshot.Bytes()), receipt)
		closeErr := store.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if !result.Published || !result.FilesSynced || !result.DirectorySynced {
			return errors.New("fixture not durably published")
		}
		f.Digests[v.name] = snapshot.Digest()
	}
	movie := models.Movie{ContentID: f.MovieID, ID: f.MovieID, Title: "Offline sync fixture", RenderGeneration: 9}
	if err := db.Create(&movie).Error; err != nil {
		return err
	}
	id := f.Actors["managed"].ID
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: id, CreditedName: "Original reported name", CreditedJapaneseName: "Original reported Japanese", Source: "dmm", Origin: string(models.CreditOriginScrape), UserOverride: true, OverrideName: "Manual display", Suppressed: true}
	if err := db.Create(&credit).Error; err != nil {
		return err
	}
	if err := db.Create(&models.ActressTranslation{ActressID: id, Language: "en", DisplayName: "Stale translation"}).Error; err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "fixture.json"), b, 0600)
}

// inspect only reads fixed tables in an existing disposable DB. It never starts
// runtime/migrations/auth, and has no HTTP route or user-supplied SQL interface.
func inspect(root string) error {
	info, err := os.Lstat(filepath.Join(root, "names.db"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("inspect requires existing regular database")
	}
	dsn := url.URL{Scheme: "file", Path: filepath.Join(root, "names.db"), RawQuery: "mode=ro&_query_only=1&_busy_timeout=10000"}
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// #nosec G101 -- Fixed read-only SQL projections, not credentials.
	queries := map[string]string{
		"actors":       "SELECT id, dmm_id, first_name, last_name, japanese_name, thumb_url, aliases, verified, origin, name_key, ambiguity_quarantined FROM actresses ORDER BY id",
		"jobs":         "SELECT id, snapshot_digest, cancelled FROM actress_sync_jobs ORDER BY id",
		"tasks":        "SELECT id, job_id, actress_id, source_key, before_name, after_name, status, reason FROM actress_sync_tasks ORDER BY id",
		"bindings":     "SELECT actress_id, source_key, status FROM actress_sync_bindings ORDER BY id",
		"guards":       "SELECT actress_id, protected FROM actress_sync_name_guards ORDER BY actress_id",
		"credits":      "SELECT id, movie_content_id, actress_id, credited_name, credited_japanese_name, source, origin, user_override, override_name, suppressed FROM movie_credits ORDER BY id",
		"translations": "SELECT actress_id, language, display_name FROM actress_translations ORDER BY actress_id, language",
		"aliases":      "SELECT alias_name, canonical_name FROM actress_aliases ORDER BY id",
	}
	out := make(map[string][]map[string]any, len(queries))
	for key, query := range queries {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return err
		}
		out[key] = []map[string]any{}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range ptrs {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				_ = rows.Close()
				return err
			}
			row := map[string]any{}
			for i, col := range cols {
				if b, ok := values[i].([]byte); ok {
					values[i] = string(b)
				}
				row[col] = values[i]
			}
			out[key] = append(out[key], row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		// These fixed SELECTs use single-result SQLite rows. Exhausting Next
		// closes them, and Err above reports any iteration/close failure.
		// Retain the idempotent close without a second, unreachable error path.
		_ = rows.Close()
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
