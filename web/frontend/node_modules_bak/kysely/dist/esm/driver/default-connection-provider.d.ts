import type { DatabaseConnection } from './database-connection.js';
import type { ConnectionProvider } from './connection-provider.js';
import type { Driver } from './driver.js';
export declare class DefaultConnectionProvider implements ConnectionProvider {
    #private;
    constructor(driver: Driver);
    /**
     * Provides a connection for the callback and takes care of disposing
     * the connection after the callback has been run.
     */
    provideConnection<T>(consumer: (connection: DatabaseConnection) => Promise<T>): Promise<T>;
}
