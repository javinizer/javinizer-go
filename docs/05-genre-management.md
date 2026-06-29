# Genre Management

Javinizer Go provides a database-backed genre replacement system to customize genre names from scrapers.

## Overview

Different scrapers may use different genre names for the same concept. Genre replacements allow you to normalize these into your preferred terminology.

### Why Use Genre Replacements?

- **Consistency**: Unify genre names across different scrapers
- **Clarity**: Replace abbreviated or unclear genre names
- **Preference**: Use terminology that makes sense to you
- **Organization**: Better filtering and searching in media libraries

## Configuration

Genre replacement is a SQLite-backed feature toggled under `metadata.genre_replacement` in `configs/config.yaml`:

```yaml
metadata:
  # Genre replacement/normalization database (SQLite-backed)
  genre_replacement:
    enabled: true
    auto_add: true  # Automatically add new genres with identity mapping (genre -> genre)
```

| Key | Default | Description |
|-----|---------|-------------|
| `enabled` | `true` | Master switch. When `false`, genres pass through unchanged even if replacements exist in the database. |
| `auto_add` | `true` | When a scraped genre has no explicit replacement, persist it as an identity mapping (`genre → genre`) so it is cached and ready to edit later via the CLI, API, or SQL. |

Both default to `true`, so replacement is active out of the box. Disable `enabled` to bypass replacement entirely without deleting your mappings.

> **Note:** Genre replacement is distinct from word replacement. Word replacements (`metadata.word_replacement`, disabled by default) do substring search-and-replace across all text fields **and each genre token**, and run *before* genre replacement — see the [`word` command](./03-cli-reference.md#word) and [Metadata Management Defaults](./02-configuration.md#metadata-management-defaults). See [How It Works](#how-it-works) for the full pipeline.

## Commands

The `genre` command manages replacements stored in the database. Run `javinizer genre --help` to see all subcommands.

### Add Replacement

```bash
javinizer genre add <original> <replacement>
```

Upserts a mapping. Running `add` on an `original` that already exists updates its `replacement`.

**Examples:**
```bash
javinizer genre add "Blow" "Blowjob"
javinizer genre add "Creampie" "Cream Pie"
javinizer genre add "Beautiful Girl" "Beauty"
```

### List Replacements

```bash
javinizer genre list
```

**Output:**
```
=== Genre Replacements ===
Original                       → Replacement
-----------------------------------------------------------------
Blow                           → Blowjob
Creampie                       → Cream Pie
Beautiful Girl                 → Beauty

Total: 3 replacements
```

If no replacements are configured, it prints `No genre replacements configured`.

### Remove Replacement

```bash
javinizer genre remove <original>
```

**Example:**
```bash
javinizer genre remove "Blow"
```

### Export Replacements

```bash
javinizer genre export [output.json]
```

Writes all replacements as a JSON array sorted by `original`. With no file argument, the JSON is written to stdout; with an argument, it is written to that file.

```bash
# Print to stdout
javinizer genre export

# Save to a file (e.g. for backup or transfer)
javinizer genre export genre-replacements.json
```

The export format matches the import format — a JSON array of objects with `original` and `replacement` (plus `id`, `created_at`, `updated_at` metadata):

```json
[
  {
    "id": 1,
    "original": "Blow",
    "replacement": "Blowjob",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
]
```

### Import Replacements

```bash
javinizer genre import <input.json>
```

Loads a JSON array of replacements. Only `original` and `replacement` are required; `id` and timestamps are ignored on import.

```json
[
  {"original": "Blow", "replacement": "Blowjob"},
  {"original": "Creampie", "replacement": "Cream Pie"}
]
```

Import is idempotent and merge-friendly:

- **Skipped** — an entry whose `original` *and* `replacement` already match an existing row (no-op).
- **Imported** — a new entry, or an existing `original` whose `replacement` differs (upsert updates the replacement).
- **Errors** — rows that fail to persist (counted and reported; remaining rows still import).

```bash
javinizer genre import genre-replacements.json
# Imported: 2, Skipped: 0, Errors: 0
```

An empty array (`[]`) is rejected with an error; invalid JSON fails with `failed to parse JSON`.

## How It Works

1. **Storage**: Replacements stored in the SQLite database (`genre_replacements` table).
2. **Caching**: Loaded into an in-memory map when the aggregator's genre processor initializes, and reloaded on demand (e.g. after API mutations invalidate the cache).
3. **Application**: Applied per genre token during metadata aggregation, **after** word replacement and **before** the ignore filter. Gated by `metadata.genre_replacement.enabled` — when disabled, tokens pass through unchanged.
4. **Auto-add**: When `auto_add: true`, any token without an explicit mapping is persisted as an identity mapping so it is available to edit later.
5. **Persistence**: Survives across restarts. No CSV files are involved.

### Processing Flow

```
Scraper → Original Genres → Apply Word Replacements → Apply Genre Replacements → Apply Ignore Filter → Final Genres
```

- **Word replacements** (optional, `metadata.word_replacement`) normalize substrings in each token first.
- **Genre replacements** then map the resulting token via an exact, case-sensitive lookup.
- **Ignore filter** (`metadata.ignore_genres`) drops matching tokens last.

Example (with `genre_replacement.enabled: true`):
```
R18.dev returns: ["Blow", "Creampie", "Solowork"]
                    ↓ (Apply word replacements — none here)
After word pass:  ["Blow", "Creampie", "Solowork"]
                    ↓ (Apply genre replacements)
After replacement: ["Blowjob", "Cream Pie", "Solowork"]
                    ↓ (Apply ignore filter — none here)
Final genres:     ["Blowjob", "Cream Pie", "Solowork"]
```

## Common Replacements

### Normalize Abbreviations

```bash
javinizer genre add "3P" "Threesome"
javinizer genre add "4P" "Foursome"
javinizer genre add "POV" "Point of View"
```

### Fix Inconsistent Names

```bash
javinizer genre add "Big Tits" "Big Breasts"
javinizer genre add "Busty" "Big Breasts"
javinizer genre add "Large Breasts" "Big Breasts"
```

### Simplify Long Names

```bash
javinizer genre add "Beautiful Girl" "Beauty"
javinizer genre add "Slender Figure" "Slender"
```

### Personal Preference

```bash
javinizer genre add "Solowork" "Solo"
javinizer genre add "Hi-Vision" "HD"
```

## Combining with Ignore Filter

You can combine genre replacements with the ignore filter in `configs/config.yaml`:

```yaml
metadata:
  ignore_genres:
    - "Uncensored"
    - "VR"
    - "Sample"
```

**Processing order:**
1. Apply word replacements (if enabled)
2. Apply genre replacements
3. Apply ignore filter

> The ignore filter is applied to the *replaced* name, so ignore a genre by the name it becomes after replacement, not the original scraper name.

### Regex Support

`ignore_genres` accepts both exact strings and regex patterns. A pattern is treated as regex when it contains regex metacharacters or `^`/`$` anchors; plain strings use exact, case-sensitive matching. Use `(?i)` for case-insensitive regex.

```yaml
metadata:
  ignore_genres:
    - "Sample"           # Exact match
    - "^Featured"        # Starts with "Featured"
    - ".*mosaic.*"       # Contains "mosaic"
    - "^(HD|4K|VR)"      # Starts with HD, 4K, or VR
    - "(?i)uncensored"   # Case-insensitive match for "uncensored"
```

Invalid regex patterns are skipped with a warning when the genre processor initializes.

## HTTP API

Genre replacements can also be managed through the REST API (authentication required). All routes are prefixed with `/api/v1`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/genres` | List all genres in the database |
| `GET` | `/api/v1/genres/replacements` | List genre replacements |
| `POST` | `/api/v1/genres/replacements` | Create a genre replacement |
| `PUT` | `/api/v1/genres/replacements` | Update a genre replacement |
| `DELETE` | `/api/v1/genres/replacements` | Delete a genre replacement |
| `GET` | `/api/v1/genres/replacements/export` | Export genre replacements |
| `POST` | `/api/v1/genres/replacements/import` | Import genre replacements |

Successful mutations invalidate the aggregator's in-memory cache so changes take effect on the next scrape without a restart. See [API Reference — Genres & Words](./07-api-reference.md#genres--words) for request/response details.

## Tips

1. **Case-sensitive**: "Blow" and "blow" are different — replacements use an exact, case-sensitive lookup.
2. **Exact match**: Partial matches don't work. Replace the full token, or use the regex-capable ignore filter to drop by pattern.
3. **Update existing**: Running `add` on an existing `original` updates the replacement.
4. **Batch setup**: Add all your preferences before processing large libraries, or bulk-load them with `genre import`.
5. **Backup/transfer**: Back up `data/javinizer.db`, or use `javinizer genre export replacements.json` to produce a portable JSON file you can version-control or `genre import` on another machine.

## Workflow Example

### Initial Setup

```bash
# Initialize
javinizer init

# Add preferred genre names
javinizer genre add "Blow" "Blowjob"
javinizer genre add "Creampie" "Cream Pie"
javinizer genre add "3P" "Threesome"
javinizer genre add "Beautiful Girl" "Beauty"

# Verify
javinizer genre list
```

### Bulk Load from JSON

```bash
# From a previously exported file or a hand-authored one
javinizer genre import my-genres.json
# Imported: 4, Skipped: 0, Errors: 0

javinizer genre list
```

### Test with Scraping

```bash
# Scrape a movie
javinizer scrape IPX-535

# Check genres in output - should show replaced names
```

### Apply to Library

```bash
# Process files (replacements applied automatically)
javinizer sort ~/Videos --dry-run

# If satisfied
javinizer sort ~/Videos
```

## Migration from PowerShell Javinizer

The PowerShell version used CSV files (`jvGenres.csv`). Javinizer Go is database-only — there are no CSV settings in the config, so the CSV file is not read directly. Convert the CSV to a JSON array and load it with `genre import`.

### Convert and Import

If `jvGenres.csv` is a two-column `Original,Replacement` file (with a header row), convert it to the import format:

```bash
# Build genres.json from jvGenres.csv (skip the header row)
{
  echo '['
  first=true
  tail -n +2 jvGenres.csv | while IFS=, read -r original replacement; do
    [ -z "$original" ] && continue
    if [ "$first" = true ]; then first=false; else echo ','; fi
    printf '  {"original": "%s", "replacement": "%s"}' "$original" "$replacement"
  done
  echo
  echo ']'
} > genres.json

# Import into the database
javinizer genre import genres.json
```

### Manual Migration (Small Sets)

For a handful of entries, add them directly:

```bash
javinizer genre add "Original" "Replacement"
```

Either way, verify with `javinizer genre list` afterwards.

## Database Details

Genre replacements are stored in the `genre_replacements` table. The schema created by the baseline migration (`internal/database/migrations/000001_baseline.sql`):

```sql
CREATE TABLE IF NOT EXISTS genre_replacements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original TEXT NOT NULL,
    replacement TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_genre_replacements_original ON genre_replacements(original);
```

Uniqueness on `original` is enforced by the `idx_genre_replacements_original` index, which is why `add` upserts rather than duplicating rows.

### Manual Database Queries

```bash
# View all replacements (using sqlite3)
sqlite3 data/javinizer.db "SELECT original, replacement FROM genre_replacements;"

# Add via SQL
sqlite3 data/javinizer.db "INSERT INTO genre_replacements (original, replacement, created_at, updated_at) VALUES ('Test', 'TestReplacement', datetime('now'), datetime('now'));"
```

> Prefer the CLI/API over direct SQL edits — the aggregator caches replacements in memory, and CLI/API mutations invalidate that cache. Direct SQL changes require a restart (or an API call) to be picked up.

## Troubleshooting

### Replacement Not Applied

1. **Check `enabled`**: Confirm `metadata.genre_replacement.enabled: true` in your config.
2. **Check spelling**: Ensure an exact, case-sensitive match.
3. **Verify added**: Run `javinizer genre list`.
4. **Re-scrape**: Clear cache and scrape again.
5. **Check source**: Verify the scraper returns that genre token.

### Lost Replacements

- Replacements live in the database.
- If the database is deleted, replacements are lost.
- Back up `data/javinizer.db`, or run `javinizer genre export replacements.json` periodically.

### Too Many Replacements

- No limit on the number of replacements.
- Performance impact is minimal (cached in memory as a map lookup).
- Remove unused ones with `genre remove`, or edit via the API.

---

**Next**: [Database Schema](./06-database-schema.md)
