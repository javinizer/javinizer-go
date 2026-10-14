-- +goose Up
-- +goose StatementBegin
ALTER TABLE actress_aliases ADD COLUMN alias_name_key TEXT NOT NULL DEFAULT '';
ALTER TABLE actress_aliases ADD COLUMN canonical_name_key TEXT NOT NULL DEFAULT '';
-- Deliberately non-unique: legacy Unicode-equivalent rows are preserved so
-- different-owner conflicts remain inspectable and lookups can fail closed.
CREATE INDEX idx_actress_aliases_alias_name_key ON actress_aliases(alias_name_key);
CREATE INDEX idx_actress_aliases_canonical_name_key ON actress_aliases(canonical_name_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actress_aliases_canonical_name_key;
DROP INDEX IF EXISTS idx_actress_aliases_alias_name_key;
ALTER TABLE actress_aliases DROP COLUMN canonical_name_key;
ALTER TABLE actress_aliases DROP COLUMN alias_name_key;
-- +goose StatementEnd
