import { colors } from "consola/utils";
import consola from "consola";
consola.options = {
    ...consola.options,
    formatOptions: { date: false },
};
export class Logger {
    options;
    constructor(options = { silent: false, prefix: true }) {
        this.options = options;
    }
    log(message) {
        if (this.options.silent)
            return this;
        const prefix = this.options.prefix ? colors.bold("[paraglide-js] ") : "";
        consola.log(prefix + message);
        return this;
    }
    info(message) {
        if (this.options.silent)
            return this;
        const prefix = this.options.prefix
            ? colors.bold(colors.blue("[paraglide-js] "))
            : "";
        consola.info(prefix + message);
        return this;
    }
    success(message) {
        if (this.options.silent)
            return this;
        const prefix = this.options.prefix
            ? colors.bold(colors.green("[paraglide-js] "))
            : "";
        consola.success(prefix + message);
        return this;
    }
    warn(message, ...args) {
        const prefix = this.options.prefix
            ? colors.bold(colors.yellow("[paraglide-js] "))
            : "";
        consola.warn(prefix + message, ...args);
        return this;
    }
    error(message, ...args) {
        const prefix = this.options.prefix
            ? colors.bold(colors.red("[paraglide-js] "))
            : "";
        consola.error(prefix + message, ...args);
        return this;
    }
    box(message, ...args) {
        if (this.options.silent)
            return this;
        consola.box(message, ...args);
        return this;
    }
}
//# sourceMappingURL=index.js.map