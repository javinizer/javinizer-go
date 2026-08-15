export type LoggerOptions = {
    silent: boolean;
    /**
     * If the [paraglide] prefix should be printed before each log message.
     */
    prefix: boolean;
};
export declare class Logger {
    private options;
    constructor(options?: LoggerOptions);
    log(message: string): Logger;
    info(message: string): Logger;
    success(message: string): Logger;
    warn(message: any, ...args: any[]): Logger;
    error(message: any, ...args: any[]): Logger;
    box(message: any, ...args: any[]): Logger;
}
//# sourceMappingURL=index.d.ts.map