# Javinizer TUI Guide

## Overview

The Javinizer TUI (Terminal User Interface) provides an interactive way to browse, select, and process JAV files with real-time progress tracking. Built with Bubble Tea, it offers a modern, responsive interface for batch file operations.

## Features

- **Interactive File Browser**: Navigate and select multiple files with keyboard shortcuts
- **Real-Time Progress Tracking**: Monitor concurrent task execution with live updates
- **Task Dashboard**: View statistics and overall progress
- **Live Logging**: See detailed operation logs as they happen
- **Concurrent Processing**: Process multiple files in parallel with configurable worker pool
- **Help System**: Built-in keyboard shortcut reference

## Installation

```bash
# Build from source
go build -o javinizer ./cmd/javinizer

# Or install directly
go install github.com/javinizer/javinizer-go/cmd/javinizer@latest
```

## Usage

### Basic Usage

```bash
# Launch TUI in current directory
javinizer tui

# Scan a specific directory
javinizer tui /path/to/jav/files

# Scan recursively (default)
javinizer tui /path/to/files -r

# Non-recursive scan
javinizer tui /path/to/files --recursive=false
```

### Advanced Options

```bash
# Specify source and destination
javinizer tui --source /source/path --dest /destination/path

# Or use positional argument
javinizer tui /source/path -d /destination/path

# Move files instead of copying
javinizer tui /source/path -d /dest/path -m

# Hard-link files instead of copying (incompatible with --move)
javinizer tui /source/path -d /dest/path --link-mode hard

# Dry-run mode (preview only)
javinizer tui /source/path --dry-run

# Download extrafanart (screenshots)
javinizer tui /source/path --extrafanart

# Custom scraper priority
javinizer tui /source/path --scrapers r18dev,dmm

# Combine options
javinizer tui /source \
  -d /dest \
  --move \
  --recursive \
  --extrafanart \
  --scrapers dmm,r18dev
```

### Available Flags

```bash
-s, --source string      # Source directory to scan (alternative to positional arg)
-d, --dest string        # Destination directory (default: same as source)
-r, --recursive          # Scan subdirectories recursively (default true)
-m, --move               # Move files instead of copying
-n, --dry-run            # Preview operations without making changes
    --link-mode string   # Link mode for copy operations: none, hard, soft (default "none")
    --extrafanart        # Download extrafanart (screenshots)
-p, --scrapers strings   # Scraper priority (comma-separated)
    --update-mode        # Update mode: merge metadata with existing NFO and skip file organization
    --preset string      # Merge strategy preset: conservative, gap-fill, or aggressive (update mode)
    --scalar-strategy string  # Scalar field merge strategy for update mode (default "prefer-nfo")
    --array-strategy string   # Array field merge strategy for update mode (default "merge")
-v, --verbose            # Enable debug logging
```

### Update Mode

Pass `--update-mode` to merge freshly scraped metadata into existing NFO files without moving or renaming the video files. This mirrors the `javinizer update` CLI command (see [CLI Reference](./03-cli-reference.md#update)). Use `--preset` (`conservative`, `gap-fill`, `aggressive`) or the explicit `--scalar-strategy` / `--array-strategy` flags to control how existing NFO values are preserved or overwritten.

`--preset` expands to a fixed scalar + array strategy pair:

| Preset | Scalar strategy | Array strategy | Behavior |
|--------|------------------|-----------------|----------|
| `conservative` | `preserve-existing` | `merge` | Keep all existing NFO values; only merge missing array entries |
| `gap-fill` | `fill-missing-only` | `merge` | Only fill NFO fields that are empty; merge arrays |
| `aggressive` | `prefer-scraper` | `replace` | Trust freshly scraped data; overwrite existing NFO entirely |

When `--preset` is set it overrides `--scalar-strategy` and `--array-strategy`. Valid scalar strategies are `prefer-nfo` (default), `prefer-scraper`, `merge-arrays`, `preserve-existing`, and `fill-missing-only`; valid array strategies are `merge` (default) and `replace`. You can also toggle update mode at runtime from the Settings view (see below).

### Link Mode

`--link-mode` controls how files are placed in the destination during copy operations:

| Value | Behavior |
|-------|----------|
| `none` (default) | Copy files normally |
| `hard` | Create hard links in the destination instead of copying |
| `soft` | Create symbolic links in the destination instead of copying |

Link mode is mutually exclusive with move mode. The TUI rejects the combination at startup — whether move mode comes from the `--move` flag or from `move_files: true` in `config.yaml` — but the two paths report it differently. When `--move` is set, startup fails with `--link-mode can only be used when --move is disabled`; when move mode comes only from `move_files: true` in `config.yaml` (with no `--move` flag), startup fails with `--link-mode can only be used when move mode is disabled (move_files is false and --move is not set)`. The Settings view likewise refuses to enable the Move Files toggle while link mode is active.

## Interface

### Views

The TUI has four main tab views accessible via number keys or Tab:

1. **Browser (1)**: File selection and management
2. **Dashboard (2)**: Statistics and progress overview
3. **Logs (3)**: Real-time operation logging
4. **Settings (4)**: Runtime processing toggles

The help view is available with `?`.

### Browser View

The browser displays discovered video files with their match status:

```
Files
----------------------------------------
☐ IPX-123.mp4              [Matched]
☑ ABP-456.mkv              [Matched]
☐ STARS-789.mp4            [Matched]
☐ random_file.mp4          [Not Matched]

45/120 files | 3 selected
```

**Indicators:**
- `☐` - Not selected
- `☑` - Selected for processing
- `[Matched]` - JAV ID successfully identified
- `[Not Matched]` - No JAV ID found

### Dashboard View

Displays real-time statistics:

```
Dashboard
----------------------------------------
Total:     120
Running:   5
Success:   45
Failed:    2

Progress:  42.3%
Elapsed:   2m 15s
```

### Task List

Shows active and recently completed tasks:

```
Tasks
----------------------------------------
[RUN] [████████░░] scrape-IPX-123
[OK]  [██████████] download-ABP-456
[ERR] [█████░░░░░] organize-STARS-789
[...] [░░░░░░░░░░] nfo-IPX-123
```

**Status Indicators:**
- `[RUN]` - Currently running
- `[OK]` - Completed successfully
- `[ERR]` - Failed with error
- `[...]` - Pending/queued

### Log View

Real-time scrollable logs:

```
Logs
----------------------------------------
15:04:32 [INFO]  Scanned 120 files
15:04:33 [INFO]  Matched 98 JAV IDs
15:04:35 [INFO]  Started processing
15:04:36 [INFO]  Scraped IPX-123
15:04:37 [WARN]  Rate limit reached, waiting...
15:04:40 [ERROR] Failed to download: connection timeout
```

### Settings View

A list of runtime processing toggles you can flip without restarting the TUI. Navigate with `↑`/`↓` (or `k`/`j`) and press `Space` to flip the highlighted row:

| # | Setting | Effect when enabled |
|---|---------|---------------------|
| 0 | Dry Run | Preview operations without writing any files |
| 1 | Force Update | Overwrite existing organized files/NFO |
| 2 | Force Refresh | Clear cached DB metadata and rescrape |
| 3 | Move Files | Move files instead of copying (persisted to `config.yaml`; cannot be enabled while link mode is active) |
| 4 | Scrape | Query scrapers for metadata |
| 5 | Download | Fetch cover, poster, screenshots, and actress images |
| 6 | Extrafanart | Download extrafanart (screenshots) |
| 7 | Organize | Move/copy files to the destination with formatted names |
| 8 | NFO | Generate Kodi-compatible NFO files |
| 9 | Update Mode | Merge metadata into existing NFO without organizing files (auto-disables Organize) |

Toggling **Move Files** (row 3) is written back to `config.yaml` so it survives restarts. Toggling **Update Mode** (row 9) automatically disables — and later re-enables — **Organize**.

## Keyboard Shortcuts

### Global

| Key | Action |
|-----|--------|
| `?` | Toggle help view |
| `1` / `b` | Switch to browser view |
| `2` | Switch to dashboard view |
| `3` | Switch to logs view |
| `4` | Switch to settings view |
| `Tab` | Cycle through views |
| `d` | Dismiss the processing-complete banner |
| `q` / `Ctrl+C` | Quit application |

### Browser View

| Key | Action |
|-----|--------|
| `f` | Open source folder picker |
| `o` | Open output folder picker |
| `m` | Open manual search modal |
| `M` | Open actress merge modal |
| `r` | Refresh/rescan current source path |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Space` | Toggle file selection |
| `a` | Select all matched files |
| `A` | Deselect all files |
| `Enter` | Start processing selected files |
| `p` | Pause/resume processing |

### Manual Search Modal

From Browser view, press `m` to open the manual search modal. Type a JAV ID or URL and tick exactly which scrapers to query (none are selected by default — pick at least one).

| Key | Action |
|-----|--------|
| `Tab` | Toggle focus between the ID input and the scraper list |
| `↑` / `↓` | Move the scraper-list cursor (when the list is focused) |
| `Space` | Tick/untick the highlighted scraper (when the list is focused) |
| `Enter` | Run the scrape with the entered ID and selected scrapers |
| `Esc` | Cancel and close |

### Actress Merge Modal

From Browser view, press `M` to open the manual actress merge modal. This merges one actress record (the **source**) into another (the **target**, the survivor) and re-points the source's movies and aliases to the target.

**Input step** — enter numeric actress IDs:

| Key | Action |
|-----|--------|
| `Tab` / `↑` / `↓` | Switch between Target ID and Source ID fields |
| `Enter` | On Target: move focus to Source. On Source: load the conflict preview |
| `Esc` / `q` | Cancel and close |

**Conflict step** — resolve differing fields before applying:

| Key | Action |
|-----|--------|
| `↑` / `k` | Previous conflicting field |
| `↓` / `j` | Next conflicting field |
| `t` / `h` / `←` | Keep the target value for this field |
| `s` / `l` / `→` | Use the source value for this field |
| `Space` | Toggle between target and source for this field |
| `Enter` | Apply the merge |
| `r` | Go back to ID input |
| `Esc` / `q` | Cancel and close |

**Result step** — shows updated-movie, resolved-conflict, and added-alias counts:

| Key | Action |
|-----|--------|
| `Enter` / `Esc` / `q` | Close the modal |
| `r` | Start a new merge keeping the same target ID |

### Logs View

| Key | Action |
|-----|--------|
| `↑` / `k` | Scroll up |
| `↓` / `j` | Scroll down |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `a` | Toggle auto-scroll |

### Dashboard View

| Key | Action |
|-----|--------|
| `r` | Reset the elapsed-time clock / refresh statistics |

### Settings View

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Space` | Toggle the highlighted setting |

## Configuration

The TUI uses settings from `configs/config.yaml`:

```yaml
performance:
  max_workers: 5          # Concurrent tasks (1-100)
  worker_timeout: 300     # Task timeout in seconds (10-3600)
  buffer_size: 100        # Progress update buffer
  update_interval: 100    # UI refresh rate in ms (10-5000)

logging:
  output: "stdout,data/logs/javinizer.log"  # See note below
  level: info             # debug, info, warn, error
  format: text            # text or json
  max_size_mb: 10         # Max size in MB before rotation (0 = disabled)
  max_backups: 5          # Rotated files to keep (0 = unlimited)
  max_age_days: 0         # Max age in days to keep (0 = no limit)
  compress: true          # Compress rotated files
```

The TUI runs with `tea.WithAltScreen`, so the logger is reconfigured to **file-only** output at startup: any `stdout`/`stderr` targets in `logging.output` are stripped so logs cannot corrupt the TUI display. The remaining file target is used as-is, so with the default config the TUI writes to `data/logs/javinizer.log`. If `logging.output` contains no file target at all, it falls back to `data/logs/javinizer-tui.log`. Log rotation settings (`max_size_mb`, `max_backups`, `max_age_days`, `compress`) are preserved.

### Performance Tuning

**max_workers**: Number of concurrent tasks
- **Low (1-3)**: Slow but gentle on system/network
- **Medium (4-6)**: Balanced performance
- **High (7-10)**: Fast but resource-intensive
- **Very High (11+)**: Maximum speed, may hit rate limits

**worker_timeout**: Maximum time per task
- Increase for slow networks
- Decrease to fail fast on stuck tasks

**buffer_size**: Progress update queue size
- Increase if processing many files (100+)
- Default (100) works for most cases

## Processing Pipeline

When you press Enter, each selected file goes through:

1. **Scrape**: Query your configured scrapers (e.g. R18Dev, DMM) for metadata
2. **Download**: Fetch cover, poster, screenshots, and actress images
3. **Organize**: Move/copy file to destination with formatted name
4. **NFO**: Generate Kodi-compatible NFO file with metadata

Tasks run concurrently up to `max_workers` limit.

## Workflow Example

### Basic Workflow

1. Launch TUI:
   ```bash
   javinizer tui /path/to/videos
   ```

2. Wait for scan to complete (shows in logs)

3. Review matched files in browser

4. Select files:
   - Use arrow keys to navigate
   - Press `Space` to select individual files
   - Or press `a` to select all matched files

5. Press `Enter` to start processing

6. Monitor progress:
   - Press `2` for dashboard view
   - Press `3` to watch logs
   - Press `1` to return to browser

7. Press `q` when finished

### Advanced Workflow

```bash
# Step 1: Scan and organize
javinizer tui /downloads -d /organized --move

# In TUI:
# - Select files
# - Press Enter
# - Wait for completion
# - Press Tab to view logs
# - Press q to exit
```

## Error Handling

### Common Errors

**"No scrapers available"**
- Configure at least one scraper in config.yaml
- Check scraper credentials if required

**"Failed to scrape"**
- Scraper may be down or rate-limiting
- Try again later or use different scraper

**"Download failed"**
- Network issue or rate limit
- Files may be unavailable on source

**"Organize failed"**
- Destination path doesn't exist
- Permission issues
- Disk full

### Recovery

- Failed tasks are logged with details
- Other tasks continue processing
- Re-run TUI to retry failed files
- Check the log file (default `data/logs/javinizer.log` — see [Configuration](#configuration))

## Tips & Tricks

1. **Use filters**: Deselect files you don't want by pressing `A` then manually selecting

2. **Monitor resources**: Switch to dashboard view to see active workers

3. **Pause if needed**: Press `p` to pause, make changes, then `p` to resume

4. **Check logs often**: Press `3` to catch errors early

5. **Rate limiting**: Reduce `max_workers` if seeing many failures

6. **Test first**: Try a few files before processing entire library

7. **Use dry-run**: Test organization with the `sort` command first:
   ```bash
   javinizer sort /path --dry-run
   ```

## Troubleshooting

### TUI doesn't start

```bash
# Check terminal size (minimum 80x24)
echo $COLUMNS x $LINES

# Try with explicit path
javinizer tui .

# Check logs (default file target from logging.output)
cat data/logs/javinizer.log
```

### Files not matched

- Check filename format (should contain JAV ID)
- Verify matcher configuration in config.yaml
- Run `javinizer sort /path --dry-run` to test matching

### Processing stuck

- Press `2` to view dashboard
- Check if workers are active
- Press `q` to quit and check logs
- May need to increase `worker_timeout`

### UI glitches

- Resize terminal
- Press `Ctrl+L` to redraw
- Ensure terminal supports UTF-8

## Technical Details

### Architecture

```
┌─────────────────┐
│   Bubble Tea    │  UI Framework
├─────────────────┤
│   TUI Model     │  State Management
├─────────────────┤
│   Coordinator   │  Task Orchestration
├─────────────────┤
│   Worker Pool   │  Concurrent Execution
├─────────────────┤
│   Progress      │  Status Tracking
│   Tracker       │
└─────────────────┘
```

### Components

- **Model**: Application state and logic
- **Views**: Browser, Dashboard, Logs, Settings (Help is a `?` toggle overlay, not a tab)
- **Components**: Reusable UI widgets
- **Coordinator**: Task submission and lifecycle
- **Worker Pool**: Concurrent task execution
- **Progress Tracker**: Thread-safe progress monitoring

### Threading

- **Main thread**: UI rendering and events
- **Worker goroutines**: Task execution (limited by `max_workers`)
- **Progress goroutine**: Update collection and notification
- **Tick goroutine**: Periodic UI refresh

All goroutines coordinate via channels for thread safety.

## See Also

- [Configuration Guide](./02-configuration.md)
- [CLI Reference](./03-cli-reference.md)
- [File Matching](./02-configuration.md#file-matching)
- [Scraper Setup](./02-configuration.md#scraper-configuration)
