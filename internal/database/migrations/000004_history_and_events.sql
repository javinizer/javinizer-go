-- +goose Up
-- +goose StatementBegin
-- Add batch_job_id column to history table (nullable for backward compatibility)
ALTER TABLE history ADD COLUMN batch_job_id TEXT;
CREATE INDEX IF NOT EXISTS idx_history_batch_job_id ON history(batch_job_id);

-- Add reverted_at column to jobs table for tracking when a batch was reverted
ALTER TABLE jobs ADD COLUMN reverted_at DATETIME;

-- Create batch_file_operations table for per-file organize details
CREATE TABLE IF NOT EXISTS batch_file_operations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_job_id TEXT NOT NULL,
    movie_id TEXT,
    original_path TEXT NOT NULL,
    new_path TEXT NOT NULL,
    operation_type TEXT NOT NULL DEFAULT 'move',
    nfo_snapshot TEXT,
    generated_files TEXT,
    revert_status TEXT NOT NULL DEFAULT 'applied',
    reverted_at DATETIME,
    in_place_renamed NUMERIC NOT NULL DEFAULT 0,
    original_dir_path TEXT,
    nfo_path TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CONSTRAINT fk_bfo_batch_job_id FOREIGN KEY (batch_job_id) REFERENCES jobs(id)
);

CREATE INDEX IF NOT EXISTS idx_bfo_batch_job_id ON batch_file_operations(batch_job_id);
CREATE INDEX IF NOT EXISTS idx_bfo_batch_job_revert_status ON batch_file_operations(batch_job_id, revert_status);

-- Create events table for structured event logging (independent of history data)
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    context TEXT,
    source TEXT,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_severity ON events(severity);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
CREATE INDEX IF NOT EXISTS idx_events_type_severity ON events(event_type, severity);
CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
CREATE INDEX IF NOT EXISTS idx_events_type_source ON events(event_type, source);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop events table and its indexes
DROP INDEX IF EXISTS idx_events_type_source;
DROP INDEX IF EXISTS idx_events_source;
DROP INDEX IF EXISTS idx_events_type_severity;
DROP INDEX IF EXISTS idx_events_created_at;
DROP INDEX IF EXISTS idx_events_severity;
DROP INDEX IF EXISTS idx_events_type;
DROP TABLE IF EXISTS events;

-- Drop batch_file_operations table and its indexes
DROP INDEX IF EXISTS idx_bfo_batch_job_revert_status;
DROP INDEX IF EXISTS idx_bfo_batch_job_id;
DROP TABLE IF EXISTS batch_file_operations;

-- Remove reverted_at column from jobs (SQLite 3.35.0+ supports DROP COLUMN)
ALTER TABLE jobs DROP COLUMN reverted_at;

-- Remove batch_job_id column from history (SQLite requires table rebuild)
CREATE TABLE history_backup (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id TEXT,
    operation TEXT,
    original_path TEXT,
    new_path TEXT,
    status TEXT,
    error_message TEXT,
    metadata JSON,
    dry_run NUMERIC,
    created_at DATETIME
);

INSERT INTO history_backup SELECT id, movie_id, operation, original_path, new_path, status, error_message, metadata, dry_run, created_at FROM history;

DROP TABLE history;

ALTER TABLE history_backup RENAME TO history;

CREATE INDEX IF NOT EXISTS idx_history_movie_id ON history(movie_id);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history(created_at);
-- +goose StatementEnd