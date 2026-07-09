export function clearClientStorage(): void {
	if (typeof window === 'undefined') return;

	try {
		localStorage.clear();
	} catch {
		// localStorage may be unavailable in private mode or sandboxed frames
	}

	try {
		const cookies = document.cookie.split(';');
		for (const raw of cookies) {
			const name = raw.split('=')[0]?.trim();
			if (!name) continue;
			document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; max-age=0`;
			document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; domain=${location.hostname}; max-age=0`;
		}
	} catch {
		// ignore cookie access errors
	}
}
