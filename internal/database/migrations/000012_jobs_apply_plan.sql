-- +goose Up
-- +goose StatementBegin
ALTER TABLE jobs ADD COLUMN apply_plan TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN apply_plan;
-- +goose StatementEnd