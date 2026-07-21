// UI preference keys survive the wipe — they are not per-server state. The
// locale pin must persist or a language picked on the setup wizard is
// silently discarded on the next page load (the wipe runs on every mount
// while the server is uninitialized).
const PRESERVED_KEYS = ['javinizer-locale', 'javinizer-theme'];

export function clearClientStorage(): void {
	if (typeof window === 'undefined') return;

	try {
		const preserved = PRESERVED_KEYS.map((key) => [key, localStorage.getItem(key)] as const);
		localStorage.clear();
		for (const [key, value] of preserved) {
			if (value !== null) localStorage.setItem(key, value);
		}
	} catch {
		// localStorage may be unavailable in private mode or sandboxed frames
	}

	try {
		const cookies = document.cookie.split(';');
		const hostname = location.hostname;
		for (const raw of cookies) {
			const name = raw.split('=')[0]?.trim();
			if (!name) continue;
			document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; max-age=0`;
			if (hostname) {
				document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; domain=${hostname}; max-age=0`;
				document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; domain=.${hostname}; max-age=0`;
			}
		}
	} catch {
		// ignore cookie access errors
	}
}
