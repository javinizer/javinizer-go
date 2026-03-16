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

## Quick Start (Docker, Recommended)

```bash
# 1) Prepare config/data directory
mkdir -p ./javinizer-data
cp ./configs/config.yaml.example ./javinizer-data/config.yaml

# 2) Run container
GHCR_TAG=latest

docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/javinizer-data:/javinizer" \
  -v "/path/to/media:/media" \
  ghcr.io/javinizer/javinizer-go:${GHCR_TAG}
```

Open [http://localhost:8080](http://localhost:8080).

Notes:
- Use a pinned tag (for example `v0.1.1-alpha`) for reproducible deployments.
- `latest` is intended for regular users who want current release behavior.

## CLI Binary (Secondary)

```bash
# Install from source
go install github.com/javinizer/javinizer-go/cmd/javinizer@latest

# Initialize config/data
javinizer init

# Check version
javinizer version --short
```

Prebuilt binaries are available on the [GitHub Releases](https://github.com/javinizer/javinizer-go/releases) page.

## Common Usage

```bash
# Interactive mode (recommended for manual review)
javinizer tui ~/Videos

# Scan and organize
javinizer sort ~/Videos --dry-run
javinizer sort ~/Videos

# Scrape one ID
javinizer scrape IPX-535

# Start API + web UI
javinizer api
```

## Configuration

- Docker path: `/javinizer/config.yaml`
- Main reference: [Configuration Guide](./docs/02-configuration.md)
- Full example config: [configs/config.yaml.example](./configs/config.yaml.example)

## Documentation

- [Getting Started](./docs/01-getting-started.md)
- [Docker Deployment](./docs/docker-deployment.md)
- [Configuration](./docs/02-configuration.md)
- [CLI Reference](./docs/03-cli-reference.md)
- [TUI Guide](./docs/11-tui.md)
- [API Reference](./docs/07-api-reference.md)
- [Troubleshooting](./docs/10-troubleshooting.md)

## Support

- Issues: [github.com/javinizer/javinizer-go/issues](https://github.com/javinizer/javinizer-go/issues)
- Discussions: [github.com/javinizer/javinizer-go/discussions](https://github.com/javinizer/javinizer-go/discussions)

## License

This project is a Go recreation of the original [Javinizer](https://github.com/jvlflame/Javinizer).
