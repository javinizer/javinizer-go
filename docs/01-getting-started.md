# Getting Started with Javinizer Go

Javinizer Go is a modern, high-performance metadata scraper and file organizer for Japanese Adult Videos (JAV). This guide will help you get started quickly.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Initial Setup](#initial-setup)
- [Your First Scrape](#your-first-scrape)
- [Your First Sort Operation](#your-first-sort-operation)
- [Next Steps](#next-steps)

## Prerequisites

### System Requirements

- **Operating System**: Linux, macOS, or Windows
- **Go**: Version 1.21 or higher (if building from source)
- **Disk Space**: ~50MB for the binary, additional space for database and downloaded media

### Optional

- **Internet Connection**: Required for scraping metadata
- **Video Files**: JAV files with recognizable IDs in the filename (e.g., `IPX-535.mp4`)

## Installation

### Option 1: Download Pre-built Binary (Recommended)

1. Download the latest release for your platform from the [Releases page](https://github.com/javinizer/javinizer-go/releases)

2. Extract the archive:
   ```bash
   # Linux/macOS
   tar -xzf javinizer-go-linux-amd64.tar.gz

   # Windows (use your preferred extraction tool)
   ```

3. Move the binary to your PATH:
   ```bash
   # Linux/macOS
   sudo mv javinizer /usr/local/bin/

   # Windows: Add the directory to your PATH environment variable
   ```

4. Verify installation:
   ```bash
   javinizer --help
   ```

### Option 2: Build from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/javinizer/javinizer-go.git
   cd javinizer-go
   ```

2. Build the binary:
   ```bash
   go build -o bin/javinizer ./cmd/javinizer
   ```

3. Run the binary:
   ```bash
   ./bin/javinizer --help
   ```

## Initial Setup

### 1. Initialize Javinizer

Run the initialization command to create the configuration file and database:

```bash
javinizer init
```

This will:
- Create `configs/config.yaml` with default settings
- Create `data/` directory for the database
- Initialize SQLite database at `data/javinizer.db`

Expected output:
```
Initializing Javinizer...
✅ Created data directory: data
✅ Initialized database: data/javinizer.db
✅ Saved configuration: configs/config.yaml

🎉 Initialization complete!

Next steps:
  - Run 'javinizer scrape IPX-535' to test scraping
  - Run 'javinizer info' to view configuration
```

### 2. Verify Configuration

Check that everything is set up correctly:

```bash
javinizer info
```

You should see output showing:
- Config file location
- Database type and location
- Enabled scrapers
- Priority settings

## Your First Scrape

Let's test the scraper by fetching metadata for a movie:

```bash
javinizer scrape IPX-535
```

Expected output:
```
Scraping metadata for: IPX-535

📡 Scraping from r18dev...
✅ r18dev scraped successfully

📡 Scraping from dmm...
✅ dmm scraped successfully

🎬 Movie: IPX-535
Title: [Example Title]
Studio: Idea Pocket
Release Date: 2020-09-13
Runtime: 120 minutes
Actresses: [Actress Names]
Genres: [Genre List]

💾 Saved to database
```

The metadata is now cached in your local database. Subsequent scrapes of the same ID will be instant!

### Understanding What Happened

1. **Multi-Source Scraping**: Javinizer queried both R18.dev and DMM for metadata
2. **Aggregation**: Data from both sources was combined based on priority settings
3. **Database Caching**: Results were saved to SQLite for fast future access
4. **Genre Replacement**: Any configured genre replacements were applied

## Your First Sort Operation

Now let's organize some video files. First, set up a test directory:

```bash
# Create a test directory
mkdir -p ~/javinizer-test
cd ~/javinizer-test

# Create a test video file (or copy a real one)
touch "IPX-535 Beautiful Day.mp4"
```

### Dry Run (Preview Only)

Always start with a dry run to preview what will happen:

```bash
javinizer sort ~/javinizer-test --dry-run
```

Expected output:
```
📁 Scanning: /Users/you/javinizer-test
Found 1 video file(s)

🔍 Matching files...
✅ Matched: IPX-535 Beautiful Day.mp4 → IPX-535

📡 Scraping metadata...
✅ IPX-535 (cached)

📝 Planning file organization...

Plan for: IPX-535 Beautiful Day.mp4
  Source: /Users/you/javinizer-test/IPX-535 Beautiful Day.mp4
  Target: /Users/you/javinizer-test/IPX-535 [Idea Pocket] - Beautiful Day (2020)/IPX-535.mp4
  NFO: /Users/you/javinizer-test/IPX-535 [Idea Pocket] - Beautiful Day (2020)/IPX-535.nfo
  Media: 5 files (cover, poster, 3 screenshots)

Files organized: 1 (dry-run)

💡 Run without --dry-run to apply changes
```

### Apply Changes

If the plan looks good, run it for real:

```bash
javinizer sort ~/javinizer-test
```

This will:
1. ✅ Create organized folder structure
2. ✅ Move/copy video files with clean names
3. ✅ Generate Kodi-compatible NFO files
4. ✅ Download cover images and screenshots
5. ✅ Download actress thumbnails

### Sort Options

```bash
# Organize files recursively in subdirectories
javinizer sort ~/Videos --recursive

# Move files instead of copying
javinizer sort ~/Videos --move

# Specify output destination
javinizer sort ~/Videos --dest ~/Organized

# Skip NFO generation
javinizer sort ~/Videos --nfo=false

# Skip media downloads
javinizer sort ~/Videos --download=false

# Combine options
javinizer sort ~/Videos --recursive --move --dest ~/Organized
```

## Next Steps

Now that you have the basics working, explore these topics:

### Customize Your Setup

1. **[Configure Priority](./02-configuration.md#metadata-priority)**: Choose which scraper to prefer for each field
2. **[Template System](./04-template-system.md)**: Customize folder and file naming formats
3. **[Genre Management](./05-genre-management.md)**: Replace genre names to match your preferences

### Advanced Usage

- **[CLI Reference](./03-cli-reference.md)**: Complete command documentation
- **[Database Schema](./06-database-schema.md)**: Direct database queries and management
- **[Troubleshooting](./10-troubleshooting.md)**: Common issues and solutions

## Quick Tips

1. **Always use `--dry-run` first** to preview changes before applying them
2. **Keep the database** - it caches metadata for instant lookups
3. **Backup your config** - `configs/config.yaml` contains all your customizations
4. **Use descriptive filenames** - Include the JAV ID for accurate matching
5. **Check genre replacements** - Run `javinizer genre list` to see active replacements

## Getting Help

- **Built-in Help**: `javinizer <command> --help`
- **Configuration Info**: `javinizer info`
- **Troubleshooting Guide**: [10-troubleshooting.md](./10-troubleshooting.md)
- **GitHub Issues**: [Report bugs or request features](https://github.com/javinizer/javinizer-go/issues)

---

**Next**: [Configuration Guide](./02-configuration.md)
