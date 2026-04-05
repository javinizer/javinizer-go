# Genre Management

Javinizer Go provides a database-backed genre replacement system to customize genre names from scrapers.

## Overview

Different scrapers may use different genre names for the same concept. Genre replacements allow you to normalize these into your preferred terminology.

### Why Use Genre Replacements?

- **Consistency**: Unify genre names across different scrapers
- **Clarity**: Replace abbreviated or unclear genre names
- **Preference**: Use terminology that makes sense to you
- **Organization**: Better filtering and searching in media libraries

## Commands

### Add Replacement

```bash
javinizer genre add <original> <replacement>
```

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

### Remove Replacement

```bash
javinizer genre remove <original>
```

**Example:**
```bash
javinizer genre remove "Blow"
```

## How It Works

1. **Storage**: Replacements stored in SQLite database (`genre_replacements` table)
2. **Caching**: Loaded into memory when aggregator initializes
3. **Application**: Applied during metadata aggregation, before genre filtering
4. **Persistence**: Survives across restarts, no CSV files needed

### Processing Flow

```
Scraper → Original Genres → Apply Replacements → Apply Ignore Filter → Final Genres
```

Example:
```
R18.dev returns: ["Blow", "Creampie", "Solowork"]
                    ↓ (Apply replacement)
After replacement: ["Blowjob", "Cream Pie", "Solowork"]
                    ↓ (Apply ignore filter)
Final genres: ["Blowjob", "Cream Pie", "Solowork"]
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
1. Apply replacements
2. Apply ignore filter

## Tips

1. **Case-sensitive**: "Blow" and "blow" are different
2. **Exact match**: Partial matches don't work
3. **Update existing**: Running `add` on existing original updates the replacement
4. **Batch setup**: Add all your preferences before processing large libraries
5. **Export/backup**: Database contains all replacements (backup `data/javinizer.db`)

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

The PowerShell version used CSV files (`jvGenres.csv`). To migrate:

### Option 1: Manual Migration

```bash
# For each line in jvGenres.csv:
javinizer genre add "Original" "Replacement"
```

### Option 2: Keep CSV (Not Recommended)

The CSV settings still exist in config but are not used. Genre replacements are now database-only for better performance and reliability.

## Database Details

Genre replacements are stored in the `genre_replacements` table:

```sql
CREATE TABLE genre_replacements (
    id INTEGER PRIMARY KEY,
    original VARCHAR(255) UNIQUE NOT NULL,
    replacement VARCHAR(255) NOT NULL,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

### Manual Database Queries

```bash
# View all replacements (using sqlite3)
sqlite3 data/javinizer.db "SELECT original, replacement FROM genre_replacements;"

# Add via SQL
sqlite3 data/javinizer.db "INSERT INTO genre_replacements (original, replacement, created_at, updated_at) VALUES ('Test', 'TestReplacement', datetime('now'), datetime('now'));"
```

## Troubleshooting

### Replacement Not Applied

1. **Check spelling**: Ensure exact match (case-sensitive)
2. **Verify added**: Run `javinizer genre list`
3. **Re-scrape**: Clear cache and scrape again
4. **Check source**: Verify scraper returns that genre

### Lost Replacements

- Replacements are in the database
- If database deleted, replacements are lost
- Backup `data/javinizer.db` to preserve

### Too Many Replacements

- No limit on number of replacements
- Performance impact is minimal (cached in memory)
- Can remove unused ones with `genre remove`

---

**Next**: [Database Schema](./06-database-schema.md)
