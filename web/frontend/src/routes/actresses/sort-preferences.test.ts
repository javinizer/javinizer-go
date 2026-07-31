import { describe, expect, it } from 'vitest';
import {
	ACTRESS_SORT_STORAGE_KEY,
	defaultActressSortPreferences,
	loadActressSortPreferences,
	saveActressSortPreferences,
} from './sort-preferences';

function storageWith(value: string | null): Storage {
	const values = new Map<string, string>();
	if (value !== null) values.set(ACTRESS_SORT_STORAGE_KEY, value);
	return {
		get length() { return values.size; },
		clear: () => values.clear(),
		getItem: (key) => values.get(key) ?? null,
		key: (index) => [...values.keys()][index] ?? null,
		removeItem: (key) => values.delete(key),
		setItem: (key, next) => values.set(key, next),
	};
}

describe('actress sort preferences', () => {
	it('round-trips a valid sort selection', () => {
		const storage = storageWith(null);
		saveActressSortPreferences(storage, { sortBy: 'updated_at', sortOrder: 'desc' });
		expect(loadActressSortPreferences(storage)).toEqual({ sortBy: 'updated_at', sortOrder: 'desc' });
	});

	it.each([
		'not-json',
		JSON.stringify({ sortBy: 'unsupported', sortOrder: 'asc' }),
		JSON.stringify({ sortBy: 'name', sortOrder: 'sideways' }),
	])('falls back for invalid storage: %s', (value) => {
		expect(loadActressSortPreferences(storageWith(value))).toEqual(defaultActressSortPreferences);
	});
});
