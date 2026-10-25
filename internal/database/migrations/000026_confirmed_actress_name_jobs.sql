-- +goose Up
-- Explicit one-shot Japanese-name updates. No runtime or automatic sync activation.
-- Historical actor IDs intentionally are not cascading foreign keys: identity
-- deletion/replacement must retain audit receipts and protection tombstones.
CREATE TABLE IF NOT EXISTS actress_sync_name_guards (
 actress_id INTEGER PRIMARY KEY,
 protected BOOLEAN NOT NULL CHECK (protected IN (0, 1))
);
CREATE TABLE IF NOT EXISTS actress_sync_bindings (
 id INTEGER PRIMARY KEY,
 actress_id INTEGER NOT NULL,
 source_key TEXT NOT NULL UNIQUE,
 revision INTEGER NOT NULL CHECK (typeof(revision) = 'integer' AND revision > 0),
 snapshot_digest TEXT NOT NULL,
 actor_token TEXT NOT NULL,
 japanese_name TEXT NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('active', 'revoked', 'consumed'))
);
CREATE INDEX IF NOT EXISTS idx_actress_sync_bindings_actor ON actress_sync_bindings(actress_id);
CREATE INDEX IF NOT EXISTS idx_actress_sync_bindings_status ON actress_sync_bindings(status, actress_id);
CREATE TABLE IF NOT EXISTS actress_sync_jobs (
 id TEXT PRIMARY KEY,
 request_key TEXT NOT NULL UNIQUE,
 request_hash TEXT NOT NULL,
 snapshot_digest TEXT NOT NULL,
 max_age_ns INTEGER NOT NULL CHECK (max_age_ns > 0),
 created_at DATETIME NOT NULL,
 cancelled BOOLEAN NOT NULL DEFAULT 0 CHECK (cancelled IN (0, 1))
);
CREATE TABLE IF NOT EXISTS actress_sync_tasks (
 id INTEGER PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES actress_sync_jobs(id),
 actress_id INTEGER NOT NULL,
 binding_id INTEGER NOT NULL REFERENCES actress_sync_bindings(id),
 binding_revision INTEGER NOT NULL,
 source_key TEXT NOT NULL,
 snapshot_digest TEXT NOT NULL,
 actor_token TEXT NOT NULL,
 before_name TEXT NOT NULL,
 after_name TEXT NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('pending', 'leased', 'applying', 'applied', 'unchanged', 'skipped', 'invalidated', 'cancelled')),
 reason TEXT NOT NULL DEFAULT '',
 lease_generation INTEGER NOT NULL DEFAULT 0 CHECK (typeof(lease_generation) = 'integer' AND lease_generation >= 0),
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until_ms INTEGER NOT NULL DEFAULT 0,
 finished_at DATETIME,
 UNIQUE(job_id, actress_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_sync_active_actor ON actress_sync_tasks(actress_id)
 WHERE status IN ('pending', 'leased', 'applying');
CREATE INDEX IF NOT EXISTS idx_actress_sync_claim ON actress_sync_tasks(job_id, status, id);

-- Raw writes and imports cannot silently keep an older confirmation alive.
-- Explicit same-value manual intents additionally use the repository hook.
-- 'applying' exists only inside the fenced mutation transaction; that one task
-- records its own result atomically while all other outstanding work is retired.
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_retire ON actress_sync_tasks(status, actress_id);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS actress_sync_identity_update AFTER UPDATE ON actresses
WHEN OLD.id IS NOT NEW.id OR OLD.dmm_id IS NOT NEW.dmm_id
 OR OLD.first_name IS NOT NEW.first_name OR OLD.last_name IS NOT NEW.last_name
 OR OLD.japanese_name IS NOT NEW.japanese_name OR OLD.thumb_url IS NOT NEW.thumb_url
 OR OLD.origin IS NOT NEW.origin OR OLD.verified IS NOT NEW.verified
 OR OLD.ambiguity_quarantined IS NOT NEW.ambiguity_quarantined
 OR OLD.aliases IS NOT NEW.aliases
BEGIN
 INSERT INTO actress_sync_name_guards VALUES (OLD.id, 1)
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 UPDATE actress_sync_bindings SET status = 'revoked' WHERE actress_id = OLD.id AND status = 'active';
 UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'identity_changed'
 WHERE actress_id = OLD.id AND status IN ('pending', 'leased');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS actress_sync_identity_delete AFTER DELETE ON actresses
BEGIN
 INSERT INTO actress_sync_name_guards VALUES (OLD.id, 1)
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 UPDATE actress_sync_bindings SET status = 'revoked' WHERE actress_id = OLD.id AND status = 'active';
 UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'identity_deleted'
 WHERE actress_id = OLD.id AND status IN ('pending', 'leased');
END;
-- +goose StatementEnd

-- Ignored duplicate inserts are ordinary association persistence: they must
-- not revoke confirmation. AFTER events distinguish successful insertion from
-- ON CONFLICT DO NOTHING / OR IGNORE. Same-ID replacements fence that identity;
-- other uniqueness victims are now absent and their active bindings are retired.
-- Indexed active bindings bound orphan detection to outstanding confirmations,
-- not historical receipts. A later reused actor ID is fenced by the first arm.
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS actress_sync_replace_insert AFTER INSERT ON actresses
BEGIN
 -- With foreign keys/recursive triggers disabled, REPLACE can retain the
 -- old actor's managed-import row even before its first source binding. A
 -- normal fresh importer creates provenance only AFTER the actor insertion.
 INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT NEW.id, 1 WHERE EXISTS
  (SELECT 1 FROM actress_import_name_provenance WHERE actress_id = NEW.id)
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT DISTINCT actress_id, 1 FROM actress_sync_bindings
 WHERE actress_id = NEW.id OR (status = 'active' AND NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_bindings.actress_id))
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'identity_replaced'
 WHERE status IN ('pending', 'leased') AND (actress_id = NEW.id OR NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_tasks.actress_id));
 UPDATE actress_sync_bindings SET status = 'revoked' WHERE status = 'active'
 AND (actress_id = NEW.id OR NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_bindings.actress_id));
END;
-- +goose StatementEnd

-- The normal UPDATE trigger retires the changed actor. Successful replacement
-- additionally retires another removed uniqueness victim, including a new PK.
-- An ignored conflict has no AFTER event and makes no authority changes.
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS actress_sync_replace_update AFTER UPDATE OF id, dmm_id, verified, name_key ON actresses
BEGIN
 -- Moving a primary key never transfers the previous actor's ownership.
 INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT NEW.id, 1 WHERE OLD.id IS NOT NEW.id
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT DISTINCT actress_id, 1 FROM actress_sync_bindings
 WHERE (OLD.id IS NOT NEW.id AND actress_id = NEW.id) OR (status = 'active' AND NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_bindings.actress_id))
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
 UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'identity_replaced'
 WHERE status IN ('pending', 'leased') AND ((OLD.id IS NOT NEW.id AND actress_id = NEW.id) OR NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_tasks.actress_id));
 UPDATE actress_sync_bindings SET status = 'revoked' WHERE status = 'active'
 AND ((OLD.id IS NOT NEW.id AND actress_id = NEW.id) OR NOT EXISTS
  (SELECT 1 FROM actresses WHERE id = actress_sync_bindings.actress_id));
END;
-- +goose StatementEnd

-- +goose Down
-- Roll back consumers first. Retire outstanding grants before removing triggers
-- that reference columns older migrations may drop. Keep additive audit tables
-- and manual protection tombstones; re-upgrade never reactivates old consent.
INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT DISTINCT actress_id, 1 FROM actress_sync_bindings WHERE 1
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
UPDATE actress_sync_bindings SET status = 'revoked' WHERE status = 'active';
UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'migration_rollback'
 WHERE status IN ('pending', 'leased', 'applying');
DROP TRIGGER IF EXISTS actress_sync_identity_update;
DROP TRIGGER IF EXISTS actress_sync_identity_delete;
DROP TRIGGER IF EXISTS actress_sync_replace_insert;
DROP TRIGGER IF EXISTS actress_sync_replace_update;
