import { prompt } from "../utils.js";
import childProcess from "node:child_process";
export const checkForUncommittedChanges = async (ctx) => {
    try {
        if ((await execAsync("git status --porcelain")).toString().length === 0) {
            return ctx;
        }
        ctx.logger.info(`You have uncommitted changes.\n\nCommitting outstanding changes ensures that you don't lose any work, and see the changes the paraglide-js init command introduces.`);
        const response = await prompt("Continue without committing?", {
            type: "confirm",
            initial: false,
        });
        if (response === true) {
            return ctx;
        }
        else {
            process.exit(0);
        }
    }
    catch {
        // git cli is not installed
        return ctx;
    }
};
function execAsync(command) {
    return new Promise((resolve, reject) => {
        childProcess.exec(command, (error, stdout) => {
            if (error) {
                reject(error);
            }
            else {
                resolve(stdout);
            }
        });
    });
}
//# sourceMappingURL=check-for-uncomitted-changes.js.map