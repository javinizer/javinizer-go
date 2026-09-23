-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS movie_credit_reassignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_content_id TEXT NOT NULL,
    source_actress_id INTEGER NOT NULL,
    target_actress_id INTEGER NOT NULL,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_movie_credit_reassignments_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id) ON DELETE CASCADE,
    CONSTRAINT fk_movie_credit_reassignments_source FOREIGN KEY (source_actress_id) REFERENCES actresses(id) ON DELETE CASCADE,
    CONSTRAINT fk_movie_credit_reassignments_target FOREIGN KEY (target_actress_id) REFERENCES actresses(id) ON DELETE CASCADE,
    UNIQUE (movie_content_id, source_actress_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_movie_credit_reassignments_target ON movie_credit_reassignments(movie_content_id, target_actress_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_movie_credit_reassignments_target;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS movie_credit_reassignments;
-- +goose StatementEnd
