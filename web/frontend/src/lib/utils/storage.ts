// UI preference keys survive the wipe — they are not per-server state. The
// locale pin must persist or a language picked on the setup wizard is
// silently discarded on the next page load (the wipe runs on every mount
// while the server is uninitialized).
const PRESERVED_KEYS = ['javinizer-locale', 'javinizer-locale-choice', 'javinizer-theme'];

export function clearClientStorage(): void {
	if (typeof window === 'undefined') return;

	try {
		// Remove only non-preserved keys instead of clear()+restore: a setItem
		// failure mid-restore (e.g. Safari private mode QuotaExceededError)
		// would otherwise silently lose the preserved preferences.
		const keysToRemove: string[] = [];
		for (let i = 0; i < localStorage.length; i++) {
			const key = localStorage.key(i);
			if (key && !PRESERVED_KEYS.includes(key)) keysToRemove.push(key);
		}
		for (const key of keysToRemove) {
			localStorage.removeItem(key);
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
