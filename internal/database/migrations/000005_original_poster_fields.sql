-- +goose Up
-- +goose StatementBegin
ALTER TABLE movies ADD COLUMN original_poster_url TEXT;
ALTER TABLE movies ADD COLUMN original_cropped_poster_url TEXT;
ALTER TABLE movies ADD COLUMN original_should_crop_poster NUMERIC;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE movies DROP COLUMN original_should_crop_poster;
ALTER TABLE movies DROP COLUMN original_cropped_poster_url;
ALTER TABLE movies DROP COLUMN original_poster_url;
-- +goose StatementEnd
