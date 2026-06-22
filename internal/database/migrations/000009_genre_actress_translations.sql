-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS genre_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    genre_id INTEGER NOT NULL,
    language TEXT NOT NULL,
    name TEXT NOT NULL,
    source_name TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_genre_translations_genre FOREIGN KEY (genre_id) REFERENCES genres(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_genre_translations_genre_language ON genre_translations(genre_id, language);

CREATE TABLE IF NOT EXISTS actress_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actress_id INTEGER NOT NULL,
    language TEXT NOT NULL,
    first_name TEXT,
    last_name TEXT,
    japanese_name TEXT,
    display_name TEXT,
    source_name TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_actress_translations_actress FOREIGN KEY (actress_id) REFERENCES actresses(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_translations_actress_language ON actress_translations(actress_id, language);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actress_translations_actress_language;
DROP TABLE IF EXISTS actress_translations;
DROP INDEX IF EXISTS idx_genre_translations_genre_language;
DROP TABLE IF EXISTS genre_translations;
-- +goose StatementEnd
