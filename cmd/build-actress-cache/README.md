# Build actress cache

List available adapters with `go run ./cmd/build-actress-cache --list-sources`.

This is a maintainer-only command and is not part of the user-facing `javinizer` CLI. Select sources in priority order: the first source wins field conflicts, while lower-priority sources fill missing fields and retain provenance.

```sh
go run ./cmd/build-actress-cache \
  --source r18dev \
  --source minnanoav \
  --source legacy-jvthumbs \
  --r18dev-dump /path/to/r18dev_dump.db \
  --legacy-csv /path/to/Javinizer/src/Javinizer/jvThumbs.csv \
  --output internal/actresscache/data/actresses.json.gz \
  --audit-output /tmp/actresses-full.json
```

The original Javinizer `jvThumbs.csv` is treated as a low-priority legacy seed. It has strong historical name/thumbnail coverage but no DMM IDs and may contain ambiguous duplicate names, so current DMM-backed sources should be placed before it.

Source-specific settings can be passed without changing the command, for example `--option r18dev.dump=/path/to/r18dev_dump.db`. Adding a source adapter only requires implementing `actresscache.Source` and registering it in `internal/actresscache/sources/registry.go`.

The builder validates every thumbnail as an image, rejects placeholders and undersized files, writes resumable state to `data/actress-cache/build-state.jsonl`, and atomically replaces the output. The state file is local and ignored by Git. The runtime gzip is the checked-in upstream artifact; `--audit-output` optionally writes the full source and validation metadata for maintainer inspection.

Planned (later phase, not wired in this change): the application will embed the compact gzip cache as a read-only runtime fallback. It will fill missing movie metadata and never import or overwrite user actress rows. Use `--refresh` to re-fetch successful candidates.