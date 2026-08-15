import { Declaration, Pattern, VariableReference, } from "../json-schema/pattern.js";
export function applySchema(args) {
    const foreignKeyActivated = args.sqlite.exec("PRAGMA foreign_keys", {
        returnValue: "resultRows",
    });
    if (
    // first row that is returned
    // first column of the first row
    // is equal to 0, then foreign keys are disabled
    foreignKeyActivated[0][0] === 0) {
        args.sqlite.exec("PRAGMA foreign_keys = ON", {
            returnValue: "resultRows",
        });
    }
    args.sqlite.exec(`
CREATE TABLE IF NOT EXISTS bundle (
  id TEXT PRIMARY KEY DEFAULT (human_id()),
	declarations BLOB NOT NULL DEFAULT (jsonb('[]'))
) strict;

CREATE TABLE IF NOT EXISTS message (
  id TEXT PRIMARY KEY DEFAULT (uuid_v7()), 
  bundle_id TEXT NOT NULL,
  locale TEXT NOT NULL,
  selectors BLOB NOT NULL DEFAULT (jsonb('[]')),
  FOREIGN KEY (bundle_id) REFERENCES bundle(id) ON DELETE CASCADE
) strict;


CREATE TABLE IF NOT EXISTS variant (
  id TEXT PRIMARY KEY DEFAULT (uuid_v7()), 
  message_id TEXT NOT NULL,
  matches BLOB NOT NULL DEFAULT (jsonb('[]')),
  pattern BLOB NOT NULL DEFAULT (jsonb('[]')),
  FOREIGN KEY (message_id) REFERENCES message(id) ON DELETE CASCADE
) strict;
  
CREATE INDEX IF NOT EXISTS idx_message_bundle_id ON message (bundle_id);
CREATE INDEX IF NOT EXISTS idx_variant_message_id ON variant (message_id);
		`);
}
//# sourceMappingURL=schema.js.map