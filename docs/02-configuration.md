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

## Server Settings

Configure the REST API server (future feature):

```yaml
server:
  host: localhost  # Bind address
  port: 8080       # Listen port
```

## Scraper Configuration

### Overview

Javinizer supports multiple metadata scrapers that can be enabled/disabled and prioritized.

### General Scraper Settings

```yaml
scrapers:
  user_agent: "Javinizer (+https://github.com/javinizer/Javinizer)"
  priority:
    - r18dev  # Try R18.dev first
    - dmm     # Fall back to DMM
```

**user_agent**: HTTP User-Agent header sent to scraper websites. This identifies your scraper to the server.

**priority**: Order to query scrapers. First scraper is tried first. If it fails, the next one is attempted.

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
    enabled: true           # Enable/disable DMM scraper
    scrape_actress: false   # Include actress data from DMM
```

**scrape_actress**: Whether to scrape actress information from DMM. This is slower due to HTML parsing.

**Pros**:
- Official source
- Accurate release dates
- Detailed descriptions

**Cons**:
- Slower (HTML parsing)
- May require more requests

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

### Field-by-Field Priority

```yaml
metadata:
  priority:
    # Basic Information
    id:
      - r18dev
      - dmm

    content_id:
      - r18dev
      - dmm

    title:
      - r18dev
      - dmm

    alternate_title:
      - r18dev
      - dmm

    # Descriptions favor DMM (more detailed)
    description:
      - dmm
      - r18dev

    # Release Information
    release_date:
      - r18dev
      - dmm

    runtime:
      - r18dev
      - dmm

    # Studio/Production
    director:
      - r18dev
      - dmm

    maker:
      - r18dev
      - dmm

    label:
      - r18dev
      - dmm

    series:
      - r18dev
      - dmm

    # Media
    cover_url:
      - r18dev
      - dmm

    screenshot_url:
      - r18dev
      - dmm

    trailer_url:
      - r18dev
      - dmm

    # Categorical
    actress:
      - r18dev
      - dmm

    genre:
      - r18dev
      - dmm

    # Ratings favor DMM
    rating:
      - dmm
      - r18dev
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

### CSV Settings (Legacy - Now Database-Based)

These settings are maintained for backward compatibility but are no longer used:

```yaml
metadata:
  thumb_csv:
    enabled: true
    path: data/actress.csv
    auto_add: true

  genre_csv:
    enabled: true
    path: data/genres.csv
    auto_add: true
```

**Note**: Javinizer Go uses SQLite database for actress and genre management. See [Genre Management](./05-genre-management.md) for the new approach.

## NFO Settings

Configure Kodi/Plex-compatible NFO file generation.

```yaml
metadata:
  nfo:
    enabled: true                    # Generate NFO files
    display_name: <TITLE>            # Movie display name in NFO
    filename_template: <ID>.nfo      # NFO filename pattern
    first_name_order: true           # Actress name order (true = First Last)
    actress_language_ja: false       # Use Japanese actress names
    unknown_actress_text: Unknown    # Placeholder for missing actress
    actress_as_tag: false            # Add actress names as tags
    include_stream_details: false    # Include video stream metadata
    include_fanart: true             # Include fanart URL
    include_trailer: true            # Include trailer URL
    rating_source: themoviedb        # Rating source identifier
    tag: []                          # Additional custom tags
    tagline: ""                      # Custom tagline template
    credits: []                      # Additional credits
```

### NFO Field Details

**enabled**: Master switch for NFO generation.

**display_name**: Template for the `<title>` field in NFO. Uses template tags (see [Template System](./04-template-system.md)).

**filename_template**: Pattern for NFO filename. Default `<ID>.nfo` creates files like `IPX-535.nfo`.

**first_name_order**:
- `true`: "Momo Sakura"
- `false`: "Sakura Momo"

**actress_language_ja**: Use Japanese names when available (e.g., "桜空もも" instead of "Momo Sakura").

**unknown_actress_text**: Placeholder text when actress information is missing.

**actress_as_tag**: If true, adds each actress name as a `<tag>` in the NFO for better searchability.

**include_stream_details**: Adds `<fileinfo><streamdetails>` section (requires video file analysis - not yet implemented).

**include_fanart**: Includes `<fanart>` URL in NFO.

**include_trailer**: Includes `<trailer>` URL in NFO.

**rating_source**: Source identifier for the rating (e.g., "themoviedb", "imdb", "dmm").

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
  file_format: "<ID>"
  subfolder_format: []  # Optional nested folder hierarchy
  delimiter: ", "
  download_cover: true
  download_poster: true
  download_extrafanart: false  # Screenshots in extrafanart/ subfolder
  download_trailer: false
  download_actress: false
```

### Naming Templates

**folder_format**: Template for folder names. Example result:
```
IPX-535 [Idea Pocket] - Beautiful Day (2020)/
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
  IPX-535 [Idea Pocket] - Title (2020)/
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
    IPX-535 [Idea Pocket] - Title (2020)/
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
      IPX-535 [Idea Pocket] - Title (2020)/
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

See [Template System](./04-template-system.md) for available tags and modifiers.

### Download Options

**download_cover**: Download cover/poster image (`<ID>-poster.jpg`).

**download_poster**: Download poster image (`<ID>-fanart.jpg`).

**download_extrafanart**: Download screenshot images to `extrafanart/` subfolder (`fanart1.jpg`, `fanart2.jpg`, etc.).

**Note**: In the original Javinizer, screenshots and extrafanart refer to the same thing. The screenshots are saved in the `extrafanart/` subfolder as `fanart<number>.jpg` files for Kodi/Plex compatibility.

**download_trailer**: Download trailer video (`<ID>-trailer.mp4`).

**download_actress**: Download actress thumbnail images to `.actors/` subfolder.

**actress_format**: Template for actress image filenames (default: `<ACTORNAME>.jpg`). Supports template variables like `<ID>`, `<ACTORNAME>`, etc. Examples:
- `<ACTORNAME>.jpg` - Default, matches original Javinizer (e.g., `白上咲花.jpg`)
- `<ID>_<ACTORNAME>.jpg` - Include movie ID (e.g., `SONE-860_白上咲花.jpg`)
- `actress-<ACTORNAME>.jpg` - With prefix (e.g., `actress-白上咲花.jpg`)

### Delimiter

**delimiter**: String used to join multiple values (e.g., actress names, genres) in templates.

Example with `delimiter: ", "`:
```
Actresses: Momo Sakura, Yua Mikami, Rara Anzai
```

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

Javinizer appends to log files. For log rotation, use external tools like:

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
  download_screenshots: false
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
  download_screenshots: true
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
    IPX-535 - Beautiful Day/
      IPX-535 - Beautiful Day.mp4
```

## Validation

Check your configuration:

```bash
javinizer info
```

This displays your current configuration and verifies it's valid.

## Advanced Tips

1. **Backup your config**: Keep a copy of `config.yaml` with your preferred settings
2. **Test changes with dry-run**: Use `--dry-run` to preview the effect of config changes
3. **Genre filtering**: Use `ignore_genres` to filter unwanted categories
4. **Priority tuning**: Experiment with different scraper priorities for best results
5. **Template testing**: Test folder/file formats before processing large batches

---

**Next**: [CLI Reference](./03-cli-reference.md)
