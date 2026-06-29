# Database Schema

Javinizer Go uses SQLite to cache scraped movie metadata, store actress and genre information, manage replacement/translation tables, record operation history and batch jobs, emit structured events, and authenticate API tokens. The schema is created and evolved by versioned migrations applied automatically at startup (see [Migration](#migration)).

## Database Location

**Default**: `data/javinizer.db`

Configure in `configs/config.yaml`:

```yaml
database:
  type: sqlite
  dsn: data/javinizer.db
  log_level: silent
```

- **type**: Database backend. Currently only `sqlite` is supported.
- **dsn**: SQLite connection string / file path. Use `:memory:` for an ephemeral in-process database (useful for tests).
- **log_level**: GORM query logging verbosity — `silent`, `error`, `warn`, or `info` (default `silent`).

The database path can also be overridden with the `JAVINIZER_DB` environment variable.

## Tables

The schema contains 18 tables, grouped below by purpose. Column types reflect the DDL in `internal/database/migrations/`. SQLite is dynamically typed, but the documented types are the ones the migrations declare.

### Core metadata

#### movies

Stores scraped movie metadata. The primary key is `content_id` (the normalized content ID, e.g. `ipx00535`); `id` is the original JAV ID (e.g. `IPX-535`) held as a plain indexed column.

| Column | Type | Description |
|--------|------|-------------|
| content_id | TEXT | **Primary key**. Normalized content ID (e.g. `ipx00535`) |
| id | TEXT | Original JAV ID (e.g. `IPX-535`); indexed via `idx_movies_id` |
| display_title | TEXT | Display title (renamed from `display_name` in migration 000003) |
| title | TEXT | Movie title |
| original_title | TEXT | Japanese / original-language title |
| description | TEXT | Plot description |
| release_date | DATETIME | Release date |
| release_year | INTEGER | Extracted release year |
| runtime | INTEGER | Runtime in minutes |
| director | TEXT | Director name |
| maker | TEXT | Studio / maker |
| label | TEXT | Label |
| series | TEXT | Series name |
| rating_score | REAL | Aggregate rating score |
| rating_votes | INTEGER | Number of ratings contributing to the score |
| poster_url | TEXT | Primary poster image URL |
| cover_url | TEXT | Cover image URL |
| cropped_poster_url | TEXT | Cropped poster image URL |
| should_crop_poster | NUMERIC | Whether the poster should be cropped |
| trailer_url | TEXT | Trailer URL |
| original_file_name | TEXT | Original source filename |
| screenshots | TEXT | JSON array of screenshot URLs |
| source_name | TEXT | Scraper source that produced the row |
| source_url | TEXT | Source page URL |
| original_poster_url | TEXT | Unmodified downloaded poster URL (before cropping) — migration 000005 |
| original_cropped_poster_url | TEXT | Unmodified cropped poster URL — migration 000005 |
| original_should_crop_poster | NUMERIC | Original crop flag — migration 000005 |
| original_cover_url | TEXT | Unmodified downloaded cover URL — migration 000011 |
| created_at | DATETIME | Record creation time |
| updated_at | DATETIME | Last update time |

Indexes: `idx_movies_content_id` (unique), `idx_movies_id`. The `original_*` poster/cover columns preserve the raw downloaded assets so re-processing can redo crops without re-downloading.

#### actresses

Stores actress information.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| dmm_id | INTEGER | DMM actress ID; unique where `> 0` (`idx_actresses_dmm_id_positive`) |
| first_name | TEXT | First name (romanized) |
| last_name | TEXT | Last name (romanized) |
| japanese_name | TEXT | Japanese name; indexed via `idx_actresses_japanese_name` |
| thumb_url | TEXT | Thumbnail URL |
| aliases | TEXT | Pipe-separated alternate names |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Index: `idx_actresses_dmm_id_positive` — a partial unique index on `dmm_id WHERE dmm_id > 0`, so actresses without a known DMM ID can coexist while DMM-linked actresses remain unique.

#### genres

Stores unique genre names.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| name | TEXT | Genre name; unique via `idx_genres_name` |

### Association tables

#### movie_actresses

Many-to-many relationship between movies and actresses. Both columns form the primary key; the movie side references `movies(content_id)`.

| Column | Type | Description |
|--------|------|-------------|
| movie_content_id | TEXT | Foreign key → `movies(content_id)` |
| actress_id | INTEGER | Foreign key → `actresses(id)` |

Primary key: `(movie_content_id, actress_id)`. Foreign keys: `fk_movie_actresses_movie`, `fk_movie_actresses_actress`.

#### movie_genres

Many-to-many relationship between movies and genres. Both columns form the primary key; the movie side references `movies(content_id)`.

| Column | Type | Description |
|--------|------|-------------|
| movie_content_id | TEXT | Foreign key → `movies(content_id)` |
| genre_id | INTEGER | Foreign key → `genres(id)` |

Primary key: `(movie_content_id, genre_id)`. Foreign keys: `fk_movie_genres_movie`, `fk_movie_genres_genre`.

#### movie_tags

User-applied tags per movie. `movie_id` is a logical reference to a movie's `content_id`; there is no database-level foreign key, so tags survive even if the referenced movie row is removed.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| movie_id | TEXT | Logical reference to `movies.content_id` (no FK) |
| tag | TEXT | Tag value |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Index: `idx_movie_tag` — unique on `(movie_id, tag)`.

### Replacements, aliases, and ID mappings

#### genre_replacements

User-defined genre name replacements.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| original | TEXT | Original genre name; unique via `idx_genre_replacements_original` |
| replacement | TEXT | Replacement genre name |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

#### word_replacements

User-defined word/phrase replacements applied during title/description normalization. Created in migration 000006.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| original | TEXT | Original word; unique via `idx_word_replacements_original` |
| replacement | TEXT | Replacement word |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

#### actress_aliases

Maps actress alias names to canonical names (used to deduplicate actresses across scrapers).

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| alias_name | TEXT | Alias name; unique via `idx_actress_aliases_alias_name` |
| canonical_name | TEXT | Canonical name; indexed via `idx_actress_aliases_canonical_name` |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

#### content_id_mappings

Caches the resolution from a search ID (the string a user scanned/matched) to a normalized `content_id` for a given source, so repeated lookups skip re-searching.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| search_id | TEXT | Search input; unique via `idx_content_id_mappings_search_id` |
| content_id | TEXT | Resolved content ID |
| source | TEXT | Scraper source that resolved it |
| created_at | DATETIME | Record creation |

### Translations

#### movie_translations

Per-language translations of movie fields. The `movie_id` column is a foreign key to `movies(content_id)`; `(movie_id, language)` is unique.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| movie_id | TEXT | Foreign key → `movies(content_id)` |
| language | TEXT | Language code (e.g. `en`) |
| title | TEXT | Translated title |
| original_title | TEXT | Original-language title |
| description | TEXT | Translated description |
| director | TEXT | Translated director |
| maker | TEXT | Translated maker |
| label | TEXT | Translated label |
| series | TEXT | Translated series |
| source_name | TEXT | Translation source label, formatted as `translation:<provider>` (e.g. `translation:deepl`, `translation:google`) |
| settings_hash | VARCHAR(16) | Hash of the translation settings used to produce this row |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Foreign key: `fk_movies_translations` → `movies(content_id)`. Index: `idx_movie_language` — unique on `(movie_id, language)`.

#### genre_translations

Per-language translations of genre names. Created in migration 000009.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| genre_id | INTEGER | Foreign key → `genres(id)`, `ON DELETE CASCADE` |
| language | TEXT | Language code |
| name | TEXT | Translated genre name |
| source_name | TEXT | Translation source |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Foreign key: `fk_genre_translations_genre`. Index: `idx_genre_translations_genre_language` — unique on `(genre_id, language)`.

#### actress_translations

Per-language translations of actress names. Created in migration 000009.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| actress_id | INTEGER | Foreign key → `actresses(id)`, `ON DELETE CASCADE` |
| language | TEXT | Language code |
| first_name | TEXT | Translated first name |
| last_name | TEXT | Translated last name |
| japanese_name | TEXT | Japanese name |
| display_name | TEXT | Translated display name |
| source_name | TEXT | Translation source |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Foreign key: `fk_actress_translations_actress`. Index: `idx_actress_translations_actress_language` — unique on `(actress_id, language)`.

### Operations, jobs, and events

#### history

Append-only log of operations performed on movies (scrape, download, organize, etc.).

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| movie_id | TEXT | Movie the operation targeted (logical reference; no FK) |
| operation | TEXT | Operation type (e.g. `scrape`, `organize`, `download`) |
| original_path | TEXT | Path before the operation |
| new_path | TEXT | Path after the operation |
| status | TEXT | Operation status (e.g. `success`, `failed`, `reverted`) |
| error_message | TEXT | Error text on failure |
| metadata | JSON | Structured operation metadata |
| dry_run | NUMERIC | Whether the operation was a dry run |
| created_at | DATETIME | Record creation |
| batch_job_id | TEXT | Links to `jobs.id` for batch operations (migration 000004); indexed |

Indexes: `idx_history_movie_id`, `idx_history_created_at`, `idx_history_batch_job_id`.

#### jobs

Batch processing jobs (e.g. sorting a directory of files). State is serialized as JSON in several TEXT columns.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT | **Primary key** |
| status | TEXT | Job status; indexed via `idx_jobs_status` |
| total_files | INTEGER | Total files in the job |
| completed | INTEGER | Files completed (default 0) |
| failed | INTEGER | Files failed (default 0) |
| progress | REAL | Progress fraction 0–1 (default 0) |
| destination | TEXT | Output destination (default `''`) |
| temp_dir | TEXT | Temporary working directory (default `data/temp`; migration 000002) |
| files | TEXT | JSON array of input files |
| results | TEXT | JSON object of per-file results (default `{}`) |
| excluded | TEXT | JSON object of excluded files (default `{}`) |
| file_match_info | TEXT | JSON object of file-match diagnostics (default `{}`) |
| started_at | DATETIME | Job start time; indexed via `idx_jobs_started_at` |
| completed_at | DATETIME | Job completion time |
| organized_at | DATETIME | When organize phase completed |
| reverted_at | DATETIME | When the batch was reverted (migration 000004) |
| update | BOOLEAN | Whether the job is an update pass (default `false`; migration 000008) |
| operation_mode_override | TEXT | Per-job operation-mode override (default `''`; migration 000010) |

Indexes: `idx_jobs_status`, `idx_jobs_started_at`.

#### batch_file_operations

Per-file details for batch organize operations, used to drive revert. Created in migration 000004.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| batch_job_id | TEXT | Foreign key → `jobs(id)` |
| movie_id | TEXT | Movie the file mapped to |
| original_path | TEXT | Path before the operation |
| new_path | TEXT | Path after the operation |
| operation_type | TEXT | Operation type (default `move`) |
| nfo_snapshot | TEXT | JSON snapshot of the NFO used for revert |
| generated_files | TEXT | JSON list of generated sidecar files |
| revert_status | TEXT | Revert state (default `applied`) |
| reverted_at | DATETIME | When this file was reverted |
| in_place_renamed | NUMERIC | Whether the file was renamed in place (default 0) |
| original_dir_path | TEXT | Original directory path |
| nfo_path | TEXT | NFO file path |
| created_at | DATETIME | Record creation |
| updated_at | DATETIME | Last update |

Foreign key: `fk_bfo_batch_job_id` → `jobs(id)`. Indexes: `idx_bfo_batch_job_id`, `idx_bfo_batch_job_revert_status` on `(batch_job_id, revert_status)`.

#### events

Structured event log independent of operation history (used by the API events endpoint). Created in migration 000004.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key (autoincrement) |
| event_type | TEXT | Event type |
| severity | TEXT | Severity level |
| message | TEXT | Human-readable message |
| context | TEXT | JSON context payload |
| source | TEXT | Emitting component |
| created_at | DATETIME | Event time |

Indexes: `idx_events_type`, `idx_events_severity`, `idx_events_created_at`, `idx_events_type_severity`, `idx_events_source`, `idx_events_type_source`.

### Authentication

#### api_tokens

API tokens used to authenticate REST/WebSocket requests. Created in migration 000007.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT | **Primary key** |
| name | TEXT | Human-readable label (default `''`) |
| token_hash | TEXT | Hashed token; unique via `idx_api_tokens_token_hash` |
| token_prefix | TEXT | Stored prefix for display; indexed |
| last_used_at | DATETIME | Last use time |
| created_at | DATETIME | Creation time |
| revoked_at | DATETIME | Revocation time; indexed |

Indexes: `idx_api_tokens_token_hash` (unique), `idx_api_tokens_token_prefix`, `idx_api_tokens_revoked_at`.

## Relationships

```
Core metadata
  movies (PK: content_id) ─┬─ (1:N) movie_actresses (N:1) ── actresses (PK: id)
                            ├─ (1:N) movie_genres     (N:1) ── genres (PK: id)
                            ├─ (1:N) movie_translations (FK: movie_id → movies.content_id)
                            └─ (logical) movie_tags (movie_id ↔ movies.content_id, no FK)

Replacements / aliases / mappings
  genre_replacements    (standalone)
  word_replacements     (standalone)
  actress_aliases       (canonical_name → actresses, logical)
  content_id_mappings   (standalone cache)

Translations
  genres    (1:N) genre_translations   (FK: genre_id → genres.id, ON DELETE CASCADE)
  actresses (1:N) actress_translations (FK: actress_id → actresses.id, ON DELETE CASCADE)

Operations / jobs / events
  jobs (PK: id) ─┬─ (1:N) batch_file_operations (FK: batch_job_id → jobs.id)
                 └─ (1:N) history               (history.batch_job_id → jobs.id, logical)
  events (standalone)

Authentication
  api_tokens (standalone)
```

Foreign keys to `movies` all target `content_id` (the primary key), not `id`.

## Common Queries

### View All Movies

```sql
SELECT content_id, id, title, release_date, maker
FROM movies
ORDER BY release_date DESC;
```

### Movies by Actress

```sql
SELECT m.content_id, m.id, m.title, m.release_date
FROM movies m
JOIN movie_actresses ma ON m.content_id = ma.movie_content_id
JOIN actresses a ON ma.actress_id = a.id
WHERE a.japanese_name = '桜空もも'
ORDER BY m.release_date DESC;
```

### Movies by Genre

```sql
SELECT m.content_id, m.id, m.title, m.release_date
FROM movies m
JOIN movie_genres mg ON m.content_id = mg.movie_content_id
JOIN genres g ON mg.genre_id = g.id
WHERE g.name = 'Solowork'
ORDER BY m.release_date DESC;
```

### Genre Replacements

```sql
SELECT original, replacement
FROM genre_replacements
ORDER BY original;
```

### Top Actresses by Movie Count

```sql
SELECT a.first_name || ' ' || a.last_name AS name, COUNT(*) AS movie_count
FROM actresses a
JOIN movie_actresses ma ON a.id = ma.actress_id
GROUP BY a.id
ORDER BY movie_count DESC
LIMIT 10;
```

### Tags for a Movie

```sql
SELECT tag FROM movie_tags
WHERE movie_id = 'ipx00535'
ORDER BY tag;
```

### Translations for a Movie

```sql
SELECT language, title, source_name, settings_hash
FROM movie_translations
WHERE movie_id = 'ipx00535'
ORDER BY language;
```

### Recent Operation History

```sql
SELECT movie_id, operation, status, new_path, created_at, batch_job_id
FROM history
ORDER BY created_at DESC
LIMIT 50;
```

### Revertible Batch Operations

```sql
SELECT batch_job_id, movie_id, original_path, new_path, revert_status, created_at
FROM batch_file_operations
WHERE revert_status = 'applied'
ORDER BY created_at DESC;
```

### Recent Events

```sql
SELECT event_type, severity, message, source, created_at
FROM events
ORDER BY created_at DESC
LIMIT 50;
```

## Backup and Restore

### Backup

```bash
# Copy database file
cp data/javinizer.db data/javinizer.db.backup

# Or use sqlite3
sqlite3 data/javinizer.db ".backup data/javinizer-backup.db"
```

### Restore

```bash
# Copy backup over current
cp data/javinizer.db.backup data/javinizer.db

# Or use sqlite3
sqlite3 data/javinizer.db ".restore data/javinizer-backup.db"
```

### Export to SQL

```bash
sqlite3 data/javinizer.db .dump > javinizer-export.sql
```

### Import from SQL

```bash
sqlite3 data/javinizer-new.db < javinizer-export.sql
```

## Maintenance

### Database Size

```bash
# Check size
ls -lh data/javinizer.db

# Compact database
sqlite3 data/javinizer.db "VACUUM;"
```

### Clear Cache

```bash
# Delete database (will be recreated on next init)
rm data/javinizer.db
javinizer init
```

## Migration

Database migrations are automatic at startup using versioned [Goose](https://github.com/pressly/goose) migrations embedded in the binary via `//go:embed` (`internal/database/migrations/*.sql`).

- Migrations are applied before normal app startup continues.
- When pending migrations exist, a pre-migration `.backup` snapshot is created in the database directory using SQLite `VACUUM INTO` (file name `javinizer.db.<UTC-timestamp>.backup`). In-memory (`:memory:`) databases skip the snapshot.
- If a migration fails, startup aborts with an error message that includes the backup path and the `cp` command to restore it.
- Migration files follow `NNNNNN_description.sql` and each contains an `Up` and `Down` block.

## Direct Access

### Using sqlite3 CLI

```bash
# Open database
sqlite3 data/javinizer.db

# List tables
.tables

# Describe table
.schema movies

# Run query
SELECT * FROM movies LIMIT 5;

# Exit
.quit
```

### Using GUI Tools

- **DB Browser for SQLite**: https://sqlitebrowser.org/
- **DBeaver**: https://dbeaver.io/
- **DataGrip**: https://www.jetbrains.com/datagrip/

---

**Return to**: [Getting Started](./01-getting-started.md)
