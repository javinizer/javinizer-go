-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actress_translations_actress_language;
DELETE FROM actress_translations WHERE LOWER(TRIM(language)) = '';
UPDATE actress_translations SET language = LOWER(TRIM(language));
DELETE FROM actress_translations
WHERE id NOT IN (
    SELECT MIN(id)
    FROM actress_translations
    GROUP BY actress_id, language
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_translations_actress_language ON actress_translations(actress_id, language);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
