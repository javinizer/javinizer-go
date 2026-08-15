export const insertBundleNested = async (db, bundle) => {
    await db.transaction().execute(async (trx) => {
        const insertedBundle = await trx
            .insertInto("bundle")
            .values({
            id: bundle.id,
            declarations: bundle.declarations,
        })
            .returning("id")
            .executeTakeFirstOrThrow();
        for (const message of bundle.messages) {
            const insertedMessage = await trx
                .insertInto("message")
                .values({
                id: message.id,
                bundleId: insertedBundle.id,
                locale: message.locale,
                selectors: message.selectors,
            })
                .returning("id")
                .executeTakeFirstOrThrow();
            for (const variant of message.variants) {
                await trx
                    .insertInto("variant")
                    .values({
                    id: variant.id,
                    messageId: insertedMessage.id,
                    matches: variant.matches,
                    pattern: variant.pattern,
                })
                    .execute();
            }
        }
    });
};
//# sourceMappingURL=insertBundleNested.js.map