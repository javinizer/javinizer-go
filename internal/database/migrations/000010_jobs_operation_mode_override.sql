-- +goose Up
-- +goose StatementBegin
ALTER TABLE jobs ADD COLUMN operation_mode_override TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE jobs_backup_010 (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    total_files INTEGER NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    progress REAL NOT NULL DEFAULT 0,
    destination TEXT NOT NULL DEFAULT '',
    temp_dir TEXT NOT NULL DEFAULT 'data/temp',
    files TEXT NOT NULL,
    results TEXT NOT NULL DEFAULT '{}',
    excluded TEXT NOT NULL DEFAULT '{}',
    file_match_info TEXT NOT NULL DEFAULT '{}',
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    organized_at DATETIME,
    reverted_at DATETIME,
    "update" BOOLEAN NOT NULL DEFAULT false
);

INSERT INTO jobs_backup_010 SELECT id, status, total_files, completed, failed, progress, destination, temp_dir, files, results, excluded, file_match_info, started_at, completed_at, organized_at, reverted_at, "update" FROM jobs;

DROP TABLE jobs;

ALTER TABLE jobs_backup_010 RENAME TO jobs;
-- +goose StatementEnd
