export const updateBundleNested = async (db, bundle) => {
    await db
        .updateTable("bundle")
        .set(bundle)
        .where("id", "=", bundle.id)
        .execute();
    for (const message of bundle.messages) {
        await db
            .updateTable("message")
            .set(message)
            .where("id", "=", message.id)
            .execute();
        for (const variant of message.variants) {
            await db
                .updateTable("variant")
                .set(variant)
                .where("id", "=", variant.id)
                .execute();
        }
    }
};
//# sourceMappingURL=updateBundleNested.js.map