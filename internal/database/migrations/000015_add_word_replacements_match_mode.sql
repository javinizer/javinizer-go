-- +goose Up
-- +goose StatementBegin
ALTER TABLE word_replacements ADD COLUMN match_mode TEXT NOT NULL DEFAULT 'literal';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE word_replacements_backup_015 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original TEXT NOT NULL,
    replacement TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

INSERT INTO word_replacements_backup_015 SELECT id, original, replacement, created_at, updated_at FROM word_replacements;

DROP TABLE word_replacements;

ALTER TABLE word_replacements_backup_015 RENAME TO word_replacements;

CREATE UNIQUE INDEX IF NOT EXISTS idx_word_replacements_original ON word_replacements(original);
-- +goose StatementEnd
