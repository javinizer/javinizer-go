export declare class ConnectionMutex {
    #private;
    lock(): Promise<void>;
    unlock(): void;
}
