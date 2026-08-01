# User Guide

This guide covers common workflows and explains the behavior of Javinizer's key features.

## Table of Contents

- [Operation Modes](#operation-modes)
- [Metadata & Artwork vs Update Metadata](#metadata--artwork-vs-update-metadata)
- [Scrape & Organize Workflow](#scrape--organize-workflow)
- [Update Metadata Workflow](#update-metadata-workflow)
- [Web UI Flows](#web-ui-flows)

## Operation Modes

The operation mode controls how files are handled during the organize step. Set it in `config.yaml` under `output.operation_mode`, or choose it from the web UI when starting a batch. When `output.operation_mode` is unset, the mode defaults to `organize`.

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

### Merge strategies (Update Metadata only)

Update Metadata merges scraped data into the existing NFO on disk. The merge options — also exposed as flags on `javinizer update` and as JSON fields on the batch API — are:

| Option | Values | Effect |
|--------|--------|--------|
| `scalar_strategy` | `prefer-nfo` (default), `prefer-scraper`, `preserve-existing`, `fill-missing-only` | How single-value fields (title, maker, release date, …) are merged |
| `array_strategy` | `merge` (default), `replace` | `merge` combines and deduplicates arrays (genres, actresses, …); `replace` uses the scraper's array |
| `preset` | `conservative`, `gap-fill`, `aggressive` | Convenience preset that overrides `scalar_strategy`/`array_strategy`. `conservative` = preserve-existing + merge; `gap-fill` = fill-missing-only + merge; `aggressive` = prefer-scraper + replace |
| `preserve_nfo` | `true` / `false` | Never overwrite existing NFO fields, only add missing data (most conservative) |
| `force_overwrite` | `true` / `false` | Ignore the existing NFO and use only scraper data (destructive) |
| `overwrite_existing_media` | `true` / `false` | Re-download and atomically replace existing enabled media using freshly scraped URLs; default `false` |

### What each mode writes to disk

Both modes write the following to the source file's directory — the NFO file is gated by `metadata.nfo.enabled` (default `true`), and each media artifact is gated by its `output.download_*` option (all default `true`):

- **NFO file** — `<ID>.nfo`, metadata in XML format for media managers
- **Poster image** — `<ID>-poster.jpg` (vertical poster)
- **Cover/fanart** — `<ID>-fanart.jpg` (horizontal cover/background)
- **Extrafanart** — screenshot images in the `extrafanart/` subfolder, named `fanart<INDEX>.jpg` (`fanart1.jpg`, `fanart2.jpg`, …)
- **Trailer** — `<ID>-trailer.mp4`
- **Actress images** — thumbnails in the `.actors/` subfolder, named `<ACTORNAME>.jpg`

For multi-part movies a `-pt<N>` part suffix is inserted before the artifact suffix, e.g. `IPX-535-pt1-poster.jpg`. Neither mode moves, renames, or creates folders for the source video file.

For example, scraping `IPX-535.mp4` (Idea Pocket, 2020, starring Sakura Momo / 桜空もも) writes alongside the untouched source video:

```
IPX-535.mp4                <- source video (not moved or renamed)
IPX-535.nfo
IPX-535-poster.jpg
IPX-535-fanart.jpg
extrafanart/
  fanart1.jpg
  fanart2.jpg
  ...
IPX-535-trailer.mp4
.actors/
  Sakura Momo.jpg
```

## Scrape & Organize Workflow

The standard two-phase workflow used by the web UI's "Scrape & Organize" button:

1. **Scrape** — Fetch metadata from configured scrapers, save to database, and present results for review
2. **Organize** — Apply the selected operation mode: move/rename files, write NFO, and download artwork

The operation mode determines what happens during step 2. With `metadata-artwork` mode, step 2 writes NFO and downloads artwork but skips all file operations.

## Update Metadata Workflow

The single-phase workflow used by the web UI's "Update Metadata" button:

1. **Scrape + merge + write** — Fetch metadata, merge with the existing NFO file on disk, save to database, write updated NFO, and download artwork

By default, existing media files are skipped. To refresh artwork during an update, use `--overwrite-existing-media` with the CLI or send `"overwrite_existing_media": true` in the update batch API request. This re-downloads all enabled media types (covers, posters, screenshots, trailers, and actress images) from the freshly scraped URLs. Replacement is staged and performed atomically; if the download or replacement fails, the existing artwork is preserved.

This option defaults to `false` and applies to Update Metadata only. It is distinct from `force_overwrite`, which controls NFO merging, and `force-refresh`, which controls the scraper cache. Media downloads use freshly scraped URLs regardless of the NFO merge strategy.

This is designed for files that already have an NFO you want to update rather than replace. The merge strategies let you control how existing field values are preserved or overwritten.

## Web UI Flows

The web UI (`javinizer web`) drives the batch workflows through a few routes:

### Browse (`/browse`)

The primary scraping workspace (the **Scrape** item in the navigation; the post-login landing page is the dashboard at `/`). Select files, then build one explicit action plan. The operation never changes implicitly because a destination happens to match a source directory.

| Video operation | Destination | NFO | Media | Existing metadata |
|---|---|---|---|---|
| **Organize into another location** | Required | Write or skip | Missing or skip | Scraper result |
| **Rename in place** | Not used | Write or skip | Missing or skip | Scraper result |
| **Rename video file only** | Not used | Write or skip | Missing or skip | Scraper result |
| **Leave video files in place** | Not used | Write or skip | Missing, replace, or skip | Preset or custom scalar/array merge |

The “This operation will…” summary and sticky compact summary are generated from that same plan. A leave-in-place plan cannot skip both NFO and media because that would have no output; explicit rename operations may skip both sidecar outputs because renaming is still an effect.

- **Provide IDs or URLs manually** carries the complete read-only plan to `/manual`; only per-file IDs/URLs, force refresh, and scraper selection remain editable there.
- **Force refresh** controls scraper cache usage and does not change output policy.
- **Manual scraper selection** controls scrape sources and does not change the apply plan.

### Manual Scrape (`/manual`)

Reached from `/browse` when "Manual Scrape" is enabled. Lets you enter, for each selected file, a JAV ID or a URL that overrides the filename-based matcher; a badge marks each entry as ID, URL, or Auto (matcher-derived). The page persists manual inputs across the session (via `sessionStorage`), shows the enabled scrapers that will be used, and on submit sends a `POST /api/v1/batch/scrape` with a `manual_inputs` map keyed by file path (see [API Reference](./07-api-reference.md#batch-operations)) and navigates to `/review`.

### Review (`/review/[jobId]`)

The post-scrape review screen for a batch job. Tabs:

- **Movies** — edit metadata per result (title, actresses, genres, poster crop, poster-from-URL), exclude individual movies, preview the organize path, and re-scrape a single movie (with merge strategies).
- **Failed** — files that could not be matched or scraped, with re-scrape options.

The operation selected on Browse is read-only on Review. Destination, NFO, media, and supported merge policies can be adjusted before applying. Preview and apply resolve the same persisted plan and Review overrides, so the displayed paths and output policies are the ones executed. The action bar runs the organize or update endpoint for the plan family, with real-time progress streamed over `/ws/progress`.

Replacing existing media is destructive and is intentionally available only for **Leave video files in place**. Review shows it as a safety-gated option. Replacement is atomic, but media overwrites are not covered by normal organize rollback and should be treated as non-revertible.

### Typical paths

```
/browse  ──choose operation──▶  /review/[jobId]  ──preview/apply──▶  done
/browse  ──provide IDs or URLs──▶  /manual  ──submit same plan──▶  /review/[jobId]  ──apply──▶  done
```
