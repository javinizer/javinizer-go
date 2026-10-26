// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		interface PageState {
			/** One-shot successful Jobs review return; never a destination path. */
			browseReturnPath?: string;
		}
		// interface Platform {}
	}
}

export {};
