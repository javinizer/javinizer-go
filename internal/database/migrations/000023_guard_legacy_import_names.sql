-- +goose Up
-- +goose StatementBegin
-- Absence means ambiguous legacy provenance. Only the importer grants management.
-- A false row records revocation and cannot be bypassed by the legacy override.
CREATE TABLE actress_import_name_provenance (
    actress_id INTEGER PRIMARY KEY REFERENCES actresses(id) ON DELETE CASCADE,
    managed BOOLEAN NOT NULL CHECK (managed IN (0, 1))
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_import_names_revoke
AFTER UPDATE OF first_name, last_name, japanese_name, origin ON actresses
WHEN NEW.first_name IS NOT OLD.first_name
  OR NEW.last_name IS NOT OLD.last_name
  OR NEW.japanese_name IS NOT OLD.japanese_name
  OR NEW.origin IS NOT OLD.origin
BEGIN
    INSERT INTO actress_import_name_provenance (actress_id, managed) VALUES (NEW.id, 0)
    ON CONFLICT(actress_id) DO UPDATE SET managed = 0;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_import_names_delete
AFTER DELETE ON actresses
BEGIN
    DELETE FROM actress_import_name_provenance WHERE actress_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS actress_import_names_delete;
DROP TRIGGER IF EXISTS actress_import_names_revoke;
DROP TABLE IF EXISTS actress_import_name_provenance;
-- +goose StatementEnd
