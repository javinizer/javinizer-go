# Javinizer Go

Javinizer Go is a metadata scraper and file organizer for Japanese Adult Videos (JAV), with CLI, TUI, API, and a web UI.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Test & Coverage](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml/badge.svg)](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/javinizer/javinizer-go/branch/main/graph/badge.svg)](https://codecov.io/gh/javinizer/javinizer-go)

## Features

| Feature | What it does | Why it helps |
|---|---|---|
| Multi-source scraping | Pulls metadata from R18.dev, DMM/Fanza, and optional sources. | Better match quality and fewer missing fields. |
| Smart file organization | Renames and organizes files/folders using templates. | Keeps large libraries consistent and searchable. |
| Dry-run safety | Shows a full preview before making any changes. | Reduces risk when processing many files. |
| NFO generation | Creates Kodi/Plex-compatible NFO metadata files. | Improves media center indexing and display quality. |
| Media downloads | Downloads cover, poster, fanart, trailer, and actress images. | Produces complete, polished library entries. |
| Multiple interfaces | Use CLI, interactive TUI, or API + web UI. | Lets you choose fast automation or manual review. |

## Supported Scrapers

| Scraper | Enabled by default (`config.yaml.example`) | Language options | Notes |
|---|---|---|---|
| `dmm` | Yes | N/A | Supports optional browser mode for JS-rendered pages. |
| `r18dev` | Yes | `en`, `ja` | JSON API scraper with rate-limit handling options. |
| `libredmm` | Yes | N/A | Community mirror source. |
| `mgstage` | No | N/A | Usually requires age-verification cookie (`adc=1`). |
| `javlibrary` | No | `en`, `ja`, `cn`, `tw` | Can use FlareSolverr for Cloudflare challenges. |
| `javdb` | No | N/A | Can use FlareSolverr; proxy-friendly setup. |
| `javbus` | No | `ja` | Japanese mode in example config. |
| `jav321` | No | N/A | Alternative index source. |
| `tokyohot` | No | `ja` | Tokyo-Hot specific source. |
| `aventertainment` | No | `en` | Supports bonus screenshot scraping option. |
| `dlgetchu` | No | N/A | DLsite/Getchu-related source. |
| `caribbeancom` | No | `ja` | Caribbeancom-specific source. |
| `fc2` | No | N/A | FC2 source. |

## Installation

### Docker (Recommended)

The easiest way to get started is with Docker:

```bash
# 1) Create data directory and download config
mkdir -p ./javinizer-data
curl -o ./javinizer-data/config.yaml \
  https://raw.githubusercontent.com/javinizer/javinizer-go/main/configs/config.yaml.example

# 2) Edit config.yaml with your settings (scrapers, output paths, etc.)

# 3) Run container
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/javinizer-data:/javinizer" \
  -v "/path/to/your/media:/media" \
  ghcr.io/javinizer/javinizer-go:latest
```

Open [http://localhost:8080](http://localhost:8080) to access the web UI.

**Notes:**
- Replace `/path/to/your/media` with the path to your JAV library
- Use a pinned tag (e.g., `v0.1.2-alpha`) for reproducible deployments
- `latest` tracks the most recent release

### Docker Compose

For a more complete setup with optional FlareSolverr support:

```bash
# 1) Download example files
curl -o docker-compose.yml \
  https://raw.githubusercontent.com/javinizer/javinizer-go/main/docker-compose.yml.example
curl -o .env \
  https://raw.githubusercontent.com/javinizer/javinizer-go/main/.env.example

# 2) Edit .env to configure paths and settings
# MEDIA_PATH=/path/to/your/jav-library
# TZ=America/New_York

# 3) Start services
docker-compose up -d
```

The `docker-compose.yml.example` includes:
- **javinizer**: Main API server + web UI
- **flaresolverr** (optional): Cloudflare challenge solver for JavDB/JavLibrary

See [Docker Deployment Guide](./docs/docker-deployment.md) for complete documentation.

### Prebuilt Binaries

Download pre-compiled binaries from [GitHub Releases](https://github.com/javinizer/javinizer-go/releases):

**Available platforms:**
- `linux-amd64` - Linux x86_64
- `linux-arm64` - Linux ARM64 (e.g., Raspberry Pi)
- `darwin-amd64` - macOS Intel
- `darwin-arm64` - macOS Apple Silicon
- `darwin-universal` - macOS Universal (Intel + Apple Silicon)
- `windows-amd64` - Windows x86_64

**Installation:**
```bash
# Example for Linux amd64
wget https://github.com/javinizer/javinizer-go/releases/download/v0.1.2-alpha/javinizer-linux-amd64.tar.gz
tar -xzf javinizer-linux-amd64.tar.gz
sudo mv javinizer /usr/local/bin/
javinizer version
```

**Note:** Prebuilt binaries include CLI, TUI, API server, and embedded web UI.

### Build from Source

Requires Go 1.25+ and CGO (for SQLite support). For embedded web UI builds, Node.js 20+ is also required.

```bash
go install github.com/javinizer/javinizer-go/cmd/javinizer@latest

# Or clone and build manually (single binary with embedded web UI)
git clone https://github.com/javinizer/javinizer-go.git
cd javinizer-go
make build
./bin/javinizer version
```

The `make build` target now builds the frontend bundle and embeds it into the Go binary.

## Usage

### Interactive TUI (Terminal UI)

Browse and scrape files interactively with real-time progress:

```bash
javinizer tui ~/Videos
```

See [TUI Guide](./docs/11-tui.md) for keyboard shortcuts and workflows.

### File Organization

Scan, scrape metadata, and organize files using templates:

```bash
# Dry-run mode (preview changes without modifying files)
javinizer sort ~/Videos --dry-run

# Actually organize files
javinizer sort ~/Videos
```

### Manual Scraping

Scrape metadata for a specific JAV ID:

```bash
javinizer scrape IPX-535
javinizer scrape SSIS-123 --force  # Force refresh cached metadata
```

### Update Existing Metadata

Re-scrape and update metadata for already organized files:

```bash
javinizer update ~/Videos/IPX-535
javinizer update ~/Videos --dry-run  # Preview updates for entire library
```

### Manage Tags

Add custom tags to movies (appears in NFO files):

```bash
javinizer tag add IPX-535 "favorite" "4K"
javinizer tag list IPX-535
javinizer tag remove IPX-535 "favorite"
```

### Genre Management

View and modify genre replacement rules:

```bash
javinizer genre list
javinizer genre add "Creampie" "Cream Pie"  # Replace "Creampie" with "Cream Pie"
javinizer genre remove "Creampie"
```

### History Tracking

View and rollback file organization operations:

```bash
javinizer history list
javinizer history rollback <operation-id>
```

### System Info

Display configuration, scrapers, and database status:

```bash
javinizer info
javinizer info --scrapers  # List enabled scrapers
javinizer info --config    # Show current configuration
```

### API + Web Server (`web` Alias)

Start the unified server (recommended via `web` alias):

```bash
javinizer web

# Equivalent legacy command
javinizer api

# Custom port
PORT=9000 javinizer web

# With flags
javinizer web --host 0.0.0.0 --port 8081
```

`javinizer api` and `javinizer web` invoke the same backend server command, but they represent different usage intents:
- Use `javinizer api` for backend/API-focused workflows and frontend development (`npm run dev` hot reload).
- Use `javinizer web` when you want the embedded browser UI entrypoint from the Go binary.

**What this server provides:**
- `GET /` - Embedded Web UI (single-binary distribution)
- `GET /api/v1/...` - REST API endpoints
- `GET /ws/progress` - WebSocket progress stream
- `GET /docs` - Scalar API docs UI
- `GET /swagger/index.html` - Swagger UI

## Web UI

The web application provides a modern interface for managing your JAV library.

**Availability:**
- ✅ **Docker**: Web UI is included and available at `localhost:8080`
- ✅ **CLI/Binary**: Web UI is embedded in the binary and served at `localhost:8080`

**How to access:**
```bash
# Using Docker (recommended)
docker run -p 8080:8080 -v ./data:/javinizer ghcr.io/javinizer/javinizer-go:latest
# Open http://localhost:8080
```

**Pages:**
- **Dashboard** - Quick stats and recent activity
- **Browse** - View organized movies with covers and metadata
- **Review** - Batch scrape files, crop posters, edit metadata before organizing
- **Actresses** - Browse actress database with images
- **History** - View and rollback organization operations
- **Settings** - Configure scrapers, output templates, and proxy settings

**API Documentation:**
- **Scalar UI**: [http://localhost:8080/docs](http://localhost:8080/docs) - Interactive API documentation
- **Swagger UI**: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) - OpenAPI spec viewer

See [API Reference](./docs/07-api-reference.md) for endpoint documentation.

### Web Development

To build and use the web UI with a local installation:

**Option 1: Production build (single binary with embedded UI)**
```bash
# Build binary (includes web bundle)
make build

# Start API/web server
javinizer web
# Open http://localhost:8080
```

**Option 2: Development mode (hot reload)**
```bash
# Terminal 1: Start backend API server
javinizer api

# Terminal 2: Start frontend dev server with hot reload
make web-dev
# Open http://localhost:5174 (proxies API calls to :8080)
```

The development server provides:
- Hot module replacement (instant updates on file changes)
- Better error messages
- Faster iteration

See `web/frontend/README.md` for more details.

## Environment Variables

Docker deployments support environment variable overrides:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `JAVINIZER_CONFIG` | Path to config file | `/javinizer/config.yaml` | `/custom/config.yaml` |
| `JAVINIZER_DB` | Path to SQLite database | `/javinizer/javinizer.db` | `/custom/db.db` |
| `JAVINIZER_LOG_DIR` | Log file directory | `/javinizer/logs` | `/custom/logs` |
| `LOG_LEVEL` | Logging verbosity | `info` | `debug`, `warn`, `error` |
| `UMASK` | File permission mask | `002` | `022` (owner-only write) |
| `TZ` | Timezone for logs | `UTC` | `America/New_York` |
| `PORT` | API server port | `8080` | `9000` |

**Example:**
```bash
docker run --rm \
  -e LOG_LEVEL=debug \
  -e TZ=Asia/Tokyo \
  -p 9000:8080 \
  -v "$(pwd)/data:/javinizer" \
  -v "/media/jav:/media" \
  ghcr.io/javinizer/javinizer-go:latest
```

See `.env.example` for Docker Compose configuration.

## Configuration

Javinizer uses a YAML configuration file to control scrapers, output templates, and behavior.

**Key configuration sections:**
- **Scrapers**: Enable/disable sources, set priorities, configure proxies
- **Metadata**: Field-level scraper priorities, translation, genre filtering
- **Output**: Folder/file naming templates, download options
- **File Matching**: Extensions, size filters, regex patterns
- **NFO**: Kodi/Plex metadata format options

**Documentation:**
- [Configuration Guide](./docs/02-configuration.md) - Detailed option reference
- [Example Config](./configs/config.yaml.example) - Fully commented configuration
- [Template System](./docs/06-template-system.md) - Output template syntax and functions
- [Genre Management](./docs/05-genre-management.md) - Genre replacement workflow

**Initialize config:**
```bash
javinizer init  # Creates default config.yaml in current directory
```

## Documentation

- [Getting Started](./docs/01-getting-started.md) - Installation and first steps
- [Docker Deployment](./docs/docker-deployment.md) - Container setup and management
- [Configuration](./docs/02-configuration.md) - Config file reference
- [CLI Reference](./docs/03-cli-reference.md) - Command-line interface guide
- [TUI Guide](./docs/11-tui.md) - Interactive terminal UI
- [API Reference](./docs/07-api-reference.md) - REST API endpoints
- [Template System](./docs/06-template-system.md) - Output naming templates
- [Genre Management](./docs/05-genre-management.md) - Genre replacement rules
- [Troubleshooting](./docs/10-troubleshooting.md) - Common issues and solutions

## Support

- **Issues**: [github.com/javinizer/javinizer-go/issues](https://github.com/javinizer/javinizer-go/issues)
- **Discussions**: [github.com/javinizer/javinizer-go/discussions](https://github.com/javinizer/javinizer-go/discussions)

## License

This project is a Go recreation of the original [Javinizer](https://github.com/jvlflame/Javinizer).

MIT License - see [LICENSE](LICENSE) file for details.
