# Existing video destinations

Video collisions are separate from NFO merging and artwork replacement. The persisted version-1 Apply plan has an additive `video_collision_policy` field:

| Policy | Behavior |
|---|---|
| `fail` | Default, including old jobs and omitted fields. Refuse unresolved video collisions before downloading or publishing output. |
| `skip` | Leave both videos intact and skip that selected file's entire Apply, including NFO and artwork. This is a non-error outcome, not an installation. |
| `replace` | Replace only an explicitly reviewed regular video, using the server's signed review receipt. A bulk policy alone never authorizes replacement. |

There is no Keep Both, automatic suffix naming, or automatic quality selection. Equal bytes in two independent files still require a decision. The same physical file, including a proven hardlink or case alias, is a video no-op; requested metadata work may still run.

## Web Review

Review automatically checks stable destination settings. **Video destination review** shows actual source/destination paths, sizes, and new/replaced/skipped/unresolved/same-file counts. Choose **Skip entire Apply** or **Replace reviewed video**, or exclude unresolved files. Directories, symlinks (including dangling links), and objects whose identity cannot be proven cannot be replaced. Use **Review / refresh destinations** after an external filesystem change.

Decisions survive normal refreshes, retries, and tab reloads through the existing recovery settings and tab-local session storage. They remain usable only while the reviewed source, destination, naming configuration, movie render generation, and pre-admission Apply generation match. Any intervening Apply invalidates old receipts, even when it skipped every file. Refresh destination review and choose a new decision before retrying. The server atomically fences admission against the reviewed generation even when `expected_apply_generation` is omitted, and keeps that scope fixed while the admitted Apply advances the job generation. A changed or vanished target requires a fresh review. Browser guards are not authorization: the API and final filesystem handoff independently enforce these checks.

Each selected multipart file is planned independently. A selected, excluded, or skipped sibling is not silently consumed by another selected file. Automatically discovered, unselected multipart collisions cannot inherit the primary video's replacement decision: select/review those parts separately, or skip/exclude the containing work.

## API

`POST /api/v1/batch/:id/video-collisions` accepts the same destination, operation, retry subset, and review overrides as Organize, and returns `items`, `new`, `replaced`, `skipped`, `unresolved`, and `same_file`. Each occupied regular-file item includes a server-issued `review_token` when strong filesystem identity is available.

Submit endpoint-specific choices with `POST /api/v1/batch/:id/organize`:

```json
{
  "destination": "/library",
  "copy_only": true,
  "video_collision_policy": "fail",
  "video_collision_decisions": {
    "/incoming/IPX-277.mp4": {
      "source": "/incoming/IPX-277.mp4",
      "destination": "/library/IPX-277/IPX-277.mp4",
      "policy": "replace",
      "review_token": "the exact token returned by the server"
    }
  }
}
```

The same two fields may be supplied in `overrides`; do not supply contradictory policies or duplicate decision maps. Invalid policies are rejected. A movie ID, client-generated token, or replacement policy without an exact valid receipt is insufficient. Two selected files cannot sequentially replace the same endpoint: skip/exclude competitors so there is one winner.

Unresolved/stale requests return HTTP 409 with `code` and a refreshed `video_collision_review`. Late workflow refusals retain structured `video_collision` details on the file result. File results persist `video_outcome` (`installed`, `replaced`, `skipped`, or `same-file`) independently of failure status. Skips are excluded from Retry Failed and create no Undo-owned file operation.

## Publication, recovery, and Undo

Review reads video content to pin SHA-256, size, modification time, and strong native file identity. Save metadata edits explicitly before reviewing video destinations; automatic preflight never saves drafts. Full-content snapshot checks require extra read I/O for large videos. On slow disks or network shares, increase `performance.worker_timeout` when necessary; snapshot reads honor cancellation and the existing per-file timeout. Replacement re-proves the reviewed objects under destination coordination, verifies the taken-aside object, journals an owned backup, and publishes atomically without replacing a late arrival. Failed/cancelled publication rolls back without consuming an unproven source; occupied recovery paths are retained rather than clobbered.

The existing durable operation recorder is independent of `allow_revert`: disabling the ordinary Undo action does not disable replacement recovery. Missing durable recording or staged-publication capability blocks replacement. Supported copy/move Undo restores the displaced video; move restores the source, while copy retains its source. A foreign late claimant can block automatic restoration; keep the reported owned backup and use the recovery/Undo result rather than deleting files manually.

`force_overwrite` still controls metadata merging. `overwrite_existing_media` still controls downloaded artwork/media. Neither grants video replacement. The legacy CLI `sort --force-update` remains an intentional, separate legacy overwrite path; Web Organize does not enable it. Supplying a typed video policy or endpoint decisions never inherits additional authority from that legacy flag. There is no new global YAML video-overwrite flag: the safe default is in the job's Apply plan and can be overridden explicitly in Review/API.

Native Windows regressions run against the current checkout in `video-collision-regression.yml`. Local macOS results are not Windows execution evidence.
