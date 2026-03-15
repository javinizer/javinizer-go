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
    --extrafanart        # Download extrafanart (screenshots)
-p, --scrapers strings   # Scraper priority (comma-separated)
-v, --verbose            # Enable debug logging
```

## Interface

### Views

The TUI has four main views accessible via number keys or Tab:

1. **Browser (1)**: File selection and management
2. **Dashboard (2)**: Statistics and progress overview
3. **Logs (3)**: Real-time operation logging
4. **Help (?)**: Keyboard shortcuts reference

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

## Keyboard Shortcuts

### Global

| Key | Action |
|-----|--------|
| `?` | Toggle help view |
| `1` | Switch to browser view |
| `2` | Switch to dashboard view |
| `3` | Switch to logs view |
| `Tab` | Cycle through views |
| `q` / `Ctrl+C` | Quit application |

### Browser View

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Space` | Toggle file selection |
| `a` | Select all matched files |
| `A` | Deselect all files |
| `Enter` | Start processing selected files |
| `p` | Pause/resume processing |

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
| `r` | Refresh statistics |

## Configuration

The TUI uses settings from `configs/config.yaml`:

```yaml
performance:
  max_workers: 5          # Concurrent tasks (1-20)
  worker_timeout: 300     # Task timeout in seconds
  buffer_size: 100        # Progress update buffer
  update_interval: 100    # UI refresh rate (ms)

logging:
  output: data/logs/javinizer-tui.log  # Log file location
  level: info             # debug, info, warn, error
  format: text            # text or json
```

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

1. **Scrape**: Query configured scrapers (R18Dev, DMM) for metadata
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
- Check logs at `data/logs/javinizer-tui.log`

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

# Check logs
cat data/logs/javinizer-tui.log
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
- **Views**: Browser, Dashboard, Logs, Help
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

- [Configuration Guide](../README.md#configuration)
- [CLI Reference](../README.md#cli-commands)
- [Matcher Configuration](../README.md#matching)
- [Scraper Setup](../README.md#scrapers)
