import { describe, it, expect, beforeEach } from 'vitest';
import { clearClientStorage } from './storage';

describe('clearClientStorage', () => {
	beforeEach(() => {
		localStorage.clear();
		for (const raw of document.cookie.split(';')) {
			const name = raw.split('=')[0]?.trim();
			if (name) document.cookie = name + '=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/';
		}
	});

	it('preserves UI preference keys while wiping per-server state', () => {
		localStorage.setItem('javinizer-locale', 'zh-Hans');
		localStorage.setItem('javinizer-theme', 'dark');
		localStorage.setItem('javinizer-session', 'stale-session-id');
		localStorage.setItem('some-other-cache', 'x');

		clearClientStorage();

		expect(localStorage.getItem('javinizer-locale')).toBe('zh-Hans');
		expect(localStorage.getItem('javinizer-theme')).toBe('dark');
		expect(localStorage.getItem('javinizer-session')).toBeNull();
		expect(localStorage.getItem('some-other-cache')).toBeNull();
	});

	it('clears cookies', () => {
		document.cookie = 'javinizer-session=abc; path=/';

		clearClientStorage();

		expect(document.cookie).not.toContain('javinizer-session');
	});
});
