# Javinizer Go

A metadata scraper and file organizer for Japanese Adult Videos (JAV), with CLI, TUI, REST API, and a web UI. A Go recreation of the original [Javinizer](https://github.com/jvlflame/Javinizer).

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Test & Coverage](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml/badge.svg)](https://github.com/javinizer/javinizer-go/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/javinizer/javinizer-go/branch/main/graph/badge.svg)](https://codecov.io/gh/javinizer/javinizer-go)
[![Discord](https://img.shields.io/discord/608449512352120834?color=brightgreen&style=plastic&label=discord)](https://discord.gg/Pds7xCpzpc)
[![latest release](https://img.shields.io/github/v/release/javinizer/javinizer-go?label=latest%20release)](https://github.com/javinizer/javinizer-go/releases)

---

## Quick Start

The fastest way to try Javinizer is Docker — one command gives you the web UI:

```bash
mkdir -p ./data
curl -o ./data/config.yaml \
  https://raw.githubusercontent.com/javinizer/javinizer-go/main/configs/config.yaml.example

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -p 8080:8080 \
  -v "$(pwd)/data:/javinizer" \
  -v "/path/to/your/media:/media" \
  ghcr.io/javinizer/javinizer-go:latest
```

Open **http://localhost:8080**, create your admin login on first startup, and start scraping.

- Replace `/path/to/your/media` with your JAV library path.
- On Unraid, use `--user 99:100`.
- Prefer [Homebrew](#homebrew-macos--linux), a [one-shot installer](#one-shot-install-linux--macos--windows), a [binary](#prebuilt-binaries-manual-download), or [build from source](#build-from-source) for a native install.

> **First time?** Skim [Features](#features) to see what it does, then jump to [Usage](#usage) or the [Web UI](#web-ui) section.

---

## Features

| Feature | What it does | Why it helps |
|---|---|---|
| Multi-source scraping | Pulls metadata from R18.dev, DMM/Fanza, and 12+ more sources. | Better match quality and fewer missing fields. |
| Smart file organization | Renames and organizes files/folders using templates. | Keeps large libraries consistent and searchable. |
| Dry-run safety | Shows a full preview before making any changes. | Reduces risk when processing many files. |
| NFO generation | Creates Kodi/Plex-compatible NFO metadata files. | Improves media center indexing and display quality. |
| Media downloads | Downloads cover, poster, fanart, trailer, and actress images. | Produces complete, polished library entries. |
| Manual scrape | Per-file ID/URL overrides before a batch runs. | Handle files whose filenames have no usable JAV ID. |
| Multiple interfaces | Use CLI, interactive TUI, REST API, or web UI. | Fast automation or manual review — your choice. |

## Supported Scrapers

| Scraper | Enabled by default | Languages | Notes |
|---|---|---|---|
| `r18dev` | Yes | `en`, `ja` | JSON API scraper with rate-limit handling. |
| `dmm` | No | N/A | Optional browser mode for JS-rendered pages. |
| `libredmm` | No | N/A | Aggregates Fanza, MGStage, SOD, and FC2. |
| `mgstage` | No | N/A | Usually requires age-verification cookie (`adc=1`). |
| `javlibrary` | No | `en`, `ja`, `cn`, `tw` | Can use FlareSolverr for Cloudflare challenges. |
| `javdb` | No | N/A | Can use FlareSolverr; proxy-friendly. |
| `javbus` | No | `ja`, `en`, `zh` | Multi-language support. |
| `jav321` | No | N/A | Alternative index source. |
| `tokyohot` | No | `ja`, `en`, `zh` | Tokyo-Hot specific source. |
| `aventertainment` | No | `en`, `ja` | Bonus screenshot scraping option. |
| `dlgetchu` | No | N/A | DLsite/Getchu-related source. |
| `caribbeancom` | No | `ja`, `en` | Caribbeancom-specific source. |
| `fc2` | No | N/A | FC2 source. |
| `javstash` | No | `en`, `ja` | GraphQL API scraper; requires API key from javstash.org. |

---

## Installation

### Docker (Recommended)

See [Quick Start](#quick-start) above. For a complete setup with optional FlareSolverr support, use Docker Compose:

```bash
curl -o .env https://raw.githubusercontent.com/javinizer/javinizer-go/main/.env.example
curl -o docker-compose.yml https://raw.githubusercontent.com/javinizer/javinizer-go/main/docker-compose.yml
# Edit .env: MEDIA_PATH=/path/to/your/library, PUID, PGID, TZ
docker-compose up -d
```

The compose file includes **javinizer** (API + web UI) and an optional **flaresolverr** (Cloudflare solver for JavDB/JavLibrary). See the [Docker Deployment Guide](./docs/docker-deployment.md) for details.

**Tag policy:** `latest` tracks the most recent release; pin a tag (e.g. `v1.0.0`) for reproducible deployments.

### Homebrew (macOS / Linux)

Install via the Homebrew tap (recommended for macOS):

```bash
brew tap javinizer/homebrew-tap https://github.com/javinizer/homebrew-tap
brew trust --formula javinizer/tap/javinizer   # required once on Homebrew 6.0+
brew install javinizer

brew upgrade javinizer   # update to the latest stable release later
```

Homebrew 6.0+ requires explicitly trusting third-party taps before installing from them. The `brew trust` step is a one-time setup per tap; alternatively set `HOMEBREW_NO_REQUIRE_TAP_TRUST=1` to skip the check. The formula installs a prebuilt binary (CGO/SQLite is statically linked into each release asset, so Homebrew does not build from source or pull a SQLite dependency). The tap is updated automatically on each **stable** release; prereleases never reach it, so `brew upgrade` never hands you a release candidate.

### Scoop (Windows)

Install via the Scoop bucket (recommended for Windows):

```powershell
scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer
scoop install javinizer

scoop update javinizer   # update to the latest stable release later
```

The manifest installs the prebuilt `javinizer-windows-amd64.exe` and shims it as `javinizer`. The bucket is updated automatically on each **stable** release; prereleases never reach it, so `scoop update` never hands you a release candidate. Scoop downloads via a trusted process and verifies the hash from the manifest, making this the recommended Windows install path.

### Desktop app (clickable GUI)

The desktop app is a single clickable application that opens a native window over the embedded API server and Web UI — the same surface as `javinizer api`, no browser needed. It is a **separate package** from the CLI so both can coexist:

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

The app is **unsigned** — see [Desktop App (macOS / Windows / Linux)](docs/17-desktop-app.md) for first-launch Gatekeeper / Smart App Control notes. The cask and bucket are updated automatically on each **stable** release; prereleases never reach them.

### One-shot install (Linux / macOS / Windows)

The installers download the latest **stable** release, verify its SHA256 against `checksums.txt`, and put `javinizer` on your `PATH`. Prereleases are opt-in: pass `--pre-release` (Linux/macOS) or `-PreRelease` (Windows) to install the newest release including prereleases.

**Linux / macOS:**

```bash
curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash
# install the latest pre-release instead:
curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash -s -- --pre-release
```

**Windows** (PowerShell) — installs to `%LOCALAPPDATA%\javinizer\bin` (no admin required):

```powershell
irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1 | iex
# install the latest pre-release instead:
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1))) -PreRelease
```

The Windows installer also runs `Unblock-File` on the downloaded binary, stripping the Mark-of-the-Web tag that can otherwise trigger an "Access is denied" error under Smart App Control.

### Prebuilt Binaries (manual download)

Download from [GitHub Releases](https://github.com/javinizer/javinizer-go/releases) — available for `linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`, `darwin-universal`, and `windows-amd64`. Binaries include the CLI, TUI, API server, and embedded web UI.

**Linux / macOS:**

```bash
# 1. Download the asset matching your OS/arch from the Releases page:
#    https://github.com/javinizer/javinizer-go/releases  (e.g. javinizer-linux-amd64)
# 2. Make it executable and put it on your PATH:
chmod +x javinizer
sudo mv javinizer /usr/local/bin/

javinizer version
```

> **One-shot download:** `releases/latest` resolves to the newest stable release, so you can fetch the latest binary directly — no version in the URL:
> ```bash
> curl -L -o javinizer https://github.com/javinizer/javinizer-go/releases/latest/download/javinizer-linux-amd64
> ```
> Prereleases can't be the “Latest” release on GitHub, so this permalink always points at a stable release.

**Windows:**

Download `javinizer-windows-amd64.exe` from the [Releases page](https://github.com/javinizer/javinizer-go/releases), then run in PowerShell:

```powershell
# Optional: rename for ease of use
Rename-Item javinizer-windows-amd64.exe javinizer.exe

# Run from the same folder
.\javinizer.exe version
```

> **Windows 11 + Smart App Control:** Windows release binaries are not yet Authenticode-signed. If Smart App Control is in enforcement mode it may block the unsigned binary with an "Access is denied" error. The one-shot `install.ps1` above runs `Unblock-File` automatically; for a manual download, unblock it by right-clicking the `.exe` → Properties → check **Unblock** → OK (equivalently `Unblock-File .\javinizer.exe`), or build from source (below — locally-built binaries carry no Mark-of-the-Web and are not gated by SAC).

To run `javinizer` from anywhere, add its folder to your `PATH` (System Properties → Environment Variables → Path → New), or copy `javinizer.exe` into a folder that's already on your `PATH`.

```powershell
# Start the web UI, then open http://localhost:8080
.\javinizer.exe init
.\javinizer.exe web
```

> Windows builds are CLI/TUI/API + embedded web UI, same as the other platforms. CGO/SQLite is statically linked, so no separate runtime is required.

### Self-upgrade

Once installed from a binary or `install.sh`, update in place without re-downloading by hand:

```bash
javinizer upgrade           # download + verify + replace the running binary
javinizer upgrade --check   # just report whether an update is available
javinizer upgrade --force   # reinstall even if already at the latest version
javinizer upgrade --prerelease  # upgrade to the newest release, including prereleases
```

The new binary is verified against the release `checksums.txt` before the swap. If javinizer was installed via **Homebrew** or **Scoop**, `upgrade` detects that and tells you to use `brew upgrade javinizer` / `scoop update javinizer` instead, so it never clobbers a package-manager install.

By default `upgrade` targets the latest **stable** release. Add `--prerelease` to jump to a newer release candidate (e.g. `v1.1.0-rc1`) when you want to track prereleases.

> Note: `javinizer upgrade` updates the **program**; `javinizer update` refreshes **metadata** for your existing files. They are different commands.

### Build from Source

Requires Go 1.26+ and CGO (for SQLite). For the embedded web UI, Node.js 20+ is also required (Node 22 used in CI).

```bash
go install github.com/javinizer/javinizer-go/cmd/javinizer@latest

# Or clone and build a single binary with the embedded web UI:
git clone https://github.com/javinizer/javinizer-go.git
cd javinizer-go
make build
./bin/javinizer version
```

`make build` compiles the frontend bundle and embeds it into the Go binary. For CLI-only builds without the frontend: `go build -o bin/javinizer ./cmd/javinizer`.

---

## Usage

### Start the web UI

```bash
javinizer init          # creates a default config.yaml + database
javinizer web           # starts the server at http://localhost:8080
# Custom port/host:
javinizer web --host 0.0.0.0 --port 8081
```

`web` is an alias of `api` — same server. Use `web` for the embedded UI entrypoint, `api` for backend/frontend-dev workflows. On first startup, the web UI prompts you to create an admin login (stored in `auth.credentials.json` next to your config). Delete that file to reset the password.

### Organize a folder

```bash
javinizer sort ~/Videos --dry-run   # preview renames/moves first
javinizer sort ~/Videos             # scrape + organize for real
```

### Scrape a single ID

```bash
javinizer scrape IPX-535
javinizer scrape SSIS-123 --force   # force-refresh cached metadata
```

### Update metadata in place

Re-scrape and merge into already-organized files (supports merge presets/strategies):

```bash
javinizer update ~/Videos/IPX-535
javinizer update ~/Videos --dry-run
```

### Interactive TUI

```bash
javinizer tui ~/Videos
```

See the [TUI Guide](./docs/11-tui.md) for keyboard shortcuts and workflows.

### Manage your library

```bash
# Tags (written to NFO files)
javinizer tag add IPX-535 "favorite" "4K"
javinizer tag search "favorite"

# Genre / word replacement rules
javinizer genre add "Creampie" "Cream Pie"
javinizer word add "censored" "original"

# Actress database
javinizer actress merge --target <id> --source <id>   # merge duplicates
javinizer actress export

# History & logs
javinizer history list
javinizer logs list

# API tokens (for programmatic access)
javinizer token create
javinizer token list --json

# Config & version
javinizer config migrate      # upgrade an older config to the current schema
javinizer info                # show config, scrapers, and DB status
javinizer version --check     # show version + check for updates
```

See the [CLI Reference](./docs/03-cli-reference.md) for every command and flag.

---

## Web UI

Available in Docker and in the binary (embedded), at `http://localhost:8080`.

| Page | What you do there |
|---|---|
| **Dashboard** | Quick stats and recent activity. |
| **Browse** | View organized movies with covers and metadata; send files to a manual scrape. |
| **Manual** | Per-file JAV ID/URL overrides before a batch runs (for files with no usable filename ID). |
| **Review** | Batch-scrape files, crop posters, and edit metadata before organizing. |
| **Jobs** | Monitor active batch jobs with real-time WebSocket progress. |
| **Actresses** | Browse the actress database with images. |
| **History** | View and roll back organization operations. |
| **Settings** | Configure scrapers, output templates, and proxy settings. |

**API docs** are served alongside the UI: [Scalar UI](http://localhost:8080/docs) and [Swagger UI](http://localhost:8080/swagger/index.html). See the [API Reference](./docs/07-api-reference.md) for endpoint documentation.

### Web development

**Production build (single binary with embedded UI):**
```bash
make build && javinizer web
```

**Dev mode (hot reload):**
```bash
javinizer api        # terminal 1: backend
make web-dev         # terminal 2: frontend at http://localhost:5174 (proxies API to :8080)
```

See `web/frontend/README.md` for more.

---

## Configuration

Javinizer uses a YAML config file. Initialize one with `javinizer init`, then edit it.

**Key sections:**
- **Scrapers** — enable/disable sources, set priorities, configure proxies.
- **Metadata** — per-field scraper priorities, translation, genre filtering, word replacement.
- **Output** — folder/file naming templates, download options.
- **File Matching** — extensions, size filters, regex patterns.
- **NFO** — Kodi/Plex metadata format options.

**Per-field priority semantics:** a per-field scraper list is **exclusive** (no global fallback). An absent key or empty list `[]` inherits the global priority; `["__skip__"]` leaves that field empty. See the [Configuration Guide](./docs/02-configuration.md) for the full schema and the [example config](./configs/config.yaml.example).

### Multi-language template tags

Template tags can select a language for translated fields:

```yaml
output:
  folder_format: <ID> [<MAKER:JA>] - <TITLE:EN> (<YEAR>)
# → ROYD-191 [ROYD] - A Beautiful Day (2024)
```

- `<TITLE:EN>` — English title; `<TITLE:JA|EN>` — Japanese with English fallback.
- Supported tags: `TITLE`, `MAKER`, `LABEL`, `SERIES`, `DIRECTOR`, `DESCRIPTION`, `ORIGINALTITLE`, `STUDIO` (synonym for `MAKER`).
- Language codes are lowercase 2-letter (`en`, `ja`, `zh`, …); regional variants are normalized to the base language.

See the [Template System](./docs/04-template-system.md) for full syntax and functions.

---

## Environment Variables

Docker deployments support environment-variable overrides.

### Core

| Variable | Description | Default |
|----------|-------------|---------|
| `PUID` / `PGID` | Runtime user/group ID for the container process | `1000` |
| `USER_ID` / `GROUP_ID` | Legacy aliases for `PUID`/`PGID` | `1000` |
| `JAVINIZER_CONFIG` | Path to config file | `/javinizer/config.yaml` |
| `JAVINIZER_DB` | Path to SQLite database | `/javinizer/javinizer.db` |
| `JAVINIZER_LOG_DIR` | Relocate file log targets to this directory | `/javinizer/logs` |
| `JAVINIZER_TEMP_DIR` | Temp directory for downloads | `data/temp` |
| `LOG_LEVEL` | Logging verbosity | `info` |
| `UMASK` | File permission mask | `002` |
| `TZ` | Timezone for logs | `UTC` |

### Translation & Scraper API Keys

| Variable | Purpose |
|----------|---------|
| `TRANSLATION_PROVIDER` | `openai`, `deepl`, `google`, or `anthropic` |
| `TRANSLATION_SOURCE_LANGUAGE` / `TRANSLATION_TARGET_LANGUAGE` | e.g. `ja` → `en` |
| `OPENAI_API_KEY` / `DEEPL_API_KEY` / `GOOGLE_TRANSLATE_API_KEY` / `ANTHROPIC_API_KEY` | Provider keys |
| `JAVSTASH_API_KEY` | JavStash GraphQL API key (get from javstash.org) |

### Development

| Variable | Purpose |
|----------|---------|
| `CHROME_BIN` | Chrome binary path for browser scraping |
| `GH_TOKEN` | GitHub token (avoids rate limits for update checks) |

```bash
docker run --rm \
  -e LOG_LEVEL=debug -e TZ=Asia/Tokyo \
  -p 9000:8080 \
  -v "$(pwd)/data:/javinizer" -v "/media/jav:/media" \
  ghcr.io/javinizer/javinizer-go:latest
```

See `.env.example` for Docker Compose configuration.

---

## Documentation

| Guide | Covers |
|---|---|
| [Getting Started](./docs/01-getting-started.md) | Installation and first steps |
| [Docker Deployment](./docs/docker-deployment.md) | Container setup and management |
| [Configuration](./docs/02-configuration.md) | Config file reference |
| [CLI Reference](./docs/03-cli-reference.md) | Every command and flag |
| [TUI Guide](./docs/11-tui.md) | Interactive terminal UI |
| [API Reference](./docs/07-api-reference.md) | REST API endpoints |
| [Template System](./docs/04-template-system.md) | Output naming templates |
| [Genre Management](./docs/05-genre-management.md) | Genre replacement rules |
| [User Guide](./docs/14-user-guide.md) | Web UI workflows |
| [Architecture](./docs/16-architecture.md) | System architecture overview |
| [Development](./docs/09-development.md) | Contributing and dev setup |
| [Testing](./docs/12-testing-guide.md) | Testing practices and coverage |
| [Troubleshooting](./docs/10-troubleshooting.md) | Common issues and solutions |

## Support

- **Issues**: [github.com/javinizer/javinizer-go/issues](https://github.com/javinizer/javinizer-go/issues)
- **Discussions**: [github.com/javinizer/javinizer-go/discussions](https://github.com/javinizer/javinizer-go/discussions)
- **Discord**: [invite link](https://discord.gg/Pds7xCpzpc)

## License

MIT License — see [LICENSE](LICENSE). This project is a Go recreation of the original [Javinizer](https://github.com/jvlflame/Javinizer).
