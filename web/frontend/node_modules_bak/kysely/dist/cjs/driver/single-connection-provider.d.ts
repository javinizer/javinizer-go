import type { DatabaseConnection } from './database-connection.js';
import type { ConnectionProvider } from './connection-provider.js';
export declare class SingleConnectionProvider implements ConnectionProvider {
    #private;
    constructor(connection: DatabaseConnection);
    /**
     * Provides a connection for the callback and takes care of disposing
     * the connection after the callback has been run.
     */
    provideConnection<T>(consumer: (connection: DatabaseConnection) => Promise<T>): Promise<T>;
}
