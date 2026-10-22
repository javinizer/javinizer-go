# Troubleshooting

Common issues and solutions for Javinizer Go.

## Installation Issues

### "javinizer: command not found"

**Problem**: Binary not in PATH

**Solutions**:
```bash
# Option 1: Move to PATH location
sudo mv javinizer /usr/local/bin/

# Option 2: Run with full path
/path/to/javinizer --help

# Option 3: Add directory to PATH
export PATH=$PATH:/path/to/directory
```

### "Permission denied"

**Problem**: Binary not executable

**Solution**:
```bash
chmod +x javinizer
```

## Configuration Issues

### "Config file not found"

**Problem**: `config.yaml` missing

**Solution**:
```bash
# Initialize configuration
javinizer init

# Or specify custom location
javinizer --config /path/to/config.yaml scrape IPX-535
```

### "Invalid configuration"

**Problem**: Malformed YAML

**Solutions**:
- Check YAML syntax (indentation, colons, quotes)
- Validate with online YAML validator
- Compare with default config
- Run `javinizer info` to see parsed config

## Database Issues

### "Database locked"

**Problem**: Multiple instances accessing database simultaneously

**Solutions**:
- Wait for an active operation to finish, or close other Javinizer instances normally.
- If the lock persists, preserve a consistent backup and inspect the database error
  and configured path before further recovery.
- Do not delete SQLite `-wal` or `-shm` files as lock-file cleanup. They belong to
  the database's transaction state; deleting them can discard recoverable data.
  Keep them with the database when preserving a stopped instance for diagnosis.

### "No such table"

**Problem**: Database not initialized or migrated

**Solution**:
```bash
# Re-initialize database
rm data/javinizer.db
javinizer init
```

### "Database is corrupt"

**Problem**: Corrupted SQLite database

**Solutions**:
```bash
# Option 1: Delete and re-initialize
rm data/javinizer.db
javinizer init

# Option 2: Attempt repair
sqlite3 data/javinizer.db "PRAGMA integrity_check;"
sqlite3 data/javinizer.db ".recover" | sqlite3 data/javinizer-recovered.db
```

> **Note:** The database path defaults to `data/javinizer.db` (set via `database.dsn` in `config.yaml`, or overridden by the `JAVINIZER_DB` environment variable). If you customized it, adjust the paths in the commands above.

## Scraping Issues

### "Failed to scrape: timeout"

**Problem**: Network timeout or slow connection

**Solutions**:
- Check your internet connection
- Try again later (the site may be down or slow)
- Use a different scraper: `javinizer scrape IPX-535 --scrapers dmm`
- Increase the scrape timeout in `config.yaml` (no rebuild required):

```yaml
scrapers:
  timeout_seconds: 60          # HTTP client timeout per request (1–300, default 30)
  request_timeout_seconds: 300 # Overall request timeout (1–600, default 180); raise above the default for slow multi-scraper chains
  browser:
    timeout: 60                # Browser-mode page timeout (default 30)
```

### "404 Not Found"

**Problem**: Movie ID doesn't exist on scraper

**Solutions**:
- Verify ID is correct
- Try different scraper
- Check if movie exists on website manually
- Some IDs only available on specific scrapers

### "No scrapers returned data"

**Problem**: All scrapers failed

**Solutions**:
```bash
# Check scraper configuration
javinizer info

# Enable scrapers in config.yaml
scrapers:
  r18dev:
    enabled: true
  dmm:
    enabled: true

# Test each scraper individually
javinizer scrape IPX-535 --scrapers r18dev
javinizer scrape IPX-535 --scrapers dmm
```

### "Rate limited"

**Problem**: Too many requests to scraper

**Solutions**:
- Wait a few minutes and retry
- Lower concurrency in `config.yaml` (`performance.max_workers`, default 5, range 1–100)
- Add a per-scraper delay in milliseconds (e.g. `scrapers.r18dev.rate_limit`; r18dev defaults to 0 (no delay))
- Spread batch operations out over time

## r18.dev Dump Upload Issues

### Upload rejected by proxy before reaching Javinizer

**Problem**: Uploading a dump via the WebUI fails instantly, times out before
Javinizer logs anything, or the reverse proxy returns 413/502.

If Javinizer sits behind nginx, Traefik, or Cloudflare, upload size and idle
limits apply before the app ever sees the request:

- nginx: raise `client_max_body_size` (the raw dump is ~250 MB; the built
  sidecar can be larger) and `proxy_read_timeout` (multi-minute imports are
  normal; the app streams progress over WebSocket while it works).
- Cloudflare free/proxy: 100 MB request body limit — upload over the LAN
  directly or use the CLI path (`javinizer dump download` on a well-connected
  host, then copy `data/r18dev/r18dev_dump.db` into place and use WebUI
  Update to activate it).

### Upload response 408 (Request Timeout)

The upload receive phase aborts after 5 minutes without incoming bytes
(dead-client detection). The connection is disposed deliberately (HTTP/1:
`Connection: close`; HTTP/2: GOAWAY) — retry on a fresh connection. A 408
wording of "server receive timeout" (rather than "receive stalled") indicates
a pre-existing server timer raced the per-request deadline lift; retrying is
equally correct. On HTTP/2 a pre-lift server write-timer race can surface as
a transport-level stream reset instead of a 408 — again, retry on a fresh
connection.

### Upload hangs on the desktop app

The desktop server uses 30-second read/write deadlines; the upload endpoint
lifts them per-request, so large uploads work. If you still see immediate
failures behind extra middleware that wraps the response writer, report the
"response writer does not support deadline control" message.

### Upload fails with "batch stored X of Y rows"

The r18.dev video key is `(content_id, service_code)`. Current imports preserve
different services for the same raw content ID instead of discarding them.
When a content ID has multiple services, local metadata lookup does not pick
one arbitrarily: it returns a miss and the caller's existing fallback applies.
This is not a change to HTTP fallback matching or a promise of service-aware
HTTP resolution. Upload the original `.sql.gz` again to recover service rows
previously discarded by content-ID-only imports; checking for updates can
otherwise skip an unchanged source. Older sidecars remain readable without
migration. Ordinary exact, display-ID, and case-alias lookups use their existing
indexes, so upgrading does not require rebuilding a retained sidecar. Dense or unusual
case-variant key ranges may use a bounded fallback scan to preserve ambiguity
checks. Newly imported sidecars have an additional case-insensitive
index that also avoids that fallback. Read-only lookups never modify the sidecar.
Older applications cannot validate the new multi-service sidecar.
DVD ID normalization is also validated with the same Unicode-aware transform
used by import and lookup, including upstream IDs with ideographic spaces.

Raw uploads reject any batch that would silently discard rows. The error
reports the table, required key columns, duplicate-key and NULL-key row counts,
and the source table's row range. These are one-based record positions across
that table's COPY blocks, not SQL file line numbers. A duplicate can match a
row in an earlier batch. NULL values in optional metadata do not cause this
error. For videos, repeating the same content ID and service still fails;
missing/NULL service is supported for older inputs but must not repeat for
the same content ID either. The existing sidecar remains unchanged when this
check fails.

The server logs one upload-failure summary. For rejected batches it includes
these same payload-free diagnostics; other failures retain their detailed
error in the WebUI/dump status rather than copying arbitrary uploaded values
into the log.

To investigate a duplicate-key rejection, provide the dump filename/date,
the affected table's CREATE TABLE and key/unique-constraint definitions, the
COPY column header, and a minimal pair of rows sharing the reported key.
Preserve differing key, site and service fields; unrelated text can be
redacted. A full dump is not needed. These details distinguish repeated
records from upstream identities that the sidecar schema cannot yet represent.

## File Matching Issues

### "No files found"

**Problem**: Scanner didn't find any video files

**Solutions**:
```bash
# Check path exists
ls -la /path/to/videos

# Verify file extensions in config (defaults: .mp4, .mkv, .avi, .wmv, .flv)
file_matching:
  extensions: [.mp4, .mkv, .avi, .wmv, .flv]

# Check exclude patterns (defaults: *-trailer*, *-sample*)
file_matching:
  exclude_patterns: ["*-trailer*", "*-sample*"]

# Files smaller than min_size_mb are skipped (0 = no limit)
file_matching:
  min_size_mb: 0

# Recursive scanning is ON by default, so --recursive is rarely the fix.
# To scan only the top-level directory, disable it explicitly:
javinizer sort /path --recursive=false
```

### "ID not detected"

**Problem**: Matcher couldn't extract JAV ID from filename

**Solutions**:
- Ensure filename contains JAV ID (e.g., `IPX-535`)
- Check custom regex if enabled
- Rename file to include ID clearly
- Disable custom regex to use builtin pattern

**Examples of Good Filenames**:
```
IPX-535.mp4
IPX-535 Beautiful Day.mkv
[Studio] IPX-535.avi
```

**Examples of Bad Filenames**:
```
movie.mp4
download (1).mkv
video_file.avi
```

## Organization Issues

### "destination volume cannot express an atomic no-clobber write"

**Problem**: A file cannot be published without risking replacement of an existing destination. On Linux, the no-replace rename and hard-link fallback must both be unavailable for this refusal.

**Why**: No-clobber writes fail closed. Checking that a destination is absent and then using an ordinary rename can overwrite a file created in between. Writing directly to an exclusively created final filename would expose incomplete contents. Neither is a substitute for atomic publication.

**Solutions**:

- Use a publication path that supports an atomic no-replace operation under the application's actual account and mount configuration, or leave the affected files unprocessed.
- Do not use force-update or relax permissions merely to bypass this safety refusal. Those changes do not establish the missing no-clobber guarantee.

### Unraid user shares (shfs): no-clobber preflight refuses

An Unraid user-share destination such as `/mnt/user/Movies` may support neither
`renameat2(RENAME_NOREPLACE)` nor hard links. A typical refusal reports
`renameat2: invalid argument (EINVAL)` and `link: operation not permitted (EPERM)`.
This does not mean all FUSE filesystems lack these operations: Linux FUSE supports
both. In [Linux v6.18](https://github.com/torvalds/linux/blob/v6.18/fs/fuse/dir.c#L1097-L1164),
a daemon's `ENOSYS` becomes `EINVAL` for rename-with-flags and `EPERM` for a link.
That link-error mapping is kernel-dependent: older kernels can return `ENOSYS`
instead. The daemon may also return `EINVAL` or `EPERM` directly, and `EPERM` can
reflect hard-link protection policy. Errno alone does not identify the cause;
see the [link error definitions](https://man7.org/linux/man-pages/man2/link.2.html).

Docker bind mounts of local Unraid user shares use the local shfs path. This is
different from an SMB client accessing a NAS; results from one do not validate
the other.

Javinizer probes the destination directory before streaming direct copies or
cross-device moves, including verified publication paths, refusing unsupported
destinations before creating `.nrstg` payload staging or streaming the source
payload into it. Earlier workflow admission or verification can still read files.
Same-volume moves still use their existing atomic publish directly.
Conclusive probe results are cached per directory for the process lifetime;
transient failures are retried on a later file. Restart after changing mounts or
account configuration. A successful probe is advisory: every final file publish
still enforces atomic no-clobber.

**Candidate workarounds, subject to the same capability checks**:

- Bind-mount the actual disk or pool directory, such as
  `/mnt/disk1/Movies:/storage` or `/mnt/pool-name/Movies:/storage`, to bypass shfs.
  A disk path exposes only that disk's files, not the combined user share.
- For a pool-only share, Unraid's **Exclusive Shares** can bypass shfs without
  changing the share name. Enable **Permit exclusive shares**, select the pool
  as Primary storage and `none` as Secondary storage, and verify **Exclusive
  access: Yes**. The share must not also exist on other volumes. Exclusive shares
  bypass the share/pool minimum-free-space settings; allow sufficient free space.
  See [Unraid's prerequisites](https://docs.unraid.net/unraid-os/release-notes/6.12.0/#exclusive-shares).
- Keep each temporary publication file and its final target on the same mounted
  filesystem. Separate bind mounts can still produce `EXDEV`, even over the same
  underlying filesystem; separate ZFS datasets are not one filesystem merely
  because they share a pool. A cross-filesystem source can be copied, but final
  publication must occur within the destination filesystem.
  See [rename restrictions](https://man7.org/linux/man-pages/man2/renameat2.2.html).

> **Critical: do not copy or move between `/mnt/user/...` and its
> `/mnt/diskN/...` or `/mnt/pool-name/...` aliases.** They may name the same files.
> Unraid warns that mixing these views can cause corruption or permanent data
> loss. Changing a bind-mount path is not an instruction to migrate data between
> those aliases. Follow the [official share guidelines](https://docs.unraid.net/unraid-os/using-unraid-to/manage-storage/shares/#disk-share-guidelines).

Check the path named in the error, including hidden transaction directories.
Older reports in [issue #252](https://github.com/javinizer/javinizer-go/issues/252)
show source-adjacent `.javinizer-apply-*` staging. Current organize-mode staging
uses the planned destination's deepest existing directory where available,
including separately mounted movie folders; videos and multipart payloads are
deferred to final publication rather than eagerly copied into `.source`.
In-place copy operations can still copy inside staging. `system.temp_dir` does
not override all atomic-publication staging locations. Source-side claims may
also require a capable source path; changing only the destination mount does not
resolve every source-side failure.

Local staging followed by an external sync only preserves end-to-end no-clobber
semantics if that sync independently provides them. Neither these workarounds nor
a successful probe establish universal Unraid/NAS compatibility; there is no
non-atomic fallback for no-clobber writes.

If probe cleanup cannot prove ownership or unlink safely, a warning identifies
the retained zero-byte `.nrprobe.*` file. A warning from later scratch cleanup
does not change an already established capability verdict. Inspect retained
files before manually removing them; a foreign substitution must not be treated
as disposable probe residue.
An `internal probe published with incomplete cleanup` error concerns only the
synthetic probe, not publication of the requested payload. It stops that attempt
and is retried on a later operation rather than cached as a capability result.

**Developer validation**: the opt-in [isolated FUSE harness](../tests/fuseintegration/README.md)
uses synthetic local fixtures and actual FUSE syscall paths on a disposable Linux
host with FUSE already available. It does not need a personal NAS, network share,
or real media. Compilation and injected-error tests do not count as a mounted
FUSE pass; even a mounted pass does not validate Unraid's shfs implementation,
SMB/NFS servers, or production NAS configuration.

### "File already exists"

**Problem**: Target file conflicts with existing file

**Solutions**:
- Use `--dry-run` to preview
- Manually resolve conflicts
- Use different destination directory
- Check if you've already processed this file

### "Permission denied" (during move/copy)

**Problem**: Insufficient permissions

**Solutions**:
```bash
# Check permissions
ls -la /destination/path

# Fix permissions
chmod 755 /destination/path

# Run with appropriate user
sudo javinizer sort /path
```

For Docker/Unraid deployments:
- Ensure the container runs with matching IDs (`PUID`/`PGID`, or legacy `USER_ID`/`GROUP_ID`)
- On Unraid, common values are `PUID=99` and `PGID=100`

### "Path too long"

**Problem**: File path exceeds OS limits (Windows: 260 chars)

**Solutions**:
- Simplify the template format
- Remove long fields like `<TITLE>`
- Use a shorter destination path
- Lower the path/title caps in `config.yaml`: `output.max_path_length` (default 240) and `output.max_title_length` (default 100)
- On Windows: enable long paths in the registry

## NFO Generation Issues

### "Invalid XML"

**Problem**: Generated NFO isn't valid XML

**Solutions**:
- Check for special characters in metadata
- Report issue with movie ID
- Manually edit NFO if needed

### "NFO not recognized by Kodi/Plex"

**Problem**: Media server doesn't parse NFO

**Solutions**:
- Verify NFO filename matches video file
- Check NFO is in same directory as video
- Validate XML structure
- Check media server logs

## Download Issues

### "Failed to download cover"

**Problem**: Image download failed

**Solutions**:
- Check your internet connection
- Verify the image URL is accessible (it may be region-locked or behind a CDN that needs `scrapers.referer`/proxy)
- Check available disk space
- Confirm the download is enabled in `config.yaml` (`output.download_cover`, `output.download_poster`) and raise `output.download_timeout` (default 120s — idle/stall timeout: aborts if no bytes received for this duration) if downloads stall. Note: `download_timeout` is NOT a total deadline — large trailer downloads can take longer than this as long as bytes keep flowing. If the worker context (`performance.worker_timeout`) is too short, increase that instead.
- Retry the operation

### "Downloaded file is corrupt"

**Problem**: Incomplete or corrupted download

**Solutions**:
```bash
# Delete partial download
rm /path/to/corrupt/file

# Retry download
javinizer sort /path
```

## Template Issues

### "Template not rendering"

**Problem**: Template tags not replaced with values

**Solutions**:
- Check tag syntax: `<TAG>` not `{TAG}` or `[TAG]`
- Verify the tag name is correct — tags are case-insensitive (`<TITLE>`, `<title>`, and `<Title>` all work); see the full [tag reference](./04-template-system.md#available-tags)
- Check whether the field actually has data: `javinizer scrape IPX-535`
- Review the template in `config.yaml`:

```yaml
output:
  folder_format: "<ID> - <TITLE> (<YEAR>)"
  file_format: "<ID>"
```

### "Special characters in filenames"

**Problem**: Unwanted characters in output

**Solutions**:
- This is automatic sanitization (expected)
- See [Template Guide](./04-template-system.md#special-characters)
- Characters like `:`, `?`, `*` are replaced automatically

## Performance Issues

### "Slow scraping"

**Problem**: Metadata fetching takes too long

**Solutions**:
- Reuse the database cache — already-scraped IDs are not re-fetched unless you pass `--force` (`scrape`) or `--force-refresh` (`sort`)
- Enable only the scrapers you need: trim `scrapers.priority` in `config.yaml`, or pass a subset per run with `--scrapers r18dev,dmm`
- Check your network connection and proxy/FlareSolverr setup (Cloudflare-protected sites require FlareSolverr)
- Consider scraper reliability (R18.dev is usually faster than browser-driven scrapers)

### "High memory usage"

**Problem**: Javinizer using too much RAM

**Solutions**:
- Process smaller batches (scan a smaller directory)
- Lower concurrency in `config.yaml` (`performance.max_workers`, default 5) so fewer scrapes run at once
- Clear the database cache if it has grown very large
- Report the issue with details

## Genre Replacement Issues

### "Replacement not applied"

**Problem**: Genre still shows original name

**Solutions**:
```bash
# Verify replacement exists
javinizer genre list

# Check exact spelling (case-sensitive)
javinizer genre add "Exact Original" "Replacement"

# Re-scrape to apply
javinizer scrape IPX-535
```

## Debug Mode

Enable detailed logging:

```yaml
# config.yaml
logging:
  level: debug
  format: text
  output: stdout
```

Then run the command, capturing the output to a file (logs go to `stdout` when `output: stdout`, so redirect both streams):
```bash
javinizer sort /path --dry-run 2>&1 | tee debug.log
```

## Getting Help

1. **Check documentation**: Review relevant guide
2. **Search issues**: https://github.com/javinizer/javinizer-go/issues
3. **Enable debug logging**: Capture detailed output
4. **Create issue**: Provide:
   - Javinizer version
   - Operating system
   - Command used
   - Error message
   - Debug log (if applicable)

## Common Error Messages

### "no such file or directory"

- Check path exists
- Use absolute paths
- Verify permissions

### "invalid argument"

- Check command syntax
- Verify flag values
- Use quotes for paths with spaces

### "context deadline exceeded"

- Network/scraper timeout — the request exceeded the configured limit
- Raise the scrape timeouts in `config.yaml` (`scrapers.timeout_seconds`, `scrapers.request_timeout_seconds`) and retry
- Check your internet connection and proxy/FlareSolverr availability

### "database schema mismatch"

Preserve the database and affected files before changing anything. Confirm the
configured database path, application version, and startup migration error.
Normal startup applies versioned migrations to the existing database; deleting it
is a destructive reset, not a schema repair. Reinitialization cannot reconstruct
saved credit decisions, mappings, history, or recovery journals.

Use a compatible backup and a reviewed recovery procedure if migration failed.
Do not run an older binary or clear recovery markers to bypass the error; see
[resident publication reconciliation](#resident-publication-reconciliation-is-pending).

---

**Return to**: [Getting Started](./01-getting-started.md)

## Resident publication reconciliation is pending

If Undo reports that resident publication reconciliation is pending, it has
retained a durable safety hold because publication or rollback did not finish
writing its history journal. Undo will refuse filesystem changes for that row;
repeated Undo requests do not clear the hold. History retention also keeps it.

Keep the database and affected files together for manual recovery. Do not clear
`publication_pending` manually or run an older Javinizer binary against this
database: older versions do not recognize this safety hold. Recovery must use
the original publication/rollback ownership proofs, not merely the current
contents at the recorded paths. If the final write could not be retried before
the process exited, automatic recovery of this row is intentionally unavailable.

Undo and resident publication now arbitrate atomically through the same database
journal, including requests using separate database connections. Once Undo (or
journal-authorized backup recovery) begins, `undo_started` permanently prevents
that operation from admitting a new resident publication checkpoint. Failed or
skipped Undo can still be retried; they do not reopen publication. Do not manually
clear either admission marker. This arbitration does not make older binaries
safe to run concurrently against this database.

Apply journal completion is also durable. A committed `apply_terminal` record
closes all Apply journal writers, including delayed replacement and intent
updates. The terminal journal and its path/status metadata commit atomically.
An exact retry of the same terminal payload is a no-op; it rewrites neither
journal nor metadata. A conflicting later completion is refused, so a delayed success cannot replace failed recovery history. Do not
edit these terminal records or open the database with an older binary to bypass
the refusal.
A failure terminal committed without recovery proofs while publication is held
cannot be replaced with a different result later; that row requires manual
recovery. If the terminal transaction itself failed, retrying the original
carried recovery result is still permitted.


## Credit collision retries and older failed jobs

New attempts blocked only by credit collisions remain ready for review and retry
after the collisions are resolved. The live collision gate still runs on retry.

An older job already saved as `failed` is not automatically reopened, even if its
file errors mention only credit collisions. Older startup recovery could erase
interruption evidence, leaving the same saved fields as a completed gate-only
attempt. Resolving a collision or restarting repeatedly cannot establish that
such a job is safe to resume.

Preserve the database and affected files. These ambiguous older jobs require a
separately reviewed, explicit recovery decision; no automatic recovery operation
is provided here. Do not edit their status or clear application data to bypass
admission.

When a file reports `apply blocked by an open credit collision`, use **Review
actress-credit conflicts** on its failed-file card. This selects the affected
movie (including multipart files), switches Poster/Cover view to Detail, and
opens a fresh view of its actress-credit conflicts. Compare each reported field
with the catalog and choose the appropriate available resolution. A conflict
can concern a name, thumbnail, or identity link; it does not necessarily mean
the actress identity is wrong.

The failed-file card records the previous attempt, not the live conflict state.
If the latest check finds no open conflicts, the old failure can still remain
visible. Wait for any resolution and review refresh to finish, then use **Retry
Failed** only when available. Resolution never automatically retries Apply or
reopens an ineligible saved job. The original attempt and settings must also be
verified before retry is available.

`apply blocked because credit collision status is unavailable` means the status
check failed, not that a conflict was confirmed. Use **Check actress-credit
status**; if loading fails, retry that status check and inspect the server logs.
If the file no longer has a reviewable movie, inspect the job details rather
than trying to resolve an unrelated movie. Preserve the database and affected
files for any job that requires an explicit recovery decision, as described
above.
