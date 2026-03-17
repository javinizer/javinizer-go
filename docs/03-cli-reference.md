# CLI Reference

Complete command-line interface reference for Javinizer Go.

## Table of Contents

- [Global Flags](#global-flags)
- [Commands](#commands)
  - [init](#init)
  - [scrape](#scrape)
  - [sort](#sort)
  - [genre](#genre)
  - [info](#info)
  - [completion](#completion)
- [Common Workflows](#common-workflows)
- [Examples](#examples)

## Global Flags

These flags work with all commands:

```bash
--config string   # Path to config file (default "configs/config.yaml")
--verbose, -v     # Enable debug logging
--help, -h        # Show help for any command
```

### Custom Config Example

```bash
javinizer --config ~/my-config.yaml scrape IPX-535
```

### Verbose Logging

Enable debug-level logging to troubleshoot issues:

```bash
javinizer -v scrape IPX-535
javinizer --verbose sort ~/Videos
```

The `--verbose` flag overrides the `logging.level` setting in your config file and sets it to `debug` for that command only.

## Commands

### `init`

Initialize Javinizer configuration and database.

```bash
javinizer init
```

**What it does:**
1. Creates `configs/` directory
2. Generates default `config.yaml`
3. Creates `data/` directory
4. Initializes SQLite database
5. Runs database migrations

**Output:**
```
Initializing Javinizer...
✅ Created data directory: data
✅ Initialized database: data/javinizer.db
✅ Saved configuration: configs/config.yaml

🎉 Initialization complete!
```

**When to use:**
- First time setup
- Resetting to default configuration
- After deleting database or config files

---

### `scrape`

Scrape metadata for a specific JAV ID.

```bash
javinizer scrape <ID> [flags]
```

**Arguments:**
- `<ID>`: JAV ID to scrape (e.g., `IPX-535`, `SSIS-123`)

**Flags:**
```bash
--source string   # Specific scraper to use (r18dev, dmm)
```

**Examples:**

Scrape from all enabled scrapers:
```bash
javinizer scrape IPX-535
```

Scrape from specific source:
```bash
javinizer scrape IPX-535 --source r18dev
```

**Output:**
```
Scraping metadata for: IPX-535

📡 Scraping from r18dev...
✅ r18dev scraped successfully

📡 Scraping from dmm...
✅ dmm scraped successfully

🎬 Movie: IPX-535
Title: Beautiful Day
Studio: Idea Pocket
Release Date: 2020-09-13
Runtime: 120 minutes
Actresses: Momo Sakura
Genres: Solowork, Beautiful Girl, Slender

💾 Saved to database
```

**Behavior:**
- Queries enabled scrapers in priority order
- Aggregates metadata from multiple sources
- Caches result in database
- Subsequent scrapes use cached data (instant)

---

### `sort`

Scan, scrape, and organize video files.

```bash
javinizer sort <path> [flags]
```

**Arguments:**
- `<path>`: Directory to scan for video files

**Flags:**
```bash
-d, --dest string        # Destination directory (default: same as source)
    --download           # Download media files (default true)
-n, --dry-run            # Preview without making changes
    --extrafanart        # Download extrafanart (screenshots)
    --force-refresh      # Force refresh metadata from scrapers (clear cache)
-f, --force-update       # Force update existing files
-h, --help               # Help for sort command
-m, --move               # Move files instead of copying
    --nfo                # Generate NFO files (default true)
-r, --recursive          # Scan subdirectories recursively (default true)
-p, --scrapers strings   # Scraper priority (comma-separated, e.g., 'r18dev,dmm')
```

**Examples:**

**Dry run (preview only):**
```bash
javinizer sort ~/Videos --dry-run
```

**Organize files in place:**
```bash
javinizer sort ~/Videos
```

**Move to different location:**
```bash
javinizer sort ~/Downloads --dest ~/Organized --move
```

**Non-recursive scan:**
```bash
javinizer sort ~/Videos --recursive=false
```

**Skip NFO and downloads:**
```bash
javinizer sort ~/Videos --nfo=false --download=false
```

**Download extrafanart (screenshots):**
```bash
javinizer sort ~/Videos --extrafanart
```

**Force refresh from scrapers (clear cache):**
```bash
javinizer sort ~/Videos --force-refresh
```

**Force update existing files (overwrite conflicts):**
```bash
javinizer sort ~/Videos --force-update
```

**Use custom scraper priority:**
```bash
# Use DMM as primary scraper
javinizer sort ~/Videos --scrapers dmm,r18dev

# Use only r18dev
javinizer sort ~/Videos --scrapers r18dev
```

**Complete example:**
```bash
javinizer sort ~/unsorted \
  --dest ~/library \
  --move \
  --recursive \
  --download \
  --nfo \
  --extrafanart \
  --scrapers r18dev,dmm
```

**Output:**
```
📁 Scanning: /Users/you/Videos
Found 3 video file(s)

🔍 Matching files...
✅ Matched: IPX-535.mp4 → IPX-535
✅ Matched: SSIS-123.mkv → SSIS-123
✅ Matched: ABW-001.mp4 → ABW-001

📡 Scraping metadata...
✅ IPX-535 (cached)
✅ SSIS-123 (cached)
📡 ABW-001...
✅ ABW-001 scraped successfully

📝 Planning file organization...

IPX-535.mp4
  → /Users/you/Videos/IPX-535 [Idea Pocket] - Beautiful Day (2020)/IPX-535.mp4
  NFO: IPX-535.nfo
  Media: 5 files

SSIS-123.mkv
  → /Users/you/Videos/SSIS-123 [S1 NO.1 STYLE] - Title (2021)/SSIS-123.mkv
  NFO: SSIS-123.nfo
  Media: 5 files

ABW-001.mp4
  → /Users/you/Videos/ABW-001 [Prestige] - Title (2020)/ABW-001.mp4
  NFO: ABW-001.nfo
  Media: 5 files

✅ Files organized: 3
✅ NFOs generated: 3
✅ Media downloaded: 15 files
```

**Dry Run Output:**
```
... same as above ...

Files organized: 3 (dry-run)

💡 Run without --dry-run to apply changes
```

**What it does:**
1. Scans directory for video files
2. Extracts JAV IDs from filenames
3. Scrapes metadata (uses cache when available)
4. Applies genre replacements
5. Generates NFO files (if enabled)
6. Downloads media files (if enabled)
7. Organizes files into folders
8. Renames files according to template

---

### `genre`

Manage genre name replacements.

Genre replacements allow you to customize genre names from scrapers to match your preferences.

#### `genre add`

Add or update a genre replacement.

```bash
javinizer genre add <original> <replacement>
```

**Arguments:**
- `<original>`: Genre name as it appears from scrapers
- `<replacement>`: Your preferred genre name

**Examples:**

```bash
# Replace "Blow" with "Blowjob"
javinizer genre add "Blow" "Blowjob"

# Replace "Creampie" with "Cream Pie"
javinizer genre add "Creampie" "Cream Pie"

# Shorten long genre names
javinizer genre add "Beautiful Girl" "Beauty"
```

**Output:**
```
✅ Genre replacement added: 'Blow' → 'Blowjob'
```

**Notes:**
- If the original genre already has a replacement, it will be updated
- Replacements are case-sensitive
- Applied during metadata aggregation

#### `genre list`

List all configured genre replacements.

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

**Empty output:**
```
No genre replacements configured
```

#### `genre remove`

Remove a genre replacement.

```bash
javinizer genre remove <original>
```

**Arguments:**
- `<original>`: Original genre name to stop replacing

**Examples:**

```bash
javinizer genre remove "Blow"
```

**Output:**
```
✅ Genre replacement removed: 'Blow'
```

---

### `info`

Display current configuration and system information.

```bash
javinizer info
```

**Output:**
```
=== Javinizer Configuration ===
Config file: configs/config.yaml
Database: data/javinizer.db (sqlite)
Server: localhost:8080

Scrapers:
  r18dev: enabled (priority 1)
  dmm: enabled (priority 2)

Metadata Priority:
  Title: r18dev → dmm
  Description: dmm → r18dev
  Actress: r18dev → dmm
  Genre: r18dev → dmm
  ...

File Matching:
  Extensions: .mp4, .mkv, .avi, .wmv, .flv
  Min Size: 0 MB
  Exclude: *-trailer*, *-sample*
  Custom Regex: disabled

Output:
  Folder: <ID> [<STUDIO>] - <TITLE> (<YEAR>)
  File: <ID>
  Download Cover: true
  Download Poster: true
  Download Screenshots: false
  Download Trailer: false

NFO:
  Enabled: true
  Display Name: <TITLE>
  Filename: <ID>.nfo
  Actress Language: English
```

**When to use:**
- Verify configuration is loaded correctly
- Check which scrapers are enabled
- Review priority settings
- Troubleshoot issues

---

### `completion`

Generate shell completion scripts.

```bash
javinizer completion [bash|zsh|fish|powershell]
```

**Bash:**
```bash
javinizer completion bash > /etc/bash_completion.d/javinizer
```

**Zsh:**
```bash
javinizer completion zsh > "${fpath[1]}/_javinizer"
```

**Fish:**
```bash
javinizer completion fish > ~/.config/fish/completions/javinizer.fish
```

**PowerShell:**
```powershell
javinizer completion powershell | Out-String | Invoke-Expression
```

---

## Common Workflows

### First Time Setup

```bash
# 1. Initialize
javinizer init

# 2. Verify config
javinizer info

# 3. Test scraping
javinizer scrape IPX-535

# 4. Test organization (dry run)
javinizer sort ~/test-folder --dry-run

# 5. Actually organize
javinizer sort ~/test-folder
```

### Adding Genre Replacements

```bash
# Add replacements
javinizer genre add "Blow" "Blowjob"
javinizer genre add "Creampie" "Cream Pie"
javinizer genre add "Cowgirl" "Riding"

# Verify
javinizer genre list

# Test with scraping
javinizer scrape IPX-535  # Check genres in output
```

### Batch Processing

```bash
# Process multiple directories
javinizer sort ~/Downloads --dest ~/Library --move
javinizer sort ~/Unsorted --dest ~/Library --move

# Recursive processing of nested folders
javinizer sort ~/Videos --recursive --dest ~/Organized
```

### Minimal Processing (Fast)

```bash
# Only organize files, skip downloads
javinizer sort ~/Videos \
  --nfo=false \
  --download=false \
  --dry-run  # Preview first
```

### Complete Processing (Everything)

Edit `config.yaml`:
```yaml
output:
  download_cover: true
  download_poster: true
  download_screenshots: true
  download_trailer: true
  download_actress: true
```

Then run:
```bash
javinizer sort ~/Videos --recursive
```

### Testing Configuration Changes

```bash
# 1. Edit config.yaml
vim configs/config.yaml

# 2. Verify changes
javinizer info

# 3. Test with dry run
javinizer sort ~/test --dry-run

# 4. Apply if satisfied
javinizer sort ~/test
```

---

## Examples

### Example 1: Organize Single Directory

```bash
javinizer sort ~/Downloads/JAV
```

**What happens:**
- Scans `~/Downloads/JAV` and subdirectories
- Identifies JAV files by ID in filename
- Scrapes metadata (caches for future use)
- Creates organized folders
- Generates NFO files
- Downloads cover and poster images
- Copies (not moves) files to new structure

### Example 2: Move Files to New Location

```bash
javinizer sort ~/Downloads/JAV \
  --dest ~/Videos/Library \
  --move
```

**What happens:**
- Same as Example 1
- But moves files instead of copying
- Target location is `~/Videos/Library`
- Original location `~/Downloads/JAV` will be empty after

### Example 3: Quick Preview

```bash
javinizer sort ~/unsorted --dry-run
```

**What happens:**
- Shows what would be done
- No files are moved/copied
- No NFOs are created
- No downloads occur
- Perfect for testing

### Example 4: Metadata Only

```bash
javinizer sort ~/Videos \
  --nfo=true \
  --download=false
```

**What happens:**
- Organizes files
- Generates NFO files
- Skips all media downloads
- Faster processing

### Example 5: Complete Library Rebuild

```bash
# Add genre preferences
javinizer genre add "Creampie" "Cream Pie"
javinizer genre add "Cowgirl" "Riding"

# Process everything
javinizer sort ~/old-library \
  --dest ~/new-library \
  --move \
  --recursive \
  --download \
  --nfo
```

### Example 6: Scrape Multiple IDs

```bash
# Scrape and cache metadata
javinizer scrape IPX-535
javinizer scrape SSIS-123
javinizer scrape ABW-001

# Now sorting will be instant (uses cache)
javinizer sort ~/Videos
```

### Example 7: Non-Recursive Scan

```bash
# Only scan top-level directory
javinizer sort ~/Videos --recursive=false
```

### Example 8: Custom Config

```bash
# Use different config for different scenarios
javinizer --config ~/configs/minimal.yaml sort ~/Videos
javinizer --config ~/configs/complete.yaml sort ~/Special
```

---

## API Server

### `api` (alias: `web`)

Start a REST API server for programmatic access to Javinizer functionality.

```bash
javinizer api [flags]
javinizer web [flags]
```

**Flags:**
- `--host string` - Server host address (default from config)
- `--port int` - Server port (default from config)
- `--verbose`, `-v` - Enable debug logging
- `--config string` - Custom config file path

**Examples:**

```bash
# Start with defaults (localhost:8080)
javinizer api

# Equivalent alias
javinizer web

# Custom host and port
javinizer api --host 0.0.0.0 --port 9000

# With verbose logging
javinizer api --verbose
```

**API Endpoints:**

- `GET /health` - Health check and scraper status
- `POST /api/v1/scrape` - Scrape metadata for a movie ID
- `GET /api/v1/movie/:id` - Get movie metadata by ID
- `GET /api/v1/movies` - List cached movies
- `GET /api/v1/config` - Get current configuration

**Example API Usage:**

```bash
# Health check
curl http://localhost:8080/health

# Scrape a movie
curl -X POST http://localhost:8080/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"id": "IPX-535"}'

# Get movie from cache
curl http://localhost:8080/api/v1/movie/IPX-535
```

**Response Format:**

```json
{
  "cached": false,
  "movie": {
    "id": "IPX-535",
    "title": "Movie Title",
    "actresses": [...],
    "genres": [...],
    ...
  },
  "sources_used": 2
}
```

---

## Tips and Tricks

1. **Always dry-run first**: Use `--dry-run` to preview before making changes
2. **Cache metadata**: Run `javinizer scrape` beforehand for faster batch processing
3. **Genre customization**: Set up genre replacements before sorting
4. **Incremental processing**: Process folders one at a time to verify results
5. **Use move cautiously**: `--move` deletes original files, use after dry-run verification
6. **Check info**: Run `javinizer info` after config changes to verify
7. **Backup database**: The database contains your genre preferences and cache
8. **Shell completion**: Install completion for faster command typing

---

**Next**: [Template System](./04-template-system.md)
