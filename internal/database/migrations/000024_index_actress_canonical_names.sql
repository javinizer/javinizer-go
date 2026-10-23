-- +goose Up
-- Derived lookup only: canonical names remain authoritative. Raw SQL writers
-- enqueue changed identities without needing an application-defined SQL function.
CREATE TABLE actress_canonical_keys (
    name_key TEXT NOT NULL,
    actress_id INTEGER NOT NULL,
    PRIMARY KEY (name_key, actress_id)
);
CREATE INDEX idx_actress_canonical_keys_identity ON actress_canonical_keys(actress_id);
CREATE TABLE actress_canonical_keys_dirty (actress_id INTEGER PRIMARY KEY);
INSERT INTO actress_canonical_keys_dirty SELECT id FROM actresses;
-- +goose StatementBegin
CREATE TRIGGER actress_canonical_keys_insert AFTER INSERT ON actresses BEGIN
    INSERT OR IGNORE INTO actress_canonical_keys_dirty VALUES (NEW.id);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER actress_canonical_keys_update AFTER UPDATE OF id, first_name, last_name, japanese_name ON actresses
WHEN OLD.id IS NOT NEW.id OR OLD.first_name IS NOT NEW.first_name OR OLD.last_name IS NOT NEW.last_name OR OLD.japanese_name IS NOT NEW.japanese_name BEGIN
    DELETE FROM actress_canonical_keys WHERE actress_id = OLD.id;
    DELETE FROM actress_canonical_keys_dirty WHERE actress_id = OLD.id;
    INSERT OR IGNORE INTO actress_canonical_keys_dirty VALUES (NEW.id);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER actress_canonical_keys_delete AFTER DELETE ON actresses BEGIN
    DELETE FROM actress_canonical_keys WHERE actress_id = OLD.id;
    DELETE FROM actress_canonical_keys_dirty WHERE actress_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER actress_canonical_keys_delete;
DROP TRIGGER actress_canonical_keys_update;
DROP TRIGGER actress_canonical_keys_insert;
DROP TABLE actress_canonical_keys_dirty;
DROP TABLE actress_canonical_keys;
