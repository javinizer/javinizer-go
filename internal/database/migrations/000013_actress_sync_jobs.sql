-- +goose Up
-- +goose StatementBegin
CREATE TABLE actress_sync_jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    scope TEXT NOT NULL,
    total_tasks INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    updated INTEGER NOT NULL DEFAULT 0,
    warnings INTEGER NOT NULL DEFAULT 0,
    skipped INTEGER NOT NULL DEFAULT 0,
    conflicts INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    cancelled INTEGER NOT NULL DEFAULT 0,
    cancel_requested NUMERIC NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME
);
CREATE INDEX idx_actress_sync_jobs_status ON actress_sync_jobs(status);
CREATE TABLE actress_sync_tasks (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    actress_id INTEGER,
    label TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL,
    stage TEXT NOT NULL,
    outcome TEXT,
    messages TEXT NOT NULL DEFAULT '[]',
    updated_fields TEXT NOT NULL DEFAULT '[]',
    warning TEXT,
    error_message TEXT,
    lease_owner TEXT,
    lease_token TEXT,
    heartbeat_at DATETIME,
    lease_expires_at DATETIME,
    attempts INTEGER NOT NULL DEFAULT 0,
    stale_retry_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    FOREIGN KEY (job_id) REFERENCES actress_sync_jobs(id) ON DELETE CASCADE,
    FOREIGN KEY (actress_id) REFERENCES actresses(id) ON DELETE SET NULL
);
CREATE INDEX idx_actress_sync_tasks_job_id ON actress_sync_tasks(job_id);
CREATE INDEX idx_actress_sync_tasks_status ON actress_sync_tasks(status);
CREATE INDEX idx_actress_sync_tasks_lease ON actress_sync_tasks(lease_expires_at);
-- Full-library syncs hot-path per-actress lookups (CreateJob dedupe scan,
-- ClaimNext EXISTS predicates); without this the scheduler degrades
-- quadratically as task history grows.
CREATE INDEX idx_actress_sync_tasks_actress_status ON actress_sync_tasks(actress_id, status);
CREATE UNIQUE INDEX idx_actress_sync_tasks_active_key ON actress_sync_tasks(dedupe_key) WHERE status IN ('pending', 'running');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE actress_sync_tasks;
DROP TABLE actress_sync_jobs;
-- +goose StatementEnd