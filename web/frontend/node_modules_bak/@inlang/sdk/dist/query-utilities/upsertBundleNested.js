export const upsertBundleNested = async (db, bundle) => {
    await db.transaction().execute(async (trx) => {
        const insertedBundle = await trx
            .insertInto("bundle")
            .values({
            id: bundle.id,
            declarations: bundle.declarations,
        })
            .returning("id")
            .onConflict((oc) => oc.column("id").doUpdateSet({
            ...bundle,
            // @ts-expect-error - undefined
            messages: undefined,
        }))
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
                .onConflict((oc) => oc.column("id").doUpdateSet({
                ...message,
                // @ts-expect-error - undefined
                variants: undefined,
            }))
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
                    .onConflict((oc) => oc.column("id").doUpdateSet(variant))
                    .execute();
            }
        }
    });
};
//# sourceMappingURL=upsertBundleNested.js.map