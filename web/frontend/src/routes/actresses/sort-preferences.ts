export const ACTRESS_SORT_STORAGE_KEY = 'javinizer_actresses_sort';

export const actressSortFields = ['name', 'japanese_name', 'id', 'dmm_id', 'updated_at', 'created_at'] as const;
export type ActressSortField = (typeof actressSortFields)[number];
export type ActressSortOrder = 'asc' | 'desc';

export interface ActressSortPreferences {
	sortBy: ActressSortField;
	sortOrder: ActressSortOrder;
}

export const defaultActressSortPreferences: ActressSortPreferences = {
	sortBy: 'name',
	sortOrder: 'asc',
};

export function loadActressSortPreferences(storage: Pick<Storage, 'getItem'> | null): ActressSortPreferences {
	if (!storage) return defaultActressSortPreferences;
	try {
		const parsed = JSON.parse(storage.getItem(ACTRESS_SORT_STORAGE_KEY) ?? 'null') as Partial<ActressSortPreferences> | null;
		if (!parsed || !actressSortFields.includes(parsed.sortBy as ActressSortField) || (parsed.sortOrder !== 'asc' && parsed.sortOrder !== 'desc')) {
			return defaultActressSortPreferences;
		}
		return { sortBy: parsed.sortBy as ActressSortField, sortOrder: parsed.sortOrder };
	} catch {
		return defaultActressSortPreferences;
	}
}

export function saveActressSortPreferences(storage: Pick<Storage, 'setItem'> | null, preferences: ActressSortPreferences): void {
	try {
		storage?.setItem(ACTRESS_SORT_STORAGE_KEY, JSON.stringify(preferences));
	} catch {
		return;
	}
}
