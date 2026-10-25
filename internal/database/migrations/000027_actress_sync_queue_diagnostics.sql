-- +goose Up
-- Additive worker diagnostics only. Never replace receipts or grant name ownership.
CREATE TABLE IF NOT EXISTS actress_sync_task_problems (
 task_id INTEGER PRIMARY KEY REFERENCES actress_sync_tasks(id),
 problem_code TEXT NOT NULL CHECK (problem_code IN ('snapshot_missing', 'snapshot_invalid', 'apply_failed')),
 attempts INTEGER NOT NULL CHECK (typeof(attempts) = 'integer' AND attempts BETWEEN 1 AND 16),
 retry_after_ms INTEGER NOT NULL,
 updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_actress_sync_jobs_history ON actress_sync_jobs(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_queue ON actress_sync_tasks(status, job_id, lease_until_ms);

-- +goose Down
-- Roll back consumers first. Older workers do not enforce persisted backoff;
-- revoke pending grants rather than allowing a downgrade to reactivate them.
-- Keep diagnostics, immutable receipts and tombstones as additive audit history.
INSERT INTO actress_sync_name_guards (actress_id, protected)
 SELECT DISTINCT actress_id, 1 FROM actress_sync_tasks
 WHERE status IN ('pending', 'leased', 'applying')
 ON CONFLICT(actress_id) DO UPDATE SET protected = 1;
UPDATE actress_sync_bindings SET status = 'revoked'
 WHERE status = 'active' AND id IN
 (SELECT binding_id FROM actress_sync_tasks WHERE status IN ('pending', 'leased', 'applying'));
UPDATE actress_sync_tasks SET status = 'invalidated', reason = 'migration_rollback'
 WHERE status IN ('pending', 'leased', 'applying');
