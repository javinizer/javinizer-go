-- +goose Up
-- +goose StatementBegin
-- The existing positive-DMM uniqueness index cannot serve DMM-less fallback.
CREATE INDEX IF NOT EXISTS idx_actresses_dmm_lookup ON actresses(dmm_id);
-- CLI import preflight also checks exact English name pairs, ordered by DMM
-- ownership and then ID. Keep duplicate names legal and preserve that ordering.
CREATE INDEX IF NOT EXISTS idx_actresses_name_pair_lookup ON actresses(first_name, last_name, dmm_id DESC, id ASC);
-- Reconciliation visits only candidates and verified DMM-less evidence, not
-- every positive-DMM identity imported so far. Preserve legacy NULL eligibility.
CREATE INDEX IF NOT EXISTS idx_actresses_candidate_scan ON actresses(id) WHERE verified = 0;
CREATE INDEX IF NOT EXISTS idx_actresses_verified_dmmless_scan ON actresses(id) WHERE verified = 1 AND (dmm_id <= 0 OR dmm_id IS NULL);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actresses_verified_dmmless_scan;
DROP INDEX IF EXISTS idx_actresses_candidate_scan;
DROP INDEX IF EXISTS idx_actresses_name_pair_lookup;
DROP INDEX IF EXISTS idx_actresses_dmm_lookup;
-- +goose StatementEnd
