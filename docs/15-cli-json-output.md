# CLI JSON Output Mode

The `javinizer scrape` command supports machine-readable JSON output via the `--output json` flag. This is designed for automated tooling (e.g., [javinizer-go-tracker](https://github.com/javinizer/javinizer-go-tracker)) that needs raw per-scraper results without aggregation, caching, or database persistence.

## Usage

```bash
javinizer scrape ABF-153 --scrapers r18dev --output json
```

**Requirements:**
- `--scrapers` must specify exactly one scraper
- `--force` is not allowed (it would trigger a database write)
- `--config` must point to a valid config file with the named scraper enabled

## Output Schema

### Success (exit 0)

Stdout contains a single JSON object matching `models.ScraperResult`:

```json
{
  "source": "r18dev",
  "source_url": "https://...",
  "id": "ABF-153",
  "title": "...",
  "actresses": [...],
  "genres": [...],
  "poster_url": "...",
  "screenshot_urls": [...]
}
```

### Error (exit 1)

Stdout contains an error envelope:

```json
{
  "error": {
    "kind": "not_found",
    "message": "movie not found",
    "status_code": 0,
    "retryable": false,
    "temporary": false
  }
}
```

**Error kinds:**
- `not_found` — scraper returned no results (status_code: 0)
- `blocked` — HTTP 403/451, Cloudflare challenge, or access denied
- `rate_limited` — HTTP 429
- `unavailable` — HTTP 5xx, timeout, or context cancellation (retryable, temporary)
- `unknown` — panic, config error, unknown scraper, or other unexpected failure

All diagnostic logs go to stderr; stdout contains only the JSON document.
