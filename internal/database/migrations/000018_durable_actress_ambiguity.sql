-- +goose Up
-- +goose StatementBegin
ALTER TABLE actresses ADD COLUMN ambiguity_quarantined BOOLEAN NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE actresses
SET ambiguity_quarantined = 1
WHERE verified = 0
  AND dmm_id > 0
  AND id IN (
    SELECT mc.actress_id
    FROM movie_credits mc
    JOIN credit_collisions cc ON cc.credit_id = mc.id
    WHERE cc.field = 'identity_link' AND cc.status = 'open'
  );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE actresses DROP COLUMN ambiguity_quarantined;
-- +goose StatementEnd
