import { describe, it, expect } from 'vitest';
import { formatActressName } from './actress';
import type { ActressName } from './actress';

function makeActress(over: Partial<ActressName>): ActressName {
	return { first_name: '', last_name: '', japanese_name: '', ...over };
}

describe('formatActressName', () => {
	const cases: Array<{
		name: string;
		actress: Partial<ActressName>;
		firstNameOrder: boolean;
		want: string;
	}> = [
		{
			name: 'both names present, first-name order',
			actress: { first_name: 'Sara', last_name: 'Aoyama' },
			firstNameOrder: true,
			want: 'Sara Aoyama'
		},
		{
			name: 'both names present, last-name order',
			actress: { first_name: 'Sara', last_name: 'Aoyama' },
			firstNameOrder: false,
			want: 'Aoyama Sara'
		},
		{
			name: 'only first name, first-name order',
			actress: { first_name: 'Sara', last_name: '' },
			firstNameOrder: true,
			want: 'Sara'
		},
		{
			name: 'only first name, last-name order',
			actress: { first_name: 'Sara', last_name: '' },
			firstNameOrder: false,
			want: 'Sara'
		},
		{
			name: 'only last name, first-name order',
			actress: { first_name: '', last_name: 'Aoyama' },
			firstNameOrder: true,
			want: 'Aoyama'
		},
		{
			name: 'only last name, last-name order',
			actress: { first_name: '', last_name: 'Aoyama' },
			firstNameOrder: false,
			want: 'Aoyama'
		},
		{
			name: 'no names, japanese_name present',
			actress: { first_name: '', last_name: '', japanese_name: '青山 sarah' },
			firstNameOrder: false,
			want: '青山 sarah'
		},
		{
			name: 'no names, no japanese_name',
			actress: { first_name: '', last_name: '', japanese_name: '' },
			firstNameOrder: false,
			want: 'Unknown'
		},
		{
			name: 'no names, no japanese_name, first-name order',
			actress: { first_name: '', last_name: '', japanese_name: '' },
			firstNameOrder: true,
			want: 'Unknown'
		}
	];

	for (const tc of cases) {
		it(tc.name, () => {
			expect(formatActressName(makeActress(tc.actress), tc.firstNameOrder)).toBe(tc.want);
		});
	}

	it('accepts Actress-like objects with extra fields', () => {
		const actress = {
			id: 1,
			first_name: 'Sara',
			last_name: 'Aoyama',
			japanese_name: '',
			thumb_url: 'http://example.com/x.jpg'
		};
		expect(formatActressName(actress, true)).toBe('Sara Aoyama');
	});
});
