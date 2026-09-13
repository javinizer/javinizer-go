-- +goose Up
-- +goose StatementBegin
ALTER TABLE actresses ADD COLUMN verified BOOLEAN NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE actresses ADD COLUMN origin TEXT NOT NULL DEFAULT 'user';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE actresses ADD COLUMN name_key TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE movies ADD COLUMN render_dirty BOOLEAN NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE movies ADD COLUMN render_generation INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE actresses SET name_key = NULL WHERE name_key = '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_actresses_name_key_candidate ON actresses(name_key) WHERE verified = 0 AND name_key IS NOT NULL AND name_key != '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS movie_actresses_backup_016 (
    movie_content_id TEXT,
    actress_id INTEGER,
    PRIMARY KEY (movie_content_id, actress_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO movie_actresses_backup_016 (movie_content_id, actress_id)
    SELECT movie_content_id, actress_id FROM movie_actresses;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS movie_credits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_content_id TEXT NOT NULL,
    actress_id INTEGER NOT NULL,
    credited_name TEXT NOT NULL DEFAULT '',
    credited_japanese_name TEXT NOT NULL DEFAULT '',
    reported_thumb_url TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL DEFAULT 'user',
    order_index INTEGER NOT NULL DEFAULT 0,
    order_pinned BOOLEAN NOT NULL DEFAULT 0,
    override_name TEXT NOT NULL DEFAULT '',
    user_override BOOLEAN NOT NULL DEFAULT 0,
    suppressed BOOLEAN NOT NULL DEFAULT 0,
    legacy_inferred BOOLEAN NOT NULL DEFAULT 0,
    display_force_canonical BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_movie_credits_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id),
    CONSTRAINT fk_movie_credits_actress FOREIGN KEY (actress_id) REFERENCES actresses(id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_credits_pair ON movie_credits(movie_content_id, actress_id);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO movie_credits (movie_content_id, actress_id, credited_name, credited_japanese_name, reported_thumb_url, source, origin, order_index, user_override, suppressed, legacy_inferred, created_at, updated_at)
SELECT
    ma.movie_content_id,
    ma.actress_id,
    COALESCE(a.last_name, '') || ' ' || COALESCE(a.first_name, ''),
    COALESCE(a.japanese_name, ''),
    COALESCE(a.thumb_url, ''),
    'legacy',
    'user',
    ROW_NUMBER() OVER (PARTITION BY ma.movie_content_id ORDER BY ma.actress_id) - 1,
    0,
    0,
    1,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
FROM movie_actresses ma
JOIN actresses a ON a.id = ma.actress_id
WHERE NOT EXISTS (
    SELECT 1 FROM movie_credits mc WHERE mc.movie_content_id = ma.movie_content_id AND mc.actress_id = ma.actress_id
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS credit_collisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    credit_id INTEGER NOT NULL,
    movie_content_id TEXT NOT NULL,
    field TEXT NOT NULL,
    reported_value TEXT NOT NULL DEFAULT '',
    canonical_value TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    resolution TEXT NOT NULL DEFAULT '',
    occurrences INTEGER NOT NULL DEFAULT 0,
    sources_seen TEXT NOT NULL DEFAULT '',
    user_pinned BOOLEAN NOT NULL DEFAULT 0,
    last_seen_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_credit_collisions_credit FOREIGN KEY (credit_id) REFERENCES movie_credits(id),
    CONSTRAINT fk_credit_collisions_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_credit_collisions_movie ON credit_collisions(movie_content_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_credit_collisions_status ON credit_collisions(status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS credit_collisions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS movie_credits;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE movie_actresses_restore_016 (
    movie_content_id TEXT,
    actress_id INTEGER,
    PRIMARY KEY (movie_content_id, actress_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO movie_actresses_restore_016 (movie_content_id, actress_id)
    SELECT movie_content_id, actress_id FROM movie_actresses_backup_016;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS movie_actresses;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE movie_actresses_restore_016 RENAME TO movie_actresses;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS movie_actresses_backup_016;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actresses_name_key_candidate;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actresses_dmm_id_unique;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE movies DROP COLUMN render_generation;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE movies DROP COLUMN render_dirty;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE actresses DROP COLUMN name_key;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE actresses DROP COLUMN origin;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE actresses DROP COLUMN verified;
-- +goose StatementEnd
