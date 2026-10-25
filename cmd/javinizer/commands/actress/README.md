# Actress commands

The existing `merge`, `export` and `import` commands operate on the application
database. `provision-cache` is deliberately separate: it does not initialize
application configuration, logging, desktop storage or database dependencies.

## Provision an offline evidence artifact

First choose an existing private directory with no untrusted writers. The CLI
will not create it or change its permissions. On Linux/macOS it must not be
group/other writable. Supply a regular uncompressed PostgreSQL COPY dump file;
symlink leaves, directories, FIFOs and over-budget files are rejected. The input
file must not be modified during conversion. No SQL is executed, compressed
container is expanded, remote URL is fetched, or image bytes are imported.

```sh
javinizer actress provision-cache \
  --dump /absolute/path/to/supplied-dump.sql \
  --artifact-dir /absolute/path/to/private-evidence \
  --source-url https://r18.dev/dump \
  --parser-revision reviewed-copy-v1 \
  --observed-at 2026-01-02T03:04:05Z
```

Every flag is required. The URL is provenance only and must have no user info,
query (including an empty `?`) or fragment. All queries are refused rather than
trying to guess which query keys contain secrets. Source URL, parser revision
and observation time are explicit caller assertions, not authentication or proof
of complete upstream coverage. Supply the actual source observation time: the
CLI never replaces it with the publication time or file mtime. The source SHA-256
covers all exact accepted input bytes through EOF; the artifact SHA-256 covers
the independent canonical evidence bytes. Explicit empty actress COPY blocks
remain empty evidence, never an instruction to prune application records.

The CLI calls the real bounded `r18dump.Convert` and `artifactstore.Publish`.
Shared finite caps are described in `internal/actresscache/offline/README.md`.
Only Linux/macOS publication is enabled. Windows provisioning refuses before
opening/converting the dump, while runtime readers can use an existing store.
There is no live-fetch fallback, network access, SQL execution, implicit source
selection, application import, deletion/pruning, or permission weakening.

After a publication attempt, stdout contains one JSON result with
`artifact_sha256`, `source_sha256`, `source_bytes`, `actress_rows`, `observed_at`,
`published`, `commit_attempted`, `files_synced`, `directory_synced` and
`outcome_uncertain`. The command also returns a nonzero exit status on errors.

- `published=true` means current-manifest replacement succeeded. A later sync
  error is still visible publication, not a safe-to-retry nonexecution.
- `published=false` with `commit_attempted=true` is uncertain visibility, not
  proof that current stayed unchanged. Inspect the validated current/previous
  generation before choosing another action; the CLI does not retry.
- Failure before current replacement does not promise a pristine directory:
  bounded immutable objects/stages or the retained writer lock may remain.
- Even if stdout itself fails, the returned error includes the publication and
  synchronization flags. Do not blindly retry a command with an uncertain reply.

Keep immutable objects referenced by active durable sync jobs even after they
leave current/previous. The package does not guess a latest object or prune
unreferenced files to make space. When capacity is reached, publication fails
until a separately reviewed maintenance operation is available. Neither a
successful parse nor publication grants identity merge, credit, override,
trusted-alias, candidate-promotion or source-completeness authority.
