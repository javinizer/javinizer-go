-- +goose Up
-- The graph oracle is single-pass: its own NameKey/bit writes can invalidate it.
CREATE TABLE actress_quarantine_state (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 revision INTEGER NOT NULL CHECK (typeof(revision) = 'integer' AND revision >= 0),
 consumed_revision INTEGER NOT NULL CHECK (consumed_revision >= 0 AND consumed_revision <= revision)
);
INSERT INTO actress_quarantine_state VALUES (1, 1, 0);

-- Outer OR IGNORE can suppress CHECK failures inside a mutation trigger.
-- Explicit ABORT keeps revision overflow/integrity errors fail-closed under
-- every outer conflict policy, rolling back the authoritative statement too.
-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_revision_integrity BEFORE UPDATE ON actress_quarantine_state
WHEN typeof(NEW.revision) != 'integer' OR typeof(NEW.consumed_revision) != 'integer'
 OR NEW.revision < 0 OR NEW.consumed_revision < 0 OR NEW.consumed_revision > NEW.revision
BEGIN
 SELECT RAISE(ABORT, 'invalid actress quarantine revision');
END;
-- +goose StatementEnd


-- INSERT/UPDATE OR REPLACE can delete victims without firing DELETE triggers.
-- Actual actress uniqueness: id; positive dmm_id; nonempty candidate name_key.
-- Any future unique constraint must update the victim invalidation contract.
-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_insert BEFORE INSERT ON actresses
-- OR REPLACE substitutes the NOT NULL default after BEFORE INSERT sees NULL.
WHEN NEW.verified IS NULL OR (NEW.verified = 0 OR (NEW.verified = 1 AND (NEW.dmm_id <= 0 OR NEW.dmm_id IS NULL)))
 OR EXISTS (SELECT 1 FROM actresses victim WHERE victim.id = NEW.id AND
  (victim.verified = 0 OR (victim.verified = 1 AND (victim.dmm_id <= 0 OR victim.dmm_id IS NULL))))
 OR (NEW.dmm_id > 0 AND EXISTS (SELECT 1 FROM actresses victim
  WHERE victim.dmm_id > 0 AND victim.dmm_id = NEW.dmm_id AND
  (victim.verified = 0 OR (victim.verified = 1 AND (victim.dmm_id <= 0 OR victim.dmm_id IS NULL)))))
 OR (NEW.verified = 0 AND NEW.name_key IS NOT NULL AND NEW.name_key != '' AND
  EXISTS (SELECT 1 FROM actresses victim WHERE victim.verified = 0 AND
   victim.name_key IS NOT NULL AND victim.name_key != '' AND victim.name_key = NEW.name_key))
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_conflict_update BEFORE UPDATE OF id, dmm_id, verified, name_key ON actresses
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_evidence_update AFTER UPDATE OF first_name, last_name, japanese_name, ambiguity_quarantined ON actresses
WHEN ((OLD.verified = 0 OR (OLD.verified = 1 AND (OLD.dmm_id <= 0 OR OLD.dmm_id IS NULL))) OR (NEW.verified = 0 OR (NEW.verified = 1 AND (NEW.dmm_id <= 0 OR NEW.dmm_id IS NULL)))) AND (OLD.first_name IS NOT NEW.first_name OR OLD.last_name IS NOT NEW.last_name OR OLD.japanese_name IS NOT NEW.japanese_name OR OLD.ambiguity_quarantined IS NOT NEW.ambiguity_quarantined)
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_delete AFTER DELETE ON actresses
WHEN (OLD.verified = 0 OR (OLD.verified = 1 AND (OLD.dmm_id <= 0 OR OLD.dmm_id IS NULL)))
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_actress_aliases_insert BEFORE INSERT ON actress_aliases
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_actress_aliases_update BEFORE UPDATE ON actress_aliases
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_actress_aliases_delete AFTER DELETE ON actress_aliases
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_movie_credits_insert BEFORE INSERT ON movie_credits
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_movie_credits_update BEFORE UPDATE ON movie_credits
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_movie_credits_delete AFTER DELETE ON movie_credits
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_credit_collisions_insert BEFORE INSERT ON credit_collisions
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_credit_collisions_update BEFORE UPDATE ON credit_collisions
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER actress_quarantine_credit_collisions_delete AFTER DELETE ON credit_collisions
BEGIN
 UPDATE actress_quarantine_state SET revision = revision + 1 WHERE id = 1;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER actress_quarantine_credit_collisions_delete;
DROP TRIGGER actress_quarantine_credit_collisions_update;
DROP TRIGGER actress_quarantine_credit_collisions_insert;
DROP TRIGGER actress_quarantine_movie_credits_delete;
DROP TRIGGER actress_quarantine_movie_credits_update;
DROP TRIGGER actress_quarantine_movie_credits_insert;
DROP TRIGGER actress_quarantine_actress_aliases_delete;
DROP TRIGGER actress_quarantine_actress_aliases_update;
DROP TRIGGER actress_quarantine_actress_aliases_insert;
DROP TRIGGER actress_quarantine_delete;
DROP TRIGGER actress_quarantine_evidence_update;
DROP TRIGGER actress_quarantine_conflict_update;
DROP TRIGGER actress_quarantine_insert;
DROP TRIGGER actress_quarantine_revision_integrity;
DROP TABLE actress_quarantine_state;
