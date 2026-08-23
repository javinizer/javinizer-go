-- +goose Up
-- +goose StatementBegin
ALTER TABLE jobs ADD COLUMN prune_version INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN prune_version;
-- +goose StatementEnd
