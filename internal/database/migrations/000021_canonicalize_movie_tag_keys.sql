-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_movie_tag;
UPDATE movie_tags
SET movie_id = (
    SELECT movies.content_id
    FROM movies
    WHERE movies.id = movie_tags.movie_id
      AND movies.id IS NOT NULL
      AND movies.id <> ''
      AND movies.content_id IS NOT NULL
      AND movies.content_id <> ''
)
WHERE NOT EXISTS (
    SELECT 1 FROM movies WHERE movies.content_id = movie_tags.movie_id
)
AND 1 = (
    SELECT COUNT(*)
    FROM movies
    WHERE movies.id = movie_tags.movie_id
      AND movies.id IS NOT NULL
      AND movies.id <> ''
      AND movies.content_id IS NOT NULL
      AND movies.content_id <> ''
);
DELETE FROM movie_tags
WHERE id NOT IN (
    SELECT MIN(id)
    FROM movie_tags
    GROUP BY movie_id, tag
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_tag ON movie_tags(movie_id, tag);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
