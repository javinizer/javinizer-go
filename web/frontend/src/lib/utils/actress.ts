export interface ActressName {
	first_name?: string;
	last_name?: string;
	japanese_name?: string;
}

export interface FormatActressNameOptions {
	firstNameOrder?: boolean;
	japaneseNames?: boolean;
}

export function formatActressName<T extends ActressName>(
	actress: T,
	opts?: FormatActressNameOptions
): string {
	const firstNameOrder = opts?.firstNameOrder ?? false;
	const japaneseNames = opts?.japaneseNames ?? false;

	const first = actress.first_name ?? '';
	const last = actress.last_name ?? '';

	if (japaneseNames && actress.japanese_name) {
		return actress.japanese_name;
	}

	if (first === '' && last === '') {
		if (actress.japanese_name) {
			return actress.japanese_name;
		}
		return 'Unknown';
	}

	if (firstNameOrder) {
		if (first !== '' && last !== '') {
			return `${first} ${last}`;
		}
		if (first !== '') {
			return first;
		}
		return last;
	}

	if (first !== '' && last !== '') {
		return `${last} ${first}`;
	}
	if (last !== '') {
		return last;
	}
	return first;
}
