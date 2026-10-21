import { describe, expect, it } from 'vitest';
import { pruneStaleFileSelections } from './file-selection';

describe('pruneStaleFileSelections', () => {
	it.each([
		['POSIX', '/library', '/library/stale.mp4', true],
		['trailing separators', '/library/\\/', '/library/stale.mp4', true],
		['Windows', 'C:\\library\\', 'C:\\library\\stale.mp4', true],
		['Windows drive root', 'C:\\', 'C:\\stale.mp4', true],
		['UNC share', '\\\\server\\share\\', '\\\\server\\share\\stale.mp4', true],
		['forward slash UNC', '//server/share/', '//server/share/stale.mp4', true],
		['mixed final separator', '/library', '/library\\stale.mp4', true],
		['nested selection', '/library', '/library/sub/stale.mp4', false],
		['other directory', '/library', '/elsewhere/stale.mp4', false],
		['same prefix', '/library', '/library-extra/stale.mp4', false],
		['case remains significant', 'C:\\Library', 'C:\\library\\stale.mp4', false],
		['internal separators remain significant', 'C:/library', 'C:\\library\\stale.mp4', false],
		['POSIX root keeps existing interpretation', '/', '/stale.mp4', false],
		['backslash root keeps existing interpretation', '\\', '\\stale.mp4', false],
		['empty directory', '', 'stale.mp4', true],
		['relative directory', 'library/', 'library/stale.mp4', true],
		['separator-only directory', '///', '/stale.mp4', false],
	])('%s', (_name, directory, selected, pruned) => {
		expect(pruneStaleFileSelections([selected], directory, [])).toEqual(pruned ? [] : [selected]);
	});

	it('keeps present files and unrelated selections in order, without mutating inputs', () => {
		const selected = Object.freeze([
			'/library/a.mp4',
			'/library/stale.mp4',
			'/other/b.mp4',
			'/library/a.mp4',
		]);
		const items = Object.freeze([
			{ path: '/library/a.mp4', is_dir: false },
			{ path: '/library/stale.mp4', is_dir: true },
		]);
		expect(pruneStaleFileSelections(selected, '/library', items)).toEqual([
			'/library/a.mp4',
			'/other/b.mp4',
			'/library/a.mp4',
		]);
		expect(selected).toHaveLength(4);
		expect(items).toHaveLength(2);
	});

	it('returns an equal copy on a no-op and handles empty selections', () => {
		const selected = ['/other/a.mp4'];
		const result = pruneStaleFileSelections(selected, '/library', []);
		expect(result).toEqual(selected);
		expect(result).not.toBe(selected);
		expect(pruneStaleFileSelections([], '/library', [])).toEqual([]);
	});
});
