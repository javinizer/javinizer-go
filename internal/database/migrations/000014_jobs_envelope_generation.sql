-- +goose Up
-- +goose StatementBegin
ALTER TABLE jobs ADD COLUMN envelope_generation INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN envelope_generation;
-- +goose StatementEnd
