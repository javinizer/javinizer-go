# Database Schema

Javinizer Go uses SQLite for caching metadata, storing actress information, and managing genre replacements.

## Database Location

**Default**: `data/javinizer.db`

Configure in `configs/config.yaml`:
```yaml
database:
  type: sqlite
  dsn: data/javinizer.db
```

## Tables

### movies

Stores scraped movie metadata.

| Column | Type | Description |
|--------|------|-------------|
| id | VARCHAR(50) | Primary key (JAV ID) |
| content_id | VARCHAR(50) | Content ID (e.g., ipx00535) |
| title | TEXT | Movie title |
| alternate_title | TEXT | Japanese/alternate title |
| description | TEXT | Plot description |
| release_date | TIMESTAMP | Release date |
| release_year | INTEGER | Extracted year |
| runtime | INTEGER | Runtime in minutes |
| director | VARCHAR(255) | Director name |
| maker | VARCHAR(255) | Studio/maker |
| label | VARCHAR(255) | Label |
| series | VARCHAR(255) | Series name |
| cover_url | TEXT | Cover image URL |
| trailer_url | TEXT | Trailer URL |
| screenshots | TEXT | JSON array of screenshot URLs |
| source_name | VARCHAR(50) | Scraper source |
| source_url | TEXT | Source page URL |
| original_file_name | VARCHAR(255) | Original filename |
| created_at | TIMESTAMP | Record creation time |
| updated_at | TIMESTAMP | Last update time |

### actresses

Stores actress information.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key |
| first_name | VARCHAR(100) | First name |
| last_name | VARCHAR(100) | Last name |
| japanese_name | VARCHAR(255) | Japanese name (indexed) |
| thumb_url | TEXT | Thumbnail URL |
| aliases | TEXT | Pipe-separated alternate names |
| created_at | TIMESTAMP | Record creation |
| updated_at | TIMESTAMP | Last update |

### genres

Stores unique genre names.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key |
| name | VARCHAR(255) | Genre name (unique) |

### movie_actresses

Many-to-many relationship between movies and actresses.

| Column | Type | Description |
|--------|------|-------------|
| movie_id | VARCHAR(50) | Foreign key → movies.id |
| actress_id | INTEGER | Foreign key → actresses.id |

### movie_genres

Many-to-many relationship between movies and genres.

| Column | Type | Description |
|--------|------|-------------|
| movie_id | VARCHAR(50) | Foreign key → movies.id |
| genre_id | INTEGER | Foreign key → genres.id |

### genre_replacements

Stores user-defined genre name replacements.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key |
| original | VARCHAR(255) | Original genre name (unique) |
| replacement | VARCHAR(255) | Replacement genre name |
| created_at | TIMESTAMP | Record creation |
| updated_at | TIMESTAMP | Last update |

### movie_translations

Stores translations for movies (future feature).

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER | Primary key |
| movie_id | VARCHAR(50) | Foreign key → movies.id |
| language | VARCHAR(10) | Language code |
| title | TEXT | Translated title |
| alternate_title | TEXT | Alternate translated title |
| description | TEXT | Translated description |
| created_at | TIMESTAMP | Record creation |
| updated_at | TIMESTAMP | Last update |

## Relationships

```
movies (1) ←→ (N) movie_actresses (N) ←→ (1) actresses
movies (1) ←→ (N) movie_genres (N) ←→ (1) genres
movies (1) ←→ (N) movie_translations
```

## Common Queries

### View All Movies

```sql
SELECT id, title, release_date, maker
FROM movies
ORDER BY release_date DESC;
```

### Movies by Actress

```sql
SELECT m.id, m.title, m.release_date
FROM movies m
JOIN movie_actresses ma ON m.id = ma.movie_id
JOIN actresses a ON ma.actress_id = a.id
WHERE a.japanese_name = '桜空もも'
ORDER BY m.release_date DESC;
```

### Movies by Genre

```sql
SELECT m.id, m.title, m.release_date
FROM movies m
JOIN movie_genres mg ON m.id = mg.movie_id
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
SELECT a.first_name || ' ' || a.last_name AS name, COUNT(*) as movie_count
FROM actresses a
JOIN movie_actresses ma ON a.id = ma.actress_id
GROUP BY a.id
ORDER BY movie_count DESC
LIMIT 10;
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

Database migrations are automatic at startup using versioned Goose migrations embedded in the binary.

- Migrations are applied before normal app startup continues.
- A pre-migration `.backup` snapshot is created when pending migrations exist.
- If migration fails, startup aborts with recovery instructions.

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
