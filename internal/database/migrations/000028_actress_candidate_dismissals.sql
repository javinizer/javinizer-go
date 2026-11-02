-- +goose Up
-- Queue disposition is deliberately separate from shared identity and cast data.
CREATE TABLE actress_candidate_dismissals (
    actress_id INTEGER PRIMARY KEY REFERENCES actresses(id) ON DELETE CASCADE,
    dismissed_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS actress_candidate_dismissals;
