# Getting Started with Javinizer Go

Javinizer Go is a modern, high-performance metadata scraper and file organizer for Japanese Adult Videos (JAV). This guide will help you get started quickly.

## Table of Contents

- [Feature Overview](#feature-overview)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
  - [Pre-built Binary (Linux / macOS)](#pre-built-binary-linux--macos)
  - [Pre-built Binary (Windows)](#pre-built-binary-windows)
  - [Docker](#docker)
  - [Build from Source](#build-from-source)
- [Initial Setup](#initial-setup)
- [Your First Scrape](#your-first-scrape)
- [Your First Sort Operation](#your-first-sort-operation)
  - [Linux / macOS](#linux--macos)
  - [Windows (PowerShell)](#windows-powershell)
- [Next Steps](#next-steps)
- [Quick Tips](#quick-tips)
- [Common Setup Issues](#common-setup-issues)
- [Getting Help](#getting-help)

## Feature Overview

### Multi-Source Scraping

- R18.dev scraper (fast JSON API)
- DMM/Fanza scraper (HTML parsing + browser mode)
- Additional optional scrapers (JavDB, JavLibrary, LibreDMM, and more)
- Configurable metadata priority and aggregation
- Database caching for fast repeat lookups

### File Organization

- Automatic JAV ID detection from filenames
- Template-based folder/file naming
- Nested subfolder hierarchies
- Move/copy operations with conflict handling
- Dry-run preview mode

### Metadata and Media

- Kodi/Plex-compatible NFO generation
- Actress database support (including Japanese names)
- Genre replacement system
- Download support for cover, poster, fanart, trailer, and actress images

### Interfaces

- CLI commands
- Interactive TUI workflow
- API server + web frontend

## Prerequisites

### System Requirements

- **Operating System**: Linux, macOS, or Windows
- **Go**: Version 1.26 or higher (if building from source)
- **Disk Space**: ~50MB for the binary, additional space for database and downloaded media

### Optional

- **Internet Connection**: Required for scraping metadata
- **Video Files**: JAV files with recognizable IDs in the filename (e.g., `IPX-535.mp4`)

## Installation

### Homebrew (macOS / Linux)

Once a stable `v1.0.0` is published, the Homebrew tap is the recommended install on macOS:

```bash
brew tap javinizer/homebrew-tap https://github.com/javinizer/homebrew-tap
brew trust --formula javinizer/tap/javinizer   # required once on Homebrew 6.0+
brew install javinizer
brew upgrade javinizer   # update to the latest stable release later
```

Homebrew 6.0+ requires explicitly trusting third-party taps before installing from them. The `brew trust` step is a one-time setup per tap; alternatively set `HOMEBREW_NO_REQUIRE_TAP_TRUST=1` to skip the check. The formula installs a prebuilt binary (CGO/SQLite is statically linked). The tap is updated automatically on each **stable** release; prereleases never reach it.

### Scoop (Windows)

Once a stable `v1.0.0` is published, the Scoop bucket is the recommended install on Windows:

```powershell
scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer
scoop install javinizer
scoop update javinizer   # update to the latest stable release later
```

The manifest installs the prebuilt `javinizer-windows-amd64.exe` and shims it as `javinizer`. The bucket is updated automatically on each **stable** release; prereleases never reach it. This is the recommended Windows install path until release binaries are Authenticode-signed (issue [#72](https://github.com/javinizer/javinizer-go/issues/72)).

### Desktop app (clickable GUI)

The desktop app opens a native window over the embedded API server and Web UI — the same surface as `javinizer web`, no browser needed. CLI and TUI subcommands remain available inside the same binary. It is a **separate package** from the CLI so both can coexist; see [Desktop App (macOS / Windows / Linux)](17-desktop-app.md) for details.

```bash
# macOS — Homebrew Cask (installs Javinizer.app to /Applications)
brew tap javinizer/homebrew-tap https://github.com/javinizer/homebrew-tap
brew install --cask javinizer-app
# trust the tap first on Homebrew 6.0+ if you skipped it above:
# brew trust --cask javinizer/tap/javinizer-app
```

```powershell
# Windows — Scoop (shim: javinizer-app; Start Menu shortcut: Javinizer)
scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer
scoop install javinizer-app
```

```bash
# Linux — AppImage (direct download; self-contained, no package manager needed)
curl -L -o Javinizer.AppImage https://github.com/javinizer/javinizer-go/releases/latest/download/Javinizer-linux-x86_64.AppImage
chmod +x Javinizer.AppImage
./Javinizer.AppImage

# For arm64 Linux, swap `x86_64` for `aarch64` in the asset name.
```

The app is **unsigned** — expect a one-time Gatekeeper (macOS) or Smart App Control (Windows) prompt on first launch; see [the desktop-app docs](17-desktop-app.md#the-app-is-unsigned). The cask and bucket are updated automatically on each **stable** release; prereleases never reach them.

### One-shot installer

Downloads the latest **stable** release, verifies its SHA256 against `checksums.txt`, and puts `javinizer` on your `PATH`. Prereleases are opt-in: pass `--pre-release` / `-PreRelease` to install the newest release including prereleases.

```bash
# Linux / macOS
curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash
# latest pre-release:
curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash -s -- --pre-release
```

```powershell
# Windows (PowerShell) — installs to %LOCALAPPDATA%\javinizer\bin, no admin required
irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1 | iex
# latest pre-release:
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1))) -PreRelease
```

The Windows installer runs `Unblock-File` to strip the Mark-of-the-Web tag that can otherwise cause an "Access is denied" error under Smart App Control (issue [#72](https://github.com/javinizer/javinizer-go/issues/72)).

### Self-upgrade

After a binary install, update in place without re-downloading by hand:

```bash
javinizer upgrade           # download + verify + replace the running binary
javinizer upgrade --check   # just report whether an update is available
```

If javinizer was installed via Homebrew (or Scoop), `upgrade` detects that and tells you to use `brew upgrade javinizer` / `scoop update javinizer` instead.

`upgrade` is also **environment-aware**: inside a **Docker** container it refuses the in-place swap (the image is read-only) and prints `docker pull ghcr.io/javinizer/javinizer-go:latest` instead; in the **desktop app** it points you to the [releases page](https://github.com/javinizer/javinizer-go/releases) (a bare swap would orphan the app bundle). The Web UI update banner shows the same guidance with an environment badge ("Running in Docker" / "Desktop app" / "CLI install").

> `javinizer upgrade` updates the **program**; `javinizer update` refreshes **metadata** for your files. They are different commands.

### Pre-built Binary (Linux / macOS)

Each release ships a single ready-to-run executable — no archive to extract. Download the asset matching your OS and architecture from the [Releases page](https://github.com/javinizer/javinizer-go/releases), make it executable, and put it on your `PATH`:

```bash
# 1. Download the asset matching your OS/arch from the Releases page:
#    https://github.com/javinizer/javinizer-go/releases  (e.g. javinizer-darwin-arm64)
# 2. Make it executable and put it on your PATH:
chmod +x javinizer
sudo mv javinizer /usr/local/bin/

javinizer --help
```

> **One-shot install:** fetch the latest stable binary directly — no version in the URL:
> ```bash
> curl -L -o javinizer https://github.com/javinizer/javinizer-go/releases/latest/download/javinizer-darwin-arm64
> ```
> Prereleases can't be the “Latest” release on GitHub, so this permalink always points at a stable release.

Available release assets (stable names; `rc` releases used versioned names like `javinizer-v1.0.0-rc2-darwin-arm64`):

| Platform | Asset |
|----------|-------|
| macOS Intel | `javinizer-darwin-amd64` |
| macOS Apple Silicon | `javinizer-darwin-arm64` (or `javinizer-darwin-universal`) |
| Linux x86_64 | `javinizer-linux-amd64` |
| Linux arm64 | `javinizer-linux-arm64` |

> **macOS Gatekeeper**: downloaded binaries may be flagged as an "unidentified developer." If so, right-click the file → *Open* the first time, or strip the quarantine attribute:
> ```bash
> xattr -d com.apple.quarantine /usr/local/bin/javinizer
> ```

### Pre-built Binary (Windows)

Download the Windows executable from the [Releases page](https://github.com/javinizer/javinizer-go/releases) — via your browser, or with PowerShell:

```powershell
# 1. Download javinizer-windows-amd64.exe from the Releases page:
#    https://github.com/javinizer/javinizer-go/releases
# 2. One-shot download — no version in the URL:
#    Invoke-WebRequest -Uri "https://github.com/javinizer/javinizer-go/releases/latest/download/javinizer-windows-amd64.exe" -OutFile "javinizer.exe"
# 3. Move it to a permanent location:
Move-Item javinizer-windows-amd64.exe "$env:USERPROFILE\javinizer.exe"

# Verify
javinizer.exe --help
```

To run `javinizer` from any directory, add the folder containing `javinizer.exe` to your `PATH`:

1. Press `Win` + `R`, type `sysdm.cpl`, press Enter
2. Go to *Advanced* → *Environment Variables* → edit `Path` (under User variables)
3. Add the folder containing `javinizer.exe` (e.g. `%USERPROFILE%`), then restart your terminal

Alternatively, place `javinizer.exe` in a folder that's already on your `PATH`.

The Windows build includes the CLI/TUI/API plus the embedded web UI, with a statically-linked CGO/SQLite database — no separate runtime or dependencies required.

### Docker

Javinizer ships a pre-built multi-arch image on GitHub Container Registry, so you can run it without building anything. The supported path uses the bundled `docker-compose.yml` and `.env.example`:

```bash
# 1. Clone the repository (for docker-compose.yml and .env.example)
git clone https://github.com/javinizer/javinizer-go.git
cd javinizer-go

# 2. Configure environment
cp .env.example .env
# Edit .env: set MEDIA_PATH to your JAV library, and PUID/PGID to your host user

# 3. Start the container (pulls ghcr.io/javinizer/javinizer-go:latest)
docker compose up -d

# 4. Access the web UI
open http://localhost:8765
```

Essential `.env` values:

| Variable | Purpose |
|----------|---------|
| `MEDIA_PATH` | Absolute host path to your JAV library (mounted at `/media` in the container) |
| `PUID` / `PGID` | Match your host user (`id -u` / `id -g`) to avoid volume permission issues |
| `HOST_PORT` | Host port for the web UI/API (default `8765`) |
| `TZ` | Container timezone, e.g. `America/New_York` (default `UTC`) |

State (config, database, logs) persists in the `./data` volume; your media library is mounted read-write at `/media` for organize operations. To build the image locally instead of pulling, uncomment the `build:` block in `docker-compose.yml` (and comment out `image:`).

For the full guide — volume structure, FlareSolverr, Unraid `PUID`/`PGID` notes, setup-endpoint protection, and updates — see the [Docker Deployment Guide](./docker-deployment.md).

### Build from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/javinizer/javinizer-go.git
   cd javinizer-go
   ```

2. Build the binary:
   ```bash
   # CLI only (no embedded web UI)
   go build -o bin/javinizer ./cmd/javinizer

   # Full build (CLI + embedded web UI). Requires Node.js for the frontend.
   make build
   ```
   `make build` runs `make web-build` first to compile the SvelteKit frontend into `web/dist`, then embeds it into the binary. Run `make web-dev` during frontend development instead.

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

### 3. Complete First-Run Web Authentication

Start the API/Web server:

```bash
javinizer web
```

Then open [http://localhost:8765](http://localhost:8765) and create your default username/password.

Notes:
- Credentials are stored in `auth.credentials.json` next to your `config.yaml`.
- API and WebSocket endpoints require login after setup.
- To reset credentials later, stop server, delete `auth.credentials.json`, and restart.

## Your First Scrape

Let's test the scraper by fetching metadata for a movie:

```bash
javinizer scrape IPX-535
```

Expected output (only `r18dev` is enabled by default; enable more scrapers in `config.yaml` to aggregate additional sources):
```
--------------- ----------------------------------------------------------------------------------------------------
ID            : IPX-535
ContentID     : ipx00535
Title         : 3, 2, 1, GO! Sudden Follow-Up Piston-Pounding Sex "What!? Is This Uncut?" A Documentary-Style Serious
                Orgasm! She's About To Lose Her Mind!! Look At Her Anal Hole Twitch Momo Sakura
ReleaseDate   : 2020-09-13
Runtime       : 119 min
Director      : ZAMPA
Maker         : Idea Pocket
Label         : Dish
Series        : Instant Sex? You Mean, Here? Right Now?!
Actresses (1) :
              : [1] Sakura Momo (桜空もも) - ID: 1039157
              : Thumb: https://pics.dmm.co.jp/mono/actjpgs/sakura_momo4.jpg
Genres        : Older Sister, Big Tits, Quickie, Featured Actress, Blowjob, Digital Mosaic, Hi-Def, Exclusive
                Distribution, 4K
Translations  : English (r18dev), Japanese (r18dev)
Sources       : r18dev
--------------- ----------------------------------------------------------------------------------------------------

Source URLs:

  r18dev       : https://r18.dev/videos/vod/movies/detail/-/combined=ipx00535/json
--------------- ----------------------------------------------------------------------------------------------------

Media URLs:

  Cover URL    : https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg
  Trailer URL  : https://cc3001.dmm.co.jp/litevideo/freepv/i/ipx/ipx00535/ipx00535_mhb_w.mp4
  Screenshots  : 12 total
    [ 1] https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg
    ... (11 more)

--------------- ----------------------------------------------------------------------------------------------------
```

The metadata is now cached in your local database. Subsequent scrapes of the same ID will be instant!

### Understanding What Happened

1. **Multi-Source Scraping**: Javinizer queried R18.dev (the default-enabled scraper) for metadata. Enable additional scrapers like DMM, JavDB, or JavLibrary in `config.yaml` to aggregate data from multiple sources.
2. **Aggregation**: When multiple scrapers are enabled, data from all sources is combined based on your priority settings (see [Configuration](./02-configuration.md#metadata-priority)).
3. **Database Caching**: Results were saved to SQLite for fast future access.
4. **Genre Replacement**: Any configured genre replacements were applied.

## Your First Sort Operation

Now let's organize some video files. The shell commands differ by platform — follow the section for your OS. The `javinizer` invocations themselves are identical; only the directory-creation commands and path syntax change.

### Linux / macOS

Set up a test directory:

```bash
mkdir -p ~/javinizer-test
cd ~/javinizer-test
touch "IPX-535.mp4"
```

#### Dry Run (Preview Only)

Always start with a dry run to preview what will happen:

```bash
javinizer sort ~/javinizer-test --dry-run
```

Expected output:
```
=== Javinizer Sort ===
Source: /Users/you/javinizer-test
Destination: /Users/you/javinizer-test
Mode: DRY RUN
Operation: COPY
Generate NFO: true
Download Media: true

📂 Scanning for video files...

🌐 Processing files...
   ✅ Scraped IPX-535 successfully
   ✅ Applied IPX-535 successfully

   Would organize 1 file(s)

=== Summary ===
Files scanned: 1
IDs matched: 1
Metadata found: 1
NFOs generated: 1 (dry-run)
Files organized: 1 (dry-run)

💡 Run without --dry-run to apply changes
```

With the default template (`<ID> [<STUDIO>] - <TITLE> (<YEAR>)`), the file would be organized as:
```
/Users/you/javinizer-test/IPX-535 [Idea Pocket] - <title> (2020)/IPX-535.mp4
/Users/you/javinizer-test/IPX-535 [Idea Pocket] - <title> (2020)/IPX-535.nfo
```
alongside downloaded cover, poster, and screenshot images. Customize the folder/file format in `config.yaml` (see [Template System](./04-template-system.md)).

#### Apply Changes

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

#### Sort Options

```bash
# Recursive scanning is ON by default; disable it to scan only the top level
javinizer sort ~/Videos --recursive=false

# Move files instead of copying
javinizer sort ~/Videos --move

# Specify output destination
javinizer sort ~/Videos --dest ~/Organized

# Link files instead of copying (none | hard | soft)
javinizer sort ~/Videos --link-mode hard

# Skip NFO generation
javinizer sort ~/Videos --nfo=false

# Skip media downloads
javinizer sort ~/Videos --download=false

# Combine options
javinizer sort ~/Videos --move --dest ~/Organized --link-mode hard
```

### Windows (PowerShell)

Set up a test directory:

```powershell
mkdir $HOME\javinizer-test
cd $HOME\javinizer-test
New-Item "IPX-535.mp4" -ItemType File -Force
```

> Using Command Prompt (cmd.exe) instead of PowerShell? The equivalents are `mkdir %USERPROFILE%\javinizer-test`, `cd %USERPROFILE%\javinizer-test`, and `type nul > "IPX-535.mp4"`. In the `javinizer sort` commands below, substitute `%USERPROFILE%` for `$HOME`.

#### Dry Run (Preview Only)

Always start with a dry run to preview what will happen:

```powershell
javinizer sort $HOME\javinizer-test --dry-run
```

Expected output:
```
=== Javinizer Sort ===
Source: C:\Users\you\javinizer-test
Destination: C:\Users\you\javinizer-test
Mode: DRY RUN
Operation: COPY
Generate NFO: true
Download Media: true

📂 Scanning for video files...

🌐 Processing files...
   ✅ Scraped IPX-535 successfully
   ✅ Applied IPX-535 successfully

   Would organize 1 file(s)

=== Summary ===
Files scanned: 1
IDs matched: 1
Metadata found: 1
NFOs generated: 1 (dry-run)
Files organized: 1 (dry-run)

💡 Run without --dry-run to apply changes
```

With the default template (`<ID> [<STUDIO>] - <TITLE> (<YEAR>)`), the file would be organized as:
```
C:\Users\you\javinizer-test\IPX-535 [Idea Pocket] - <title> (2020)\IPX-535.mp4
C:\Users\you\javinizer-test\IPX-535 [Idea Pocket] - <title> (2020)\IPX-535.nfo
```
alongside downloaded cover, poster, and screenshot images. Customize the folder/file format in `config.yaml` (see [Template System](./04-template-system.md)).

#### Apply Changes

If the plan looks good, run it for real:

```powershell
javinizer sort $HOME\javinizer-test
```

This will:
1. ✅ Create organized folder structure
2. ✅ Move/copy video files with clean names
3. ✅ Generate Kodi-compatible NFO files
4. ✅ Download cover images and screenshots
5. ✅ Download actress thumbnails

#### Sort Options

```powershell
# Recursive scanning is ON by default; disable it to scan only the top level
javinizer sort $HOME\Videos --recursive=false

# Move files instead of copying
javinizer sort $HOME\Videos --move

# Specify output destination
javinizer sort $HOME\Videos --dest $HOME\Organized

# Link files instead of copying (none | hard | soft)
javinizer sort $HOME\Videos --link-mode hard

# Skip NFO generation
javinizer sort $HOME\Videos --nfo=false

# Skip media downloads
javinizer sort $HOME\Videos --download=false

# Combine options
javinizer sort $HOME\Videos --move --dest $HOME\Organized --link-mode hard
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

## Common Setup Issues

### Port 8765 Already in Use

**Problem**: Default API server port (8765) conflicts with another service.

**Solution**:
```bash
# Option 1: Override the port on the command line
javinizer web --port 3000

# Option 2: Change in config.yaml
server:
  port: 3000

# Option 3: For Docker users, set HOST_PORT in .env
HOST_PORT=3000
```

### Permission Denied When Running Binary

**Problem**: Binary lacks execute permission.

**Solution**:
```bash
chmod +x javinizer
```

### Permission Denied During File Operations

**Problem**: Insufficient permissions to read/write files.

**Solutions**:
- Check directory permissions: `ls -la /path/to/videos`
- Fix permissions: `chmod 755 /path/to/videos`
- Run with appropriate user privileges
- For Docker: Ensure `PUID`/`PGID` match your host user (check with `id -u` and `id -g`)

### Missing Configuration or Database

**Problem**: "Config file not found" or "Database not initialized" errors.

**Solution**:
```bash
# Initialize configuration and database
javinizer init

# Or restore from backup
cp backup/config.yaml configs/
cp backup/javinizer.db data/
```

### Docker Container Permission Issues

**Problem**: Container cannot access mounted volumes.

**Solution**:
```bash
# Get your host user IDs
id -u   # Shows your UID
id -g   # Shows your GID

# Set in .env file
PUID=1000  # Replace with your UID
PGID=1000  # Replace with your GID
```

On Unraid, common values are `PUID=99` and `PGID=100`.

### Scraper Fails with Cookie Error

**Problem**: Cloudflare-protected scrapers (e.g., JavLibrary) reject requests without valid session cookies.

**Solution**:

JavLibrary sits behind Cloudflare. The recommended fix is to run [FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) and point Javinizer at it:

```yaml
scrapers:
  flaresolverr:
    enabled: true
    url: "http://localhost:8191/v1"
  javlibrary:
    enabled: true
    language: ja          # en, ja, cn, tw
    use_flaresolverr: true
```

If you can't run FlareSolverr, capture Cloudflare cookies from a logged-in browser and supply them manually (the `user_agent` must match the browser the cookies came from):

```yaml
scrapers:
  javlibrary:
    enabled: true
    language: ja
    base_url: "http://www.javlibrary.com"
    cookies:
      cf_clearance: ""    # Paste your cf_clearance cookie
      cf_bm: ""           # Paste your cf_bm cookie
    user_agent: ""
```

> **MGStage** needs no manual cookie — its age-verification cookie (`adc=1`) is applied automatically. Just set `scrapers.mgstage.enabled: true`.

For detailed troubleshooting, see the [Troubleshooting Guide](./10-troubleshooting.md).

## Getting Help

- **Built-in Help**: `javinizer <command> --help`
- **Configuration Info**: `javinizer info`
- **Troubleshooting Guide**: [10-troubleshooting.md](./10-troubleshooting.md)
- **GitHub Issues**: [Report bugs or request features](https://github.com/javinizer/javinizer-go/issues)

---

**Next**: [Configuration Guide](./02-configuration.md)
