# User Guide

This guide covers common workflows and explains the behavior of Javinizer's key features.

## Table of Contents

- [Operation Modes](#operation-modes)
- [Metadata & Artwork vs Update Metadata](#metadata--artwork-vs-update-metadata)
- [Scrape & Organize Workflow](#scrape--organize-workflow)
- [Update Metadata Workflow](#update-metadata-workflow)

## Operation Modes

The operation mode controls how files are handled during the organize step. Set it in `config.yaml` under `output.operation_mode`, or choose it from the web UI when starting a batch.

| Mode | Behavior |
|------|----------|
| `organize` | Moves files to destination with renamed folder/file names |
| `in-place` | Renames folder and file, but keeps them in the same directory |
| `in-place-norenamefolder` | Renames the file only, keeps the original folder name |
| `metadata-artwork` | Saves metadata to DB, writes NFO, downloads artwork — no file moves or renames |
| `preview` | Dry run: shows what would happen without making any changes |

## Metadata & Artwork vs Update Metadata

These two features produce nearly identical end results — both write NFO and download artwork to the source directory without moving files. The key differences:

| | Metadata & Artwork (`operation_mode`) | Update Metadata (`update=true`) |
|---|---|---|
| **NFO content** | Pure scraper data | Merged with existing NFO (configurable merge strategies) |
| **Merge options** | None | `preserve_nfo`, `force_overwrite`, `preset`, `scalar_strategy`, `array_strategy` |
| **Workflow** | Two-step: scrape then organize | Single-step: scrape and write in one go |
| **When to use** | First-time metadata fetch for a file | Re-scraping a file that already has an NFO you want to preserve fields from |

Both modes save the same data to the database, download the same set of files (poster, cover, extrafanart, trailer, actress images), and neither moves or renames the source video file.

### What each mode writes to disk

Both modes write the following to the source file's directory:

- **NFO file** — metadata in XML format for media managers
- **Poster image** — `<ID>-poster.jpg`
- **Cover/fanart** — `<ID>-fanart.jpg`
- **Extrafanart** — screenshot images in `extrafanart/` subfolder (if enabled)
- **Trailer** — `<ID>-trailer.mp4` (if enabled)
- **Actress images** — thumbnails in `.actors/` subfolder (if enabled)

Neither mode moves, renames, or creates folders for the source video file.

## Scrape & Organize Workflow

The standard two-phase workflow used by the web UI's "Scrape & Organize" button:

1. **Scrape** — Fetch metadata from configured scrapers, save to database, and present results for review
2. **Organize** — Apply the selected operation mode: move/rename files, write NFO, and download artwork

The operation mode determines what happens during step 2. With `metadata-artwork` mode, step 2 writes NFO and downloads artwork but skips all file operations.

## Update Metadata Workflow

The single-phase workflow used by the web UI's "Update Metadata" button:

1. **Scrape + merge + write** — Fetch metadata, merge with the existing NFO file on disk, save to database, write updated NFO, and download artwork

This is designed for files that already have an NFO you want to update rather than replace. The merge strategies let you control how existing field values are preserved or overwritten.
