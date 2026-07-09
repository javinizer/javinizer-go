# API Reference

The Javinizer REST API provides programmatic access to all metadata scraping, file organization, and database operations. The API powers the web UI and is fully documented with interactive examples.

## Overview

- **Base URL**: `http://localhost:8765/api/v1`
- **Content Type**: `application/json`
- **Authentication**: Built-in single-user session authentication
- **WebSocket**: Real-time progress updates at `/ws/progress`

## Getting Started

### Start the API Server

**Using Docker (recommended):**
```bash
docker run --rm -p 8765:8765 \
  -v "$(pwd)/data:/javinizer" \
  -v "/path/to/media:/media" \
  ghcr.io/javinizer/javinizer-go:latest
```

**Using CLI:**
```bash
javinizer web
```

**Custom port:**
```bash
javinizer web --port 9000
```
Or set `server.port` in `config.yaml` (default `8765`).

### Interactive API Documentation

The API server provides two interactive documentation interfaces:

- **Scalar UI**: [http://localhost:8765/docs](http://localhost:8765/docs) - Modern, user-friendly API explorer
- **Swagger UI**: [http://localhost:8765/swagger/index.html](http://localhost:8765/swagger/index.html) - Traditional OpenAPI spec viewer

These interfaces provide:
- Complete request/response schemas
- Try-it-now functionality
- Code generation for multiple languages
- Authentication testing

### First-Run Authentication Setup

On first startup, protected API routes return `503` until credentials are configured.

1. Start server: `javinizer web`
2. Open Web UI at `http://localhost:8765/`
3. Create default username/password in the setup screen
4. Session cookie is issued automatically after setup

**CLI setup example (cookie jar):**
```bash
curl -X POST http://localhost:8765/api/v1/auth/setup \
  -H "Content-Type: application/json" \
  -c cookies.txt \
  -d '{"username":"admin","password":"password123"}'
```

**Reset credentials:**
1. Stop server
2. Delete `auth.credentials.json` (next to `config.yaml`)
3. Restart server and run setup again

## API Endpoints

### Movies

Scrape, retrieve, and manage movie metadata.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/scrape` | Scrape metadata for a JAV ID |
| `GET` | `/api/v1/movies` | List all movies in database |
| `GET` | `/api/v1/movies/:id` | Get movie metadata by ID |
| `POST` | `/api/v1/movies/:id/rescrape` | Force re-scrape movie metadata |
| `POST` | `/api/v1/movies/:id/compare-nfo` | Compare database metadata with NFO file |

**Example - Scrape movie:**
```bash
curl -X POST http://localhost:8765/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"id": "IPX-535"}'
```

**Example - List movies:**
```bash
curl http://localhost:8765/api/v1/movies
```

### Actresses

Manage actress database with images and metadata.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/actresses` | List all actresses in database |
| `GET` | `/api/v1/actresses/:id` | Get actress by ID |
| `GET` | `/api/v1/actresses/search` | Search actresses by name |
| `POST` | `/api/v1/actresses` | Create new actress entry |
| `PUT` | `/api/v1/actresses/:id` | Update actress metadata |
| `DELETE` | `/api/v1/actresses/:id` | Delete actress from database |
| `POST` | `/api/v1/actresses/merge/preview` | Preview a target/source merge and field conflicts |
| `POST` | `/api/v1/actresses/merge` | Apply a target/source merge with conflict resolutions |
| `GET` | `/api/v1/actresses/export` | Export the full actress database as a JSON array (streamed) |
| `POST` | `/api/v1/actresses/import` | Import/update actresses from a JSON array |

**Example - Search actresses:**
```bash
curl "http://localhost:8765/api/v1/actresses/search?q=Sakura"
```

**Example - Merge preview:**
```bash
curl -X POST http://localhost:8765/api/v1/actresses/merge/preview \
  -H "Content-Type: application/json" \
  -d '{"target_id": 12, "source_id": 34}'
```

**Example - Apply merge:**
```bash
curl -X POST http://localhost:8765/api/v1/actresses/merge \
  -H "Content-Type: application/json" \
  -d '{
    "target_id": 12,
    "source_id": 34,
    "resolutions": {
      "first_name": "source",
      "thumb_url": "target"
    }
  }'
```

**Merge status codes:**
- `400`: Invalid IDs, same target/source IDs, or invalid `resolutions` payload.
- `404`: Target or source actress not found.
- `409`: Merge would violate uniqueness constraints (for example `dmm_id` collision).

**Example - Export actresses:**
```bash
curl -b cookies.txt http://localhost:8765/api/v1/actresses/export -o actresses.json
```

`GET /api/v1/actresses/export` streams the entire actress table as a JSON array with `Content-Type: application/json`. Rows are emitted in chunks of 1000 so large libraries (100k+ actresses) do not need to fit in memory. The output is a plain JSON array of actress objects, suitable for backup or migration.

**Example - Import actresses:**
```bash
curl -X POST http://localhost:8765/api/v1/actresses/import \
  -H "Content-Type: application/json" \
  -d '{
    "actresses": [
      {
        "dmm_id": 12345,
        "first_name": "Sakura",
        "last_name": "Momo",
        "japanese_name": "\u685c\u7a7a\u3082\u3082",
        "thumb_url": "https://example.com/thumb.jpg",
        "aliases": "\u3055\u304f\u3089\u3082\u3082"
      }
    ]
  }'
```

`POST /api/v1/actresses/import` accepts `{"actresses": [...]}` (request body capped at 10 MB). Each item may carry `dmm_id`, `first_name`, `last_name`, `japanese_name`, `thumb_url`, and `aliases`. Items are matched by `(japanese_name, dmm_id)`: new entries are created, existing entries are updated when any field changed, and unchanged entries are skipped. Items with both `first_name` and `japanese_name` empty, or a negative `dmm_id`, are rejected and counted as errors. The response is a summary:

```json
{"imported": 1, "skipped": 0, "errors": 0}
```

### Batch Operations

Batch scraping workflow with job tracking and WebSocket progress updates.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/batch` | List batch jobs |
| `POST` | `/api/v1/batch/scrape` | Start batch scrape job for a directory or file list |
| `GET` | `/api/v1/batch/:id` | Get batch job status and results |
| `DELETE` | `/api/v1/batch/:id` | Delete a batch job |
| `POST` | `/api/v1/batch/:id/cancel` | Cancel a running batch job |
| `POST` | `/api/v1/batch/:id/organize` | Organize files from a completed batch job |
| `POST` | `/api/v1/batch/:id/update` | Update batch job files (write NFO + media in place) |
| `PATCH` | `/api/v1/batch/:id/results/:resultId` | Update movie metadata in a batch job |
| `POST` | `/api/v1/batch/:id/results/:resultId/poster-crop` | Update poster crop settings for a result |
| `POST` | `/api/v1/batch/:id/results/:resultId/poster-from-url` | Download a poster from a URL for a result |
| `POST` | `/api/v1/batch/:id/results/:resultId/exclude` | Exclude a movie from organization |
| `POST` | `/api/v1/batch/:id/results/:resultId/preview` | Preview the organization path for a movie |
| `POST` | `/api/v1/batch/:id/results/:resultId/rescrape` | Re-scrape a specific movie in the batch |
| `POST` | `/api/v1/batch/:id/movies/batch-exclude` | Bulk exclude movies from organization |
| `POST` | `/api/v1/batch/:id/movies/batch-rescrape` | Bulk re-scrape movies with merge strategies |

**Example - Start batch scrape:**
```bash
curl -X POST http://localhost:8765/api/v1/batch/scrape \
  -H "Content-Type: application/json" \
  -d '{"directory": "/media/unsorted"}'
```

**Example - Get batch job status:**
```bash
curl http://localhost:8765/api/v1/batch/abc-123-def
```

**`manual_inputs` (PR #68):** `POST /api/v1/batch/scrape` accepts a `manual_inputs` object keyed by file path. Each value is either a JAV ID (scrapes as that ID, bypassing the matcher) or a URL (scrapes with URL-compatible scrapers). This powers the web UI's Manual Scrape flow.

```bash
curl -X POST http://localhost:8765/api/v1/batch/scrape \
  -H "Content-Type: application/json" \
  -d '{
    "files": ["/media/unsorted/vid1.mp4", "/media/unsorted/vid2.mp4"],
    "manual_inputs": {
      "/media/unsorted/vid1.mp4": "IPX-535",
      "/media/unsorted/vid2.mp4": "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis001/"
    }
  }'
```

**Rescrape merge strategies:** `POST /api/v1/batch/:id/results/:resultId/rescrape` and `POST /api/v1/batch/:id/movies/batch-rescrape` accept merge-strategy fields (`preset`, `scalar_strategy`, `array_strategy`) so re-scraped metadata can be merged with the existing NFO instead of replacing it. See the `update` command in the [CLI Reference](./03-cli-reference.md#update) for the strategy values.

### Jobs (History / Revert)

Read and revert organize batch jobs.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/jobs` | List batch jobs (organize-history view) |
| `GET` | `/api/v1/jobs/:id` | Get a single batch job |
| `GET` | `/api/v1/jobs/:id/operations` | List operations for a batch job |
| `GET` | `/api/v1/jobs/:id/revert-check` | Check for overlapping batches before revert |
| `POST` | `/api/v1/jobs/:id/revert` | Revert a batch job |
| `POST` | `/api/v1/jobs/:id/operations/:movieId/revert` | Revert a specific movie within a batch job |

Revert is gated by the `output.allow_revert` setting (default `false`); the `/revert-check` endpoint reports whether a revert is safe.

### File Operations

Browse filesystem, scan directories, and preview organization results.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/cwd` | Get current working directory |
| `POST` | `/api/v1/scan` | Scan directory for JAV files |
| `POST` | `/api/v1/browse` | Browse filesystem (directory listing) |
| `POST` | `/api/v1/browse/autocomplete` | Get path autocomplete suggestions |

**Example - Scan directory:**
```bash
curl -X POST http://localhost:8765/api/v1/scan \
  -H "Content-Type: application/json" \
  -d '{"path": "/media"}'
```

**Security Note:** File operations respect `allowed_directories` and `denied_directories` in `config.yaml`. Docker deployments auto-detect `/media` as allowed. Manual deployments must explicitly configure allowed paths.

### System

Configuration, proxy testing, and scraper management.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/auth/status` | Get auth initialization/login status |
| `POST` | `/api/v1/auth/setup` | First-run setup (create username/password) |
| `POST` | `/api/v1/auth/login` | Login and create session cookie |
| `POST` | `/api/v1/auth/logout` | Logout and clear session cookie |
| `GET` | `/api/v1/config` | Get current configuration |
| `PUT` | `/api/v1/config` | Update configuration (saves to file) |
| `GET` | `/api/v1/scrapers` | List available scrapers and status |
| `POST` | `/api/v1/proxy/test` | Test proxy connection |
| `POST` | `/api/v1/translation/models` | Get available translation models |
| `POST` | `/api/v1/translation/deepl/usage` | Get DeepL usage information |
| `GET` | `/api/v1/version` | Get version status |
| `POST` | `/api/v1/version/check` | Force a version check |

**Example - Get configuration:**
```bash
curl -b cookies.txt http://localhost:8765/api/v1/config
```

**Example - Test proxy:**
```bash
curl -X POST http://localhost:8765/api/v1/proxy/test \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "direct",
    "proxy": {
      "enabled": true,
      "url": "http://proxy.example.com:8080"
    }
  }'
```

### History

Track and rollback file organization operations.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/history` | List operation history |
| `GET` | `/api/v1/history/stats` | Get history statistics |
| `DELETE` | `/api/v1/history/:id` | Delete single history entry |
| `DELETE` | `/api/v1/history` | Bulk delete history entries |

**Example - List history:**
```bash
curl http://localhost:8765/api/v1/history
```

### Resources

Serve temporary and persistent image files.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/temp/posters/:jobId/:filename` | Serve temporary poster from batch job |
| `GET` | `/api/v1/temp/image` | Serve temporary image with query params |
| `GET` | `/api/v1/posters/:filename` | Serve cropped poster from database |

**Example - Get temp poster:**
```bash
curl http://localhost:8765/api/v1/temp/posters/abc-123/IPX-535-poster.jpg -o poster.jpg
```

**Note:** Temp posters are preserved in `data/temp/posters/{jobID}/` when organization fails, allowing retry without re-scraping.

### Genres & Words

Manage genre and word replacements applied during scraping.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/genres` | List all genres in the database |
| `GET` | `/api/v1/genres/replacements` | List genre replacements |
| `POST` | `/api/v1/genres/replacements` | Create a genre replacement |
| `PUT` | `/api/v1/genres/replacements` | Update a genre replacement |
| `DELETE` | `/api/v1/genres/replacements` | Delete a genre replacement |
| `GET` | `/api/v1/genres/replacements/export` | Export genre replacements |
| `POST` | `/api/v1/genres/replacements/import` | Import genre replacements |
| `GET` | `/api/v1/words/replacements` | List word replacements |
| `POST` | `/api/v1/words/replacements` | Create a word replacement |
| `PUT` | `/api/v1/words/replacements` | Update a word replacement |
| `DELETE` | `/api/v1/words/replacements` | Delete a word replacement |
| `GET` | `/api/v1/words/replacements/export` | Export word replacements |
| `POST` | `/api/v1/words/replacements/import` | Import word replacements |

### Tokens

Manage API tokens for programmatic access.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/tokens` | List all active API tokens |
| `POST` | `/api/v1/tokens` | Create a new API token |
| `DELETE` | `/api/v1/tokens/:id` | Revoke an API token |
| `POST` | `/api/v1/tokens/:id/regenerate` | Regenerate an API token |

### Events

Structured event log access.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/events` | List recent events |
| `GET` | `/api/v1/events/stats` | Get event statistics |
| `DELETE` | `/api/v1/events` | Delete events |

### Other Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check endpoint (returns `200 OK`) |
| `GET` | `/ws/progress` | WebSocket endpoint for real-time updates (requires auth session) |
| `GET` | `/docs` | Scalar interactive API documentation |
| `GET` | `/swagger/*` | Swagger UI and OpenAPI spec |
| `GET`/`HEAD` | `/docs/openapi.json` | Raw OpenAPI spec (JSON) backing the docs UIs |

## WebSocket

The `/ws/progress` endpoint provides real-time progress updates for batch operations.
An authenticated session cookie is required.

**Connect to WebSocket:**
```javascript
const ws = new WebSocket('ws://localhost:8765/ws/progress');

ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log('Progress:', update);
};
```

**Message Format:**
```json
{
  "job_id": "abc-123-def",
  "type": "progress",
  "file": "IPX-535.mp4",
  "progress": 0.75,
  "message": "Downloading poster...",
  "bytes_processed": 1024000
}
```

**Event Types:**
- `progress` - Task progress update (0.0 to 1.0)
- `complete` - Task completed successfully
- `error` - Task failed with error message
- `cancelled` - Job cancelled by user

**Use Cases:**
- Real-time batch scrape progress
- Live download status
- Organization operation feedback
- Multi-client synchronization

## CORS Configuration

The API includes CORS middleware for browser-based frontends. Configure in `config.yaml`:

```yaml
api:
  security:
    # Allow all origins (development only)
    allowed_origins: ["*"]

    # Specific origins (recommended for production)
    # allowed_origins: ["http://localhost:5173", "http://127.0.0.1:5173"]

    # Same-origin only (most secure)
    # allowed_origins: []
```

## Directory Security

File operations (scan, browse, organize) are restricted by `allowed_directories` config:

```yaml
api:
  security:
    allowed_directories:
      - /media
      - ~/Videos
    denied_directories:
      - /etc
      - /root
```

**Behavior:**
- Empty `allowed_directories` = deny all (secure by default)
- Docker auto-detects `/media` as allowed
- Attempts to access denied paths return `403 Forbidden`

## Error Responses

Standard error format:

```json
{
  "error": "Not Found",
  "message": "The requested resource does not exist",
  "path": "/api/v1/movies/INVALID-ID",
  "method": "GET"
}
```

**Common HTTP Status Codes:**
- `200 OK` - Success
- `201 Created` - Resource created
- `400 Bad Request` - Invalid request body or parameters
- `403 Forbidden` - Directory access denied
- `404 Not Found` - Resource not found
- `500 Internal Server Error` - Server error

## Complete Documentation

For full request/response schemas, examples, and interactive testing, visit:

- **Scalar UI**: [http://localhost:8765/docs](http://localhost:8765/docs) - Recommended for exploration
- **Swagger UI**: [http://localhost:8765/swagger/index.html](http://localhost:8765/swagger/index.html) - Full OpenAPI spec

These interfaces provide complete documentation including:
- Request body schemas
- Response models
- Query parameter validation
- Example requests for all endpoints
- Try-it-now functionality

---

**Next**: [Migration Guide](./08-migration-guide.md)
