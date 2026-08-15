import { createInMemoryDatabase } from "./createInMemoryDatabase.js";
import { importDatabase } from "./importDatabase.js";
export async function loadDatabaseInMemory(data) {
    const database = await createInMemoryDatabase({
        readOnly: false,
    });
    importDatabase({
        db: database,
        content: new Uint8Array(data),
    });
    return database;
}
//# sourceMappingURL=loadDatabaseInMemory.js.map