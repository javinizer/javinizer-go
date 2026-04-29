-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS word_replacements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original TEXT NOT NULL,
    replacement TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_word_replacements_original ON word_replacements(original);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_word_replacements_original;
DROP TABLE IF EXISTS word_replacements;
-- +goose StatementEnd
