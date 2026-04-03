-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS movies (
    content_id TEXT PRIMARY KEY,
    id TEXT,
    display_name TEXT,
    title TEXT,
    original_title TEXT,
    description TEXT,
    release_date DATETIME,
    release_year INTEGER,
    runtime INTEGER,
    director TEXT,
    maker TEXT,
    label TEXT,
    series TEXT,
    rating_score REAL,
    rating_votes INTEGER,
    poster_url TEXT,
    cover_url TEXT,
    cropped_poster_url TEXT,
    should_crop_poster NUMERIC,
    trailer_url TEXT,
    original_file_name TEXT,
    screenshots TEXT,
    source_name TEXT,
    source_url TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS actresses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dmm_id INTEGER,
    first_name TEXT,
    last_name TEXT,
    japanese_name TEXT,
    thumb_url TEXT,
    aliases TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS genres (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT
);

CREATE TABLE IF NOT EXISTS genre_replacements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original TEXT NOT NULL,
    replacement TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS actress_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias_name TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS movie_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS history (
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

CREATE TABLE IF NOT EXISTS content_id_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    search_id TEXT NOT NULL,
    content_id TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at DATETIME
);

CREATE TABLE IF NOT EXISTS movie_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id TEXT,
    language TEXT,
    title TEXT,
    original_title TEXT,
    description TEXT,
    director TEXT,
    maker TEXT,
    label TEXT,
    series TEXT,
    source_name TEXT,
    settings_hash VARCHAR(16),
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_movies_translations FOREIGN KEY (movie_id) REFERENCES movies(content_id)
);

CREATE TABLE IF NOT EXISTS movie_actresses (
    movie_content_id TEXT,
    actress_id INTEGER,
    PRIMARY KEY (movie_content_id, actress_id),
    CONSTRAINT fk_movie_actresses_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id),
    CONSTRAINT fk_movie_actresses_actress FOREIGN KEY (actress_id) REFERENCES actresses(id)
);

CREATE TABLE IF NOT EXISTS movie_genres (
    movie_content_id TEXT,
    genre_id INTEGER,
    PRIMARY KEY (movie_content_id, genre_id),
    CONSTRAINT fk_movie_genres_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id),
    CONSTRAINT fk_movie_genres_genre FOREIGN KEY (genre_id) REFERENCES genres(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_content_id ON movies(content_id);
CREATE INDEX IF NOT EXISTS idx_movies_id ON movies(id);
CREATE INDEX IF NOT EXISTS idx_actresses_japanese_name ON actresses(japanese_name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_genres_name ON genres(name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_genre_replacements_original ON genre_replacements(original);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_aliases_alias_name ON actress_aliases(alias_name);
CREATE INDEX IF NOT EXISTS idx_actress_aliases_canonical_name ON actress_aliases(canonical_name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_tag ON movie_tags(movie_id, tag);
CREATE INDEX IF NOT EXISTS idx_history_movie_id ON history(movie_id);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_content_id_mappings_search_id ON content_id_mappings(search_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_language ON movie_translations(movie_id, language);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actresses_dmm_id_positive ON actresses(dmm_id) WHERE dmm_id > 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS movie_genres;
DROP TABLE IF EXISTS movie_actresses;
DROP TABLE IF EXISTS movie_translations;
DROP TABLE IF EXISTS content_id_mappings;
DROP TABLE IF EXISTS history;
DROP TABLE IF EXISTS movie_tags;
DROP TABLE IF EXISTS actress_aliases;
DROP TABLE IF EXISTS genre_replacements;
DROP TABLE IF EXISTS genres;
DROP TABLE IF EXISTS actresses;
DROP TABLE IF EXISTS movies;
-- +goose StatementEnd
