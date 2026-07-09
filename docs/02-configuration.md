# Configuration Guide

Javinizer Go uses a YAML configuration file located at `configs/config.yaml`. This guide covers all configuration options in detail.

## Table of Contents

- [Configuration File Location](#configuration-file-location)
- [Server Settings](#server-settings)
- [Scraper Configuration](#scraper-configuration)
- [Metadata Priority](#metadata-priority)
- [File Matching](#file-matching)
- [Output Formatting](#output-formatting)
- [NFO Settings](#nfo-settings)
- [Database Configuration](#database-configuration)
- [Logging](#logging)

## Configuration File Location

By default, Javinizer looks for `configs/config.yaml`. You can specify a custom location:

```bash
javinizer --config /path/to/custom/config.yaml scrape IPX-535
```

Generate a fresh config file:

```bash
javinizer init
```

The config file includes a `config_version` field. On startup, Javinizer applies compatibility rules for older config files and writes the upgraded config back to disk.

## Server Settings

Configure the REST API server:

```yaml
server:
  host: localhost  # Bind address
  port: 8765       # Listen port
```

### API Security Settings

Configure path access control for the API:

```yaml
api:
  security:
    allowed_directories:
      - /media
      - ~/Videos
    denied_directories:
      - /etc
      - /root
    max_files_per_scan: 10000
    scan_timeout_seconds: 30
    allowed_origins:
      - "http://localhost:5173"
      - "http://localhost:5174"
    # Windows-specific UNC path settings
    allow_unc: false
    allowed_unc_servers: []
```

**allowed_directories**: Paths the API can access. Empty = deny all (secure by default).

**denied_directories**: Additional paths to block (built-in denylist includes `/proc`, `/sys`, `/dev`).

**max_files_per_scan**: Maximum files returned by scan endpoint.

**scan_timeout_seconds**: Timeout for scan operations.

**allowed_origins**: CORS allowed origins. `["*"]` allows all (development only).

**allow_unc**: (Windows only) Allow UNC paths. **Default: false** for security.

**allowed_unc_servers**: (Windows only) Whitelisted UNC servers when `allow_unc` is true.

#### UNC Path Security Warning

UNC paths like `\\server\share` can leak NTLM credentials to remote servers. Windows automatically sends authentication when accessing UNC paths. Only enable if you trust all servers in `allowed_unc_servers`.

## Scraper Configuration

### Overview

Javinizer supports multiple metadata scrapers that can be enabled/disabled and prioritized.

### General Scraper Settings

```yaml
config_version: 3

scrapers:
  user_agent: ""  # Default: Chrome-like UA. r18dev uses the Javinizer UA automatically.
  priority:
    - r18dev
    - libredmm
    - dmm
    - javlibrary
    - javdb
    - javbus
    - jav321
    - mgstage
    - tokyohot
    - aventertainment
    - caribbeancom
    - dlgetchu
    - fc2
    - javstash
  proxy:                  # Optional global proxy for all scrapers
    enabled: false
    default_profile: "main"   # Profile used by default when proxy is enabled
    profiles:
      main:
        url: ""           # Examples: "http://proxy.example.com:8080", "socks5://localhost:1080"
        username: ""      # Optional authentication
        password: ""      # Optional authentication
      backup:
        url: ""
        username: ""
        password: ""
```

**user_agent**: HTTP User-Agent header sent to scraper websites. Empty by default — Javinizer then sends a Chrome-like User-Agent, and the r18dev scraper automatically uses the `Javinizer (+https://github.com/javinizer/javinizer-go)` UA. Set this only to override the default.

**priority**: Order to query scrapers. First scraper is tried first. If it fails, the next one is attempted. The default list contains all 14 supported scrapers (`r18dev, libredmm, dmm, javlibrary, javdb, javbus, jav321, mgstage, tokyohot, aventertainment, caribbeancom, dlgetchu, fc2, javstash`).

**proxy**: Global HTTP/SOCKS5 proxy used by all scrapers by default. Define reusable connection profiles under `profiles`, pick the global default with `default_profile`, and enable with `enabled: true`. A direct top-level `url`/`username`/`password` is **not** supported — the config loader rejects those legacy fields. Each scraper can override via `scrapers.<name>.proxy` with `profile: <name>` to reference a profile from `scrapers.proxy.profiles`. FlareSolverr is configured separately at `scrapers.flaresolverr` (global) or `scrapers.<name>.flaresolverr` (per-scraper) — it is **not** nested under `proxy`.

### R18.dev Scraper

R18.dev provides a fast JSON API for JAV metadata.

```yaml
scrapers:
  r18dev:
    enabled: true  # Enable/disable R18.dev scraper
```

**Pros**:
- Fast (JSON API)
- Reliable
- Complete metadata
- Actress information included

**Cons**:
- Requires internet connection
- May have rate limiting

### DMM/Fanza Scraper

DMM (Digital Media Mart) is the official source for many JAV releases.

```yaml
scrapers:
  dmm:
    enabled: false          # Enable/disable DMM scraper (default: false)
    use_browser: true       # Enable browser for DMM streaming pages
    # scrape_actress: true  # Optional: inherits global scrape_actress (default: true)
```

**scrape_actress**: Whether to scrape actress information from DMM. When unset, inherits the global `scrapers.scrape_actress` (default `true`). DMM actress scraping is slower due to HTML parsing.

**Pros**:
- Official source
- Accurate release dates
- Detailed descriptions

**Cons**:
- Slower (HTML parsing)
- May require more requests

### JavLibrary Scraper

JavLibrary is useful as a supplemental source and often benefits from FlareSolverr.

```yaml
scrapers:
  javlibrary:
    enabled: false
    language: "ja"          # en, ja, cn, tw (default: ja)
    base_url: "http://www.javlibrary.com"
    # rate_limit: 1000      # Delay between requests in ms (0 = no delay)
    use_flaresolverr: false  # Enable to use global FlareSolverr for Cloudflare bypass
```

### JavDB Scraper

JavDB can be useful as a supplemental source. It may require both proxy routing and FlareSolverr depending on your network/location.

```yaml
scrapers:
  javdb:
    enabled: false
    base_url: "https://javdb.com"
    # rate_limit: 1000      # Delay between requests in ms (0 = no delay)
    use_flaresolverr: false  # Enable to use global FlareSolverr for Cloudflare bypass
    # Per-scraper proxy override: enable and reference a named profile.
    # FlareSolverr is a sibling key (scrapers.javdb.flaresolverr), NOT nested under proxy.
    # proxy:
    #   enabled: true
    #   profile: main        # References scrapers.proxy.profiles.main
    # flaresolverr:
    #   enabled: true
    #   url: "http://localhost:8191/v1"
```

## Metadata Priority

Control which scraper's data is used for each field when multiple scrapers return results.

### Priority System

The priority list determines data precedence:

```yaml
metadata:
  priority:
    title:
      - r18dev  # Use R18.dev title first
      - dmm     # Fall back to DMM if R18.dev missing
```

If R18.dev returns a title, use it. If not, use DMM's title.

### Per-Field Priority Semantics

A per-field priority list is **exclusive**: when a field lists specific scrapers,
only those scrapers contribute to that field — the global `scrapers.priority`
list is **not** merged in as a fallback. This matches the original PowerShell
Javinizer behavior.

- **Key present with scrapers** (e.g. `series: [tokyohot]`): only the listed
  scrapers populate the field. If none of them ran or lack the field, the field
  is left empty (no global fallback).
- **Key absent**: the field inherits the global `scrapers.priority` list.
- **Key present as an empty list** (`series: []`): inherits the global list
  (equivalent to omitting the key).
- **Skip a field entirely**: point it at a scraper that won't run or use the
  `__skip__` sentinel (e.g. `series: ["__skip__"]`), which matches no scraper
  and leaves the field empty.

This exclusivity applies consistently to both the default scrape path and the
`--scrapers` / selected-scrapers path.

### Field-by-Field Priority

The default per-field priorities (matching `configs/config.yaml.example`) favor DMM for most fields; only the vertical poster (`poster_url`) favors R18.dev. Valid field keys are exactly the 18 resolved by the aggregator: `id`, `content_id`, `title`, `original_title`, `description`, `release_date`, `runtime`, `director`, `maker`, `label`, `series`, `poster_url`, `cover_url`, `screenshot_url`, `trailer_url`, `actress`, `genre`, `rating`. Other keys (e.g. the legacy `alternate_title`) are not recognized and are ignored.

```yaml
metadata:
  priority:
    # Basic Information
    id:
      - dmm
      - r18dev
      - libredmm

    content_id:
      - dmm
      - r18dev
      - libredmm

    title:
      - dmm
      - r18dev
      - libredmm

    original_title:
      - dmm
      - r18dev
      - libredmm

    # Descriptions favor DMM (more detailed)
    description:
      - dmm
      - r18dev
      - libredmm

    # Release Information
    release_date:
      - dmm
      - r18dev
      - libredmm

    runtime:
      - dmm
      - r18dev
      - libredmm

    # Studio/Production
    director:
      - dmm
      - r18dev
      - libredmm

    maker:
      - dmm
      - r18dev
      - libredmm

    label:
      - dmm
      - r18dev
      - libredmm

    series:
      - dmm
      - r18dev
      - libredmm

    # Media — vertical poster favors R18.dev; cover/screenshots favor DMM
    poster_url:
      - r18dev
      - libredmm
      - dmm

    cover_url:
      - dmm
      - r18dev
      - libredmm

    screenshot_url:
      - dmm
      - r18dev
      - libredmm

    trailer_url:
      - dmm
      - r18dev
      - libredmm

    # Categorical
    actress:
      - dmm
      - r18dev
      - libredmm

    genre:
      - dmm
      - r18dev
      - libredmm

    # Ratings favor DMM
    rating:
      - dmm
      - r18dev
      - libredmm
```

### Customization Examples

**Prefer DMM for all fields:**
```yaml
metadata:
  priority:
    title:
      - dmm
      - r18dev
    description:
      - dmm
      - r18dev
    # ... repeat for all fields
```

**Use only R18.dev (ignore DMM):**
```yaml
scrapers:
  dmm:
    enabled: false

metadata:
  priority:
    title:
      - r18dev
    # ... only list r18dev
```

### Genre and Actress Settings

```yaml
metadata:
  ignore_genres: []        # List of genres to filter out
  required_fields: []      # Fields that must be present
```

**ignore_genres**: Array of genre names to exclude. Useful for filtering unwanted categories:

```yaml
metadata:
  ignore_genres:
    - "Uncensored"
    - "Amateur"
```

**required_fields**: Fields that must have data for the movie to be considered valid. If any required field is missing, the aggregation may fail or warn.

### Local R18.dev Dump Lookup

Javinizer can download a snapshot of the r18.dev database into a local
SQLite sidecar. When present, the `r18dev` scraper resolves DMM
`content_id`s with a single local lookup instead of issuing the slow,
rate-limit-prone HTTP probes that are normally the slowest step of a scrape.

```yaml
metadata:
  r18dev_dump:
    enabled: true                                # Activates automatically once the dump is downloaded (default: true)
    path: data/r18dev/r18dev_dump.db             # Sidecar SQLite path (relative to working dir)
```

**enabled**: Whether the scraper consults the local dump. Defaults to `true`
— it is a no-op until the dump file exists, so it is safe to leave on. When
the file is absent, the scraper silently falls back to HTTP probing, so this
flag never blocks scraping.

**path**: Where the SQLite sidecar lives. Defaults to
`data/r18dev/r18dev_dump.db` (relative to the working directory, like the main
DB). Point this at a shared location to reuse one dump across multiple
Javinizer installs.

The dump is managed with the `javinizer dump` command group — see
[`dump` in the CLI Reference](./03-cli-reference.md#dump) for `download`,
`update`, `status`, and `search`. The dump URL can be overridden with the
`JAVINIZER_R18DEV_DUMP_URL` environment variable (e.g. to use a mirror or
cache).

### CSV Settings (Legacy — Removed)

Javinizer Go replaced the PowerShell version's CSV-based actress/genre thumbprints with a SQLite database. The legacy `metadata.thumb_csv` and `metadata.genre_csv` keys are **not part of the config schema** and are silently ignored — they are not "maintained for backward compatibility". Use the database-backed features instead:

- Actress images: `metadata.actress_database` and the `javinizer actress` commands.
- Genre normalization: `metadata.genre_replacement` and the `javinizer genre` commands (see [Genre Management](./05-genre-management.md)).
- Word/text replacement: `metadata.word_replacement` and the `javinizer word` commands.
- Per-movie tags: `metadata.tag_database` and the `javinizer tag` commands.

## NFO Settings

Configure Kodi/Plex-compatible NFO file generation.

```yaml
metadata:
  nfo:
    enabled: true                    # Generate NFO files
    display_title: <TITLE>           # Movie display name in NFO (was display_name)
    filename_template: <ID>.nfo      # NFO filename pattern
    first_name_order: true           # Actress name order (true = First Last)
    actress_language_ja: false       # Use Japanese actress names
    per_file: false                  # One NFO per multi-part file when true
    unknown_actress_mode: skip       # skip (default) or fallback
    unknown_actress_text: Unknown    # Placeholder used in fallback mode
    actress_as_tag: false            # Add actress names as tags
    add_generic_role: false          # Add generic "Actress" role to all actresses
    alt_name_role: false             # Use alternate (Japanese) name in role field
    include_originalpath: false      # Include source filename in NFO
    include_stream_details: false    # Include video stream metadata
    include_fanart: true             # Include fanart URL
    include_trailer: true            # Include trailer URL
    rating_source: r18dev            # Rating source identifier
    tag: []                          # Additional custom tags
    tagline: ""                      # Custom tagline template
    credits: []                      # Additional credits
```

### NFO Field Details

**enabled**: Master switch for NFO generation.

**display_title**: Template for the `<title>` field in NFO. Uses template tags (see [Template System](./04-template-system.md)). (The legacy `display_name` key was renamed to `display_title` and is no longer recognized.)

**filename_template**: Pattern for NFO filename. Default `<ID>.nfo` creates files like `IPX-535.nfo`.

**per_file**: When `true`, generate one NFO per multi-part file instead of one per movie. Default `false`.

**first_name_order**:
- `true`: "Momo Sakura"
- `false`: "Sakura Momo"

**actress_language_ja**: Use Japanese names when available (e.g., "桜空もも" instead of "Momo Sakura").

**unknown_actress_mode**: How to handle a movie with no actress. `skip` (default) omits the actress block entirely; `fallback` inserts `unknown_actress_text` as a placeholder actress.

**unknown_actress_text**: Placeholder text used when `unknown_actress_mode: fallback` and no actress is found.

**actress_as_tag**: If true, adds each actress name as a `<tag>` in the NFO for better searchability.

**add_generic_role**: If true, adds a generic "Actress" `<role>` to every actress entry.

**alt_name_role**: If true, uses the alternate (Japanese) name in the actress `<role>` field.

**include_originalpath**: If true, records the original source filename in the NFO. Note the spelling: `include_originalpath` (no underscore between `original` and `path`).

**include_stream_details**: Adds `<fileinfo><streamdetails>` section (requires video file analysis - not yet implemented).

**include_fanart**: Includes `<fanart>` URL in NFO.

**include_trailer**: Includes `<trailer>` URL in NFO.

**rating_source**: Source identifier for the rating. Defaults to the first scraper in `scrapers.priority` (`r18dev` with the default priority list). Common values: `r18dev`, `dmm`, `libredmm`, or any scraper name.

**tag**: Array of custom tags to add to every NFO:

```yaml
metadata:
  nfo:
    tag:
      - "JAV"
      - "Japanese"
```

**tagline**: Custom tagline template (supports template tags).

**credits**: Additional credits to include.

## File Matching

Configure how Javinizer identifies JAV files and extracts IDs.

```yaml
file_matching:
  extensions:
    - .mp4
    - .mkv
    - .avi
    - .wmv
    - .flv
  min_size_mb: 0
  exclude_patterns:
    - '*-trailer*'
    - '*-sample*'
  regex_enabled: false
  regex_pattern: ([a-zA-Z|tT28]+-\d+[zZ]?[eE]?)(?:-pt)?(\d{1,2})?
```

### Field Details

**extensions**: File extensions to scan. Only files with these extensions are processed.

**min_size_mb**: Minimum file size in MB. Files smaller than this are ignored. Use this to filter out trailers/samples based on size.

**exclude_patterns**: Glob patterns to exclude. Files matching these patterns are skipped:
- `*-trailer*`: Excludes "IPX-535-trailer.mp4"
- `*-sample*`: Excludes "sample-video.mp4"

**regex_enabled**: Enable custom regex for ID extraction.

**regex_pattern**: Custom regex pattern for extracting JAV IDs. The default pattern matches:
- Standard IDs: `IPX-535`, `SSIS-123`
- With suffixes: `IPX-535Z`, `SSIS-123E`
- Multi-part: `IPX-535-pt1`, `IPX-535-cd2`
- Special formats: `T28-123`

### Custom Regex Examples

**Only 3-letter studio codes:**
```yaml
file_matching:
  regex_enabled: true
  regex_pattern: ([A-Z]{3}-\d+)
```

**Include 4-letter codes:**
```yaml
file_matching:
  regex_enabled: true
  regex_pattern: ([A-Z]{3,4}-\d+)
```

## Output Formatting

Control how files and folders are organized and named.

```yaml
output:
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"
  file_format: "<ID><IF:MULTIPART>-pt<PART></IF>"
  subfolder_format: ["<ID>"]  # Optional nested folder hierarchy
  actress_delimiter: ", "    # Legacy alias: delimiter
  download_cover: true
  download_poster: true
  download_extrafanart: false  # Screenshots in extrafanart/ subfolder
  download_trailer: false
  download_actress: false
```

### Naming Templates

**folder_format**: Template for folder names. Example result (title abbreviated — IPX-535's actual title is much longer, and special characters are sanitized for filesystem compatibility):
```
IPX-535 [Idea Pocket] - 3, 2, 1, GO! Sudden Follow-Up... (2020)/
```

**file_format**: Template for filenames (extension added automatically). Example:
```
IPX-535.mp4
```

**subfolder_format**: Array of templates for creating nested folder hierarchies. This allows you to organize files into multiple subfolder levels before the main movie folder.

Example with empty array (default):
```yaml
subfolder_format: []
```
Results in:
```
dest/
  IPX-535 [Idea Pocket] - 3, 2, 1, GO! Sudden Follow-Up... (2020)/
    IPX-535.mp4
```

Example with year organization:
```yaml
subfolder_format: ["<YEAR>"]
```
Results in:
```
dest/
  2020/
    IPX-535 [Idea Pocket] - 3, 2, 1, GO! Sudden Follow-Up... (2020)/
      IPX-535.mp4
```

Example with year and studio organization:
```yaml
subfolder_format: ["<YEAR>", "<STUDIO>"]
```
Results in:
```
dest/
  2020/
    Idea Pocket/
      IPX-535 [Idea Pocket] - 3, 2, 1, GO! Sudden Follow-Up... (2020)/
        IPX-535.mp4
  2021/
    S1 NO.1 STYLE/
      SSIS-123 [S1 NO.1 STYLE] - Title (2021)/
        SSIS-123.mkv
```

**Notes:**
- Empty subfolder values are skipped
- All template tags are supported (see [Template System](./04-template-system.md))
- Folder names are automatically sanitized for filesystem compatibility
- Can be overridden per-command with CLI flags
- SMB/NAS note: on some servers/clients, folder names that end with `.` can be shown as mangled short names (for example, `ABC123~1`). Javinizer trims trailing dots/spaces in generated folder names to avoid this.

See [Template System](./04-template-system.md) for available tags and modifiers.

### Download Options

**download_cover**: Download the horizontal cover/fanart image (`<ID>-fanart.jpg`).

**download_poster**: Download the vertical poster image (`<ID>-poster.jpg`).

**download_extrafanart**: Download screenshot images to `extrafanart/` subfolder (`fanart1.jpg`, `fanart2.jpg`, etc.).

**Note**: In the original Javinizer, screenshots and extrafanart refer to the same thing. The screenshots are saved in the `extrafanart/` subfolder as `fanart<number>.jpg` files for Kodi/Plex compatibility.

**download_trailer**: Download trailer video (`<ID>-trailer.mp4`).

**download_actress**: Download actress thumbnail images to `.actors/` subfolder.

**actress_format**: Template for actress image filenames (default: `<ACTORNAME>.jpg`). Supports template variables like `<ID>`, `<ACTORNAME>`, etc. Examples:
- `<ACTORNAME>.jpg` - Default, matches original Javinizer (e.g., `白上咲花.jpg`)
- `<ID>_<ACTORNAME>.jpg` - Include movie ID (e.g., `SONE-860_白上咲花.jpg`)
- `actress-<ACTORNAME>.jpg` - With prefix (e.g., `actress-白上咲花.jpg`)

### Delimiter

**actress_delimiter**: String used to join multiple values (e.g., actress names, genres) in templates. The legacy `delimiter` key is retained as a backward-compatible alias and is copied into `actress_delimiter` during config normalization.

Example with `actress_delimiter: ", "`:
```
Actresses: Sakura Momo, Mikami Yua, Anzai Rara
```

### Actress Organization

**group_actress**: When `true` and a movie has multiple actresses, the `<ACTRESSES>` template tag resolves to a group folder name instead of listing individual names. Only affects templates that contain `<ACTRESSES>`. Default: `false`.

**group_actress_name**: Folder name used when `group_actress` is enabled and multiple actresses are found. Default: `@Group`.

**group_unknown_actress_name**: Folder name used when `group_actress` is enabled and the actress list is empty or contains only an unknown actress. Default: `@Unknown`.

Example with `group_actress: true`:
```
Template: <ACTRESSES>/<ID> - <TITLE>
Multiple actresses: @Group/IPX-535 - 3, 2, 1, GO! Sudden Follow-Up.../
Single actress:     Sakura Momo/IPX-535 - 3, 2, 1, GO! Sudden Follow-Up.../
```

### Actress Name Ordering

**first_name_order**: Controls actress name ordering in template tags. Default: `false`.

- `false`: LastName FirstName (Japanese convention, e.g., `Sakura Momo`)
- `true`: FirstName LastName (Western convention, e.g., `Momo Sakura`)

Affects `<ACTRESSES>`, `<ACTRESS>`, and `<ACTRESSNAME>` tags. Does not affect `<FIRSTNAME>` and `<LASTNAME>` which always return raw name components.

> **Note:** This is separate from `nfo.first_name_order` which controls name formatting inside NFO files and defaults to `true` (Kodi/Plex convention).

## Database Configuration

Configure the metadata cache database.

```yaml
database:
  type: sqlite
  dsn: data/javinizer.db
```

**type**: Database type. Currently only `sqlite` is supported.

**dsn**: Database connection string. For SQLite, this is the file path.

### Database Files

The database is created in `data/javinizer.db` and contains:
- Movie metadata cache
- Actress information
- Genre replacements
- Operation history

See [Database Schema](./06-database-schema.md) for table structure.

## Logging

Configure logging output to track operations, debug issues, and maintain audit trails.

```yaml
logging:
  level: info        # Log level: debug, info, warn, error
  format: text       # Log format: text or json
  output: stdout     # Output: stdout, stderr, or file path
```

### Field Details

**level**: Minimum log level to display:
- `debug`: All messages including debug info (verbose)
- `info`: Informational messages and above (default)
- `warn`: Warnings and errors only
- `error`: Errors only

**format**:
- `text`: Human-readable format with timestamps (default)
- `json`: Structured JSON for log aggregation tools (Elasticsearch, Splunk, etc.)

**output**:
- `stdout`: Standard output (console)
- `stderr`: Standard error (console)
- `/path/to/file.log`: Write to file (creates directory if needed)
- Multiple outputs: `stdout,/path/to/file.log` (comma-separated for dual output)

When `JAVINIZER_LOG_DIR` is set, Javinizer rewrites file targets in `logging.output` to that directory.
If `logging.output` only contains `stdout`/`stderr`, `JAVINIZER_LOG_DIR` does not create a file output.

**`JAVINIZER_LOG_DIR` resolution examples:**
- `logging.output: stdout` + `JAVINIZER_LOG_DIR=/javinizer/logs` -> `stdout` (unchanged)
- `logging.output: data/logs/javinizer.log` + `JAVINIZER_LOG_DIR=/javinizer/logs` -> `/javinizer/logs/javinizer.log`
- `logging.output: "stdout,data/logs/javinizer.log"` + `JAVINIZER_LOG_DIR=/javinizer/logs` -> `"stdout,/javinizer/logs/javinizer.log"`

**Log Rotation** (applies to file outputs only):
- `max_size_mb`: Maximum file size in megabytes before rotation. Set to `0` to disable rotation (default: `10`)
- `max_backups`: Maximum number of old log files to retain (default: `5`)
- `max_age_days`: Maximum number of days to retain old log files. Set to `0` for no age limit (default: `0`)
- `compress`: Compress rotated log files with gzip (default: `true`)

When rotation is enabled, log files are automatically rotated when they exceed `max_size_mb`. Old files are renamed with a timestamp suffix (e.g., `javinizer-2026-04-06T01-00-00.000.log.gz`).

### Examples

**Console output only (default):**
```yaml
logging:
  level: info
  format: text
  output: stdout
```

**File logging for support:**
```yaml
logging:
  level: info
  format: text
  output: data/logs/javinizer.log
```

**Dual output (console + file):**
```yaml
logging:
  level: info
  format: text
  output: "stdout,data/logs/javinizer.log"
```

**Dual output with rotation:**
```yaml
logging:
  level: info
  format: text
  output: "stdout,data/logs/javinizer.log"
  max_size_mb: 10      # Rotate at 10MB
  max_backups: 5       # Keep 5 old files
  compress: true       # Compress rotated files
```

**JSON logs for analysis:**
```yaml
logging:
  level: debug
  format: json
  output: /var/log/javinizer/operations.json
```

**Debug mode:**
```yaml
logging:
  level: debug
  format: text
  output: stdout
```

### CLI Override

Use the `--verbose` or `-v` flag to enable debug logging regardless of config:

```bash
javinizer -v scrape IPX-535
javinizer --verbose sort ~/Videos
```

This temporarily sets the log level to `debug` for that command.

### Log Rotation

Javinizer supports built-in log rotation for file outputs. Configure rotation using the `max_size_mb`, `max_backups`, `max_age_days`, and `compress` settings.

**Built-in rotation (recommended):**
```yaml
logging:
  output: "stdout,data/logs/javinizer.log"
  max_size_mb: 10      # Rotate when file reaches 10MB
  max_backups: 5       # Keep up to 5 rotated files
  max_age_days: 30     # Delete files older than 30 days
  compress: true       # Compress rotated files (.gz)
```

**External rotation (alternative):**

If you prefer external tools or need time-based rotation, set `max_size_mb: 0` to disable built-in rotation and use tools like logrotate:

**Linux/macOS (logrotate):**
```
/path/to/data/logs/javinizer.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
}
```

**Manual cleanup:**
```bash
# Keep last 30 days
find data/logs/ -name "*.log" -mtime +30 -delete
```

### Troubleshooting

**Logs not appearing in file:**
- Check file permissions
- Verify directory exists (Javinizer creates it automatically)
- Check disk space

**Too many logs:**
- Change level from `debug` to `info` or `warn`
- Implement log rotation

**Need logs for support:**
1. Set output to file: `output: data/logs/support.log`
2. Set level to `debug`
3. Reproduce issue
4. Share the log file

## Configuration Examples

### Minimal Setup (Fast, Cover Only)

```yaml
output:
  download_poster: false
  download_extrafanart: false
  download_trailer: false
  download_actress: false

file_matching:
  min_size_mb: 100  # Skip trailers/samples
```

### Complete Setup (Download Everything)

```yaml
output:
  download_cover: true
  download_poster: true
  download_extrafanart: true
  download_trailer: true
  download_actress: true
```

### DMM-Only Setup

```yaml
scrapers:
  r18dev:
    enabled: false
  dmm:
    enabled: true
    scrape_actress: true

metadata:
  priority:
    title: [dmm]
    description: [dmm]
    # ... only DMM in all priorities
```

### Custom Folder Structure

```yaml
output:
  folder_format: "<STUDIO>/<YEAR>/<ID> - <TITLE>"
  file_format: "<ID> - <TITLE>"
```

Result:
```
Idea Pocket/
  2020/
    IPX-535 - 3, 2, 1, GO! Sudden Follow-Up.../
      IPX-535 - 3, 2, 1, GO! Sudden Follow-Up....mp4
```

## Validation

Check your configuration:

```bash
javinizer info
```

This displays your current configuration and verifies it's valid.

## Advanced Tips

1. **Backup your config**: Keep a copy of `config.yaml` with your preferred settings
2. **Test changes with dry-run**: Use `--dry-run` (`sort -n`) to preview organize operations without making changes
3. **Genre filtering**: Use `ignore_genres` to filter unwanted categories
4. **Priority tuning**: Experiment with different scraper priorities for best results
5. **Template testing**: Test folder/file formats before processing large batches

## Docker Deployment

### Path Configuration for Containers

When running Javinizer in a Docker container, the paths in your configuration must match what the **container** sees, not the host.

#### Linux Containers

```yaml
api:
  security:
    allowed_directories:
      - /data/videos    # Container path, not host path
```

Run with:
```bash
docker run -v /host/videos:/data/videos ghcr.io/javinizer/javinizer-go:latest
```

#### Windows Containers

```yaml
api:
  security:
    allowed_directories:
      - C:\data\videos  # Container path
```

Run with:
```powershell
docker run -v C:\host\videos:C:\data\videos ghcr.io/javinizer/javinizer-go:latest
```

### WSL2 Considerations

When running on WSL2:

1. **Windows drives** are mounted at `/mnt/c/`, `/mnt/d/`, etc.
2. **UNC paths** to WSL distros: `\\wsl$\Ubuntu\path`
3. **Performance**: Accessing Windows files from WSL2 is slower than native Linux files

**Recommended**: Use WSL2 filesystem paths (`/home/user/videos`) for best performance.

### UNC Paths and Security

UNC paths (`\\server\share`) are blocked by default because they can leak NTLM credentials to remote servers. If you need UNC access:

```yaml
api:
  security:
    allow_unc: true
    allowed_unc_servers:
      - fileserver.local
      - nas.example.com
```

**Only allow UNC servers you trust.** The server receives your Windows authentication credentials when accessed.

### Common Docker Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| "path outside allowed directories" | Host path in config, container sees different path | Use container paths in config |
| Slow file scanning on Windows | Using `/mnt/c/` paths in WSL2 | Move files to WSL2 filesystem |
| UNC path blocked | UNC paths disabled by default | Enable `api.security.allow_unc` |
| Paths with spaces fail | Improper quoting | Use quotes in volume mounts |

### Docker Compose Example

```yaml
services:
  javinizer:
    image: ghcr.io/javinizer/javinizer-go:latest
    ports:
      - "8765:8765"
    volumes:
      - ./config:/config
      - /media/videos:/data/videos:ro
    environment:
      - JAVINIZER_CONFIG=/config/config.yaml
```

## Environment Variables

Environment variables override configuration file settings and are particularly useful for Docker deployments and secrets management.

### Core Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JAVINIZER_CONFIG` | Optional | `configs/config.yaml` | Override config file path |
| `JAVINIZER_DB` | Optional | `data/javinizer.db` | Override database path |
| `JAVINIZER_LOG_DIR` | Optional | - | Relocate log file outputs to this directory |
| `JAVINIZER_TEMP_DIR` | Optional | `data/temp` | Override temp directory for file processing |
| `JAVINIZER_DATA_DIR` | Optional | - | Override data directory (reserved for future use) |
| `LOG_LEVEL` | Optional | `info` | Override log level (`debug`, `info`, `warn`, `error`) |
| `UMASK` | Optional | `002` | Override file creation mask (e.g., `002` for `rwxrwxr-x`) |

### Docker Deployment Variables

These variables are specific to Docker deployments and container orchestration:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PUID` | Optional | `1000` | User ID for file ownership in container |
| `PGID` | Optional | `1000` | Group ID for file ownership in container |
| `TZ` | Optional | `UTC` | Container timezone (IANA format: `America/New_York`) |
| `HOST_PORT` | Optional | `8765` | Host port to expose Javinizer web UI |
| `FLARESOLVERR_HOST_PORT` | Optional | `8191` | Host port to expose FlareSolverr API |
| `MEDIA_PATH` | Recommended | - | Absolute path to media library on host system |

### Scraper API Keys

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JAVSTASH_API_KEY` | Optional* | - | JAVStash scraper API key (required if using javstash scraper) |
| `CHROME_BIN` | Optional | - | Path to Chrome/Chromium binary (auto-detected if empty) |
| `CHROME_PATH` | Optional | - | Alternative path to Chrome/Chromium binary |

*Required when the javstash scraper is enabled.

### Translation Provider Credentials

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TRANSLATION_PROVIDER` | Optional | `openai` | Translation provider (`openai`, `deepl`, `google`, `openai_compatible`, `anthropic`) |
| `TRANSLATION_SOURCE_LANGUAGE` | Optional | `ja` | Source language for translation |
| `TRANSLATION_TARGET_LANGUAGE` | Optional | `en` | Target language for translation |
| `OPENAI_API_KEY` | Optional* | - | OpenAI API key for translation |
| `OPENAI_BASE_URL` | Optional | `https://api.openai.com/v1` | OpenAI API base URL |
| `OPENAI_MODEL` | Optional | `gpt-4o-mini` | OpenAI model for translation |
| `DEEPL_API_KEY` | Optional* | - | DeepL API key for translation |
| `GOOGLE_TRANSLATE_API_KEY` | Optional* | - | Google Translate API key |
| `OPENAI_COMPATIBLE_API_KEY` | Optional* | - | OpenAI-compatible API key (e.g., Ollama) |
| `ANTHROPIC_API_KEY` | Optional* | - | Anthropic API key for translation |

*Required when the corresponding translation provider is enabled.

### Version Check Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GH_TOKEN` | Optional | - | GitHub token for version check (higher priority) |
| `GITHUB_TOKEN` | Optional | - | GitHub token for version check (fallback) |

### Development Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JAVINIZER_DEV_MODE` | Optional | `false` | Enable development mode for frontend hot-reload |
| `JAVINIZER_RUN_FLARESOLVERR_TESTS` | Optional | - | Enable FlareSolverr integration tests (`1` to enable) |
| `VITE_API_URL` | Optional | - | Frontend API URL for development |

### API Initialization Variables

These variables are applied during first-time configuration initialization:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JAVINIZER_INIT_SERVER_HOST` | Optional | `localhost` | Initial server host |
| `JAVINIZER_INIT_ALLOWED_DIRECTORIES` | Optional | - | Comma-separated list of allowed directories |
| `JAVINIZER_INIT_ALLOWED_ORIGINS` | Optional | - | Comma-separated list of allowed CORS origins |

### Priority Order

Environment variables override configuration file values in the following order (highest priority first):

1. **Environment variables** (e.g., `LOG_LEVEL`, `JAVINIZER_DB`)
2. **Configuration file** (`configs/config.yaml`)
3. **Default values** (from `DefaultConfig()`)

### Docker Example

Create a `.env` file from the example:

```bash
cp .env.example .env
```

Example `.env` file for Docker:

```bash
# User/Group mapping
PUID=1000
PGID=1000

# Ports
HOST_PORT=8765
FLARESOLVERR_HOST_PORT=8191

# Timezone
TZ=America/New_York

# Media library path (REQUIRED for Docker)
MEDIA_PATH=/path/to/your/jav-library

# Optional overrides
LOG_LEVEL=debug
UMASK=002
```

Docker Compose automatically loads variables from `.env` file.

## Required vs Optional Settings

Javinizer is designed to work out-of-the-box with minimal configuration. Most settings have sensible defaults, and the application will start successfully without any configuration file (it will create one automatically).

### Settings That Cause Startup Failure

The following settings will cause the application to fail on startup if misconfigured:

| Setting | Failure Condition | Error Message |
|---------|------------------|---------------|
| Config file parsing | Invalid YAML syntax | `Failed to load config: <error>` |
| Config version | Newer version than supported | `config version X is newer than supported version Y; please update Javinizer` |
| JAVStash scraper | Enabled without API key | `javstash: api_key is required (set in config)` (or set the `JAVSTASH_API_KEY` env var) |
| Scrapers proxy | `enabled: true` without `default_profile` | `scrapers.proxy.default_profile is required when scrapers.proxy.enabled is true` |
| Scrapers proxy | `default_profile` / per-scraper `profile` references an unknown profile | `scrapers.proxy.default_profile references unknown profile "X"` |

**Note:** Javinizer creates a default configuration file on first run if one doesn't exist. No settings are required to be set manually.

### Settings That Cause Scraping Failure

These settings don't prevent startup but may cause operations to fail:

| Setting | Failure Condition | Context |
|---------|------------------|---------|
| Proxy profile | Enabled proxy whose selected profile has an empty `url` | Requests go direct (no proxy); may fail on region-blocked sources |
| Translation provider | Enabled without API key | Translation will fail but scraping continues |
| Required fields | Missing required field data | Aggregation fails if `metadata.required_fields` has missing data |

### Settings with Validation Warnings

The application logs warnings for misconfigured settings but continues to run:

| Setting | Warning Condition | Behavior |
|---------|------------------|----------|
| Umask | Invalid octal value | Error logged, umask not applied |

### Default Behavior Without Configuration

When no configuration file exists:

1. **Config creation**: Javinizer creates `configs/config.yaml` with all default values
2. **Database**: SQLite database created at `data/javinizer.db`
3. **Logs**: Output to `stdout` and `data/logs/javinizer.log`
4. **Scrapers**: All scrapers available with default settings
5. **Server**: Binds to `localhost:8765`
6. **Security**: Empty allowed directories denies all API file access (secure by default). Configure `api.security.allowed_directories` to enable access.

### Minimal Configuration Required

For most use cases, you only need to customize:

```yaml
# Required for API file operations (empty allowed_directories denies all access)
api:
  security:
    allowed_directories:
      - /path/to/media
```

All other settings have working defaults.

## Default Values

Javinizer provides sensible defaults for all configuration options. These defaults are defined in the `DefaultConfig()` function and ensure the application works immediately after installation.

### Server Defaults

```yaml
server:
  host: localhost
  port: 8765

api:
  security:
    allowed_directories: []  # Empty = deny all API file access (secure by default)
    denied_directories: []   # Only built-in system directories blocked
    max_files_per_scan: 10000
    scan_timeout_seconds: 30
    rate_limit:
      requests_per_minute: 60
    trusted_proxies: []
    force_secure_cookies: false
    allowed_origins:
      - "http://localhost:8765"
      - "http://localhost:5173"
      - "http://localhost:5174"
      - "http://127.0.0.1:8765"
      - "http://127.0.0.1:5173"
      - "http://127.0.0.1:5174"
```

### Scraper Defaults

```yaml
scrapers:
  user_agent: ""  # Default: Chrome-like UA. r18dev uses the Javinizer UA automatically.
  referer: "https://www.dmm.co.jp/"
  timeout_seconds: 30
  request_timeout_seconds: 60
  priority:
    - r18dev
    - libredmm
    - dmm
    - javlibrary
    - javdb
    - javbus
    - jav321
    - mgstage
    - tokyohot
    - aventertainment
    - caribbeancom
    - dlgetchu
    - fc2
    - javstash
  scrape_actress: true
  flaresolverr:
    enabled: false
    url: "http://localhost:8191/v1"
    timeout: 30
    max_retries: 3
    session_ttl: 300
  browser:
    enabled: false
    timeout: 30
    max_retries: 3
    headless: true
    stealth_mode: true
  proxy:
    enabled: false
    default_profile: "main"
    profiles:
      main:
        url: ""
        username: ""
        password: ""
      backup:
        url: ""
        username: ""
        password: 
```

### File Matching Defaults

```yaml
file_matching:
  extensions:
    - .mp4
    - .mkv
    - .avi
    - .wmv
    - .flv
  min_size_mb: 0
  exclude_patterns:
    - '*-trailer*'
    - '*-sample*'
  regex_enabled: false
  regex_pattern: '([a-zA-Z|tT28]+-\d+[zZ]?[eE]?)(?:-pt)?(\d{1,2})?'
```

### Output Defaults

```yaml
output:
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"
  file_format: "<ID><IF:MULTIPART>-pt<PART></IF>"
  subfolder_format: ["<ID>"]
  actress_delimiter: ", "
  max_title_length: 100
  max_path_length: 240
  # Max height in pixels for cropped posters. 0 = no cap (preserve source resolution).
  # When a cropped poster exceeds this height, it is downscaled preserving aspect ratio.
  max_poster_height: 0
  move_subtitles: false
  subtitle_extensions:
    - .srt
    - .ass
    - .ssa
    - .smi
    - .vtt
  # Move files instead of copying (default: false / copy). Persisted by the TUI Settings toggle.
  move_files: false
  rename_file: true
  group_actress: false
  # group_actress_name: "@Group"
  # group_unknown_actress_name: "@Unknown"
  # first_name_order: false
  poster_format: "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg"
  fanart_format: "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg"
  trailer_format: "<ID>-trailer.mp4"
  screenshot_format: "fanart<INDEX>.jpg"
  screenshot_folder: "extrafanart"
  screenshot_padding: 1
  actress_folder: ".actors"
  actress_format: "<ACTORNAME>.jpg"
  download_cover: true
  download_poster: true
  download_extrafanart: true
  download_trailer: true
  download_actress: true
  download_timeout: 60
```

### Database Defaults

```yaml
database:
  type: sqlite
  dsn: data/javinizer.db
  log_level: silent
```

### Logging Defaults

```yaml
logging:
  level: info
  format: text
  output: "stdout,data/logs/javinizer.log"
  max_size_mb: 10
  max_backups: 5
  max_age_days: 0
  compress: true
```

### Performance Defaults

```yaml
performance:
  max_workers: 5
  worker_timeout: 300
  buffer_size: 100
  update_interval: 100
```

### System Defaults

```yaml
system:
  umask: "002"
  version_check_enabled: true
  version_check_interval_hours: 24
  temp_dir: data/temp
```

### NFO Defaults

```yaml
metadata:
  nfo:
    enabled: true
    display_title: "<TITLE>"
    filename_template: "<ID>.nfo"
    first_name_order: true
    actress_language_ja: false
    per_file: false
    unknown_actress_mode: "skip"
    unknown_actress_text: "Unknown"
    actress_as_tag: false
    add_generic_role: false
    alt_name_role: false
    include_originalpath: false
    include_stream_details: false
    include_fanart: true
    include_trailer: true
    rating_source: "r18dev"  # First scraper in default priority list
    tag: []
    tagline: ""
    credits: []
```

### Translation Defaults

```yaml
metadata:
  translation:
    enabled: false
    provider: openai
    source_language: ja
    target_language: en
    timeout_seconds: 60
    apply_to_primary: true
    overwrite_existing_target: true
    fields:
      title: true
      original_title: true
      description: true
      director: true
      maker: true
      label: true
      series: true
      genres: true
      actresses: true
    openai:
      base_url: "https://api.openai.com/v1"
      api_key: ""
      model: "gpt-4o-mini"
    deepl:
      mode: "free"
      base_url: ""
      api_key: ""
    google:
      mode: "free"
      base_url: ""
      api_key: ""
    openai_compatible:
      base_url: "http://localhost:11434/v1"
      api_key: ""
      model: ""
    anthropic:
      base_url: "https://api.anthropic.com"
      api_key: ""
      model: "claude-sonnet-4-20250514"
```

### Metadata Management Defaults

```yaml
metadata:
  actress_database:
    enabled: true
    auto_add: true
    convert_alias: false
  genre_replacement:
    enabled: true
    auto_add: true
  word_replacement:
    enabled: false  # Opt-in: rewrites all text fields from the replacement database
  tag_database:
    enabled: false
  ignore_genres: []
```

**word_replacement**: Opt-in (default `false`). When enabled, text fields are
rewritten using the word-replacement database. Because it rewrites all text
fields, it is off by default — enable it only if you maintain a replacement
database.

### How Defaults Are Applied

1. **First run**: If no config file exists, Javinizer creates one with all defaults
2. **Config migration**: If config file has older `config_version`, missing fields are filled with defaults
3. **Environment overrides**: Environment variables override both config and defaults
4. **CLI flags**: Command-line flags override all other sources

To view your current configuration with all defaults applied:

```bash
javinizer info
```

## Per-Environment Configuration

Javinizer does not use separate configuration files for different environments (development, staging, production). Instead, use environment variables and Docker configurations for environment-specific settings.

### Environment Variable Strategy

Use environment variables to override settings per environment:

**Development:**
```bash
export LOG_LEVEL=debug
export JAVINIZER_DB=data/javinizer-dev.db
export JAVINIZER_TEMP_DIR=/tmp/javinizer-dev
javinizer web
```

**Production:**
```bash
export LOG_LEVEL=info
export JAVINIZER_DB=/var/lib/javinizer/javinizer.db
export UMASK=022
javinizer web
```

### Docker Environment Configuration

Docker deployments use `.env` files for per-environment settings:

**Development (`.env.dev`):**
```bash
LOG_LEVEL=debug
JAVINIZER_DEV_MODE=true
VITE_API_URL=http://localhost:8765
```

**Production (`.env.prod`):**
```bash
LOG_LEVEL=info
TZ=UTC
PUID=1000
PGID=1000
UMASK=022
```

Use with Docker Compose:

```bash
# Development
docker compose --env-file .env.dev up

# Production
docker compose --env-file .env.prod up
```

### Config File Location Strategy

For different environments, maintain separate config directories:

```bash
# Directory structure
configs/
  ├── config.yaml           # Default/shared config
  ├── dev/
  │   └── config.yaml       # Development overrides
  └── prod/
      └── config.yaml       # Production overrides

# Development
export JAVINIZER_CONFIG=configs/dev/config.yaml
javinizer web

# Production
export JAVINIZER_CONFIG=configs/prod/config.yaml
javinizer web
```

### Docker Volume Strategy

Use Docker volumes to inject environment-specific configuration:

```yaml
# docker-compose.yml
services:
  javinizer:
    image: ghcr.io/javinizer/javinizer-go:latest
    volumes:
      - ./config/prod:/config:ro  # Production config
      - ./data:/data
    environment:
      - JAVINIZER_CONFIG=/config/config.yaml
```

### Common Environment-Specific Settings

**Security (production only):**
```yaml
api:
  security:
    allowed_directories:
      - /media
    allowed_origins:
      - "https://javinizer.example.com"
```

**Logging (per environment):**
- Development: `LOG_LEVEL=debug`
- Staging: `LOG_LEVEL=info`
- Production: `LOG_LEVEL=warn`

**Database (per environment):**
- Development: `JAVINIZER_DB=data/javinizer-dev.db`
- Production: `JAVINIZER_DB=/var/lib/javinizer/javinizer.db`

**CORS origins (per environment):**
- Development: `http://localhost:*`
- Production: `https://your-domain.com`

### Secrets Management

For production deployments, avoid storing sensitive values in config files:

**Using environment variables for secrets:**
```bash
# Translation API keys
export OPENAI_API_KEY=sk-...
export DEEPL_API_KEY=...

# Scraper API keys
export JAVSTASH_API_KEY=...

# GitHub token for version check
export GH_TOKEN=ghp_...
```

**Using Docker secrets:**
```yaml
services:
  javinizer:
    environment:
      - OPENAI_API_KEY_FILE=/run/secrets/openai_api_key
    secrets:
      - openai_api_key

secrets:
  openai_api_key:
    file: ./secrets/openai_api_key.txt
```

**Using Kubernetes secrets:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: javinizer-secrets
type: Opaque
data:
  openai-api-key: <base64-encoded>
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: javinizer
        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: javinizer-secrets
              key: openai-api-key
```

---

**Next**: [CLI Reference](./03-cli-reference.md)
