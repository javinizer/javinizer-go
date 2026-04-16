import { describe, it, expect } from 'vitest';
import { splitPath, buildPathUp, buildBreadcrumbPath, isRootPath } from './path';

describe('splitPath', () => {
	it('splits Unix paths', () => {
		expect(splitPath('/media/videos')).toEqual(['media', 'videos']);
	});

	it('splits Windows paths', () => {
		expect(splitPath('X:\\Videos\\Anime')).toEqual(['X:', 'Videos', 'Anime']);
	});

	it('splits Windows root drive', () => {
		expect(splitPath('C:\\')).toEqual(['C:']);
	});

	it('handles Unix root', () => {
		expect(splitPath('/')).toEqual([]);
	});

	it('handles empty path', () => {
		expect(splitPath('')).toEqual([]);
	});

	it('handles mixed separators', () => {
		expect(splitPath('C:\\Users/josep\\Videos')).toEqual(['C:', 'Users', 'josep', 'Videos']);
	});
});

describe('buildPathUp', () => {
	it('uses server parent path when available', () => {
		expect(buildPathUp('X:\\Videos', 'X:\\')).toBe('X:\\');
	});

	it('prefers server parent path even when different from computed', () => {
		expect(buildPathUp('/media/videos', '/media')).toBe('/media');
	});

	it('navigates up Unix paths', () => {
		expect(buildPathUp('/media/videos')).toBe('/media');
	});

	it('navigates up to Unix root', () => {
		expect(buildPathUp('/media')).toBe('/');
	});

	it('navigates up Windows paths', () => {
		expect(buildPathUp('X:\\Videos\\Anime')).toBe('X:\\Videos');
	});

	it('navigates up to Windows drive root', () => {
		expect(buildPathUp('X:\\Videos')).toBe('X:\\');
	});

	it('navigates up from Windows drive root stays at root', () => {
		expect(buildPathUp('C:\\')).toBe('C:\\');
	});

	it('navigates up from deep Windows path', () => {
		expect(buildPathUp('C:\\Users\\josep\\Videos')).toBe('C:\\Users\\josep');
	});

	it('navigates up from single-level Windows path', () => {
		expect(buildPathUp('C:\\Users')).toBe('C:\\');
	});
});

describe('buildBreadcrumbPath', () => {
	it('builds Unix breadcrumb at index 0', () => {
		expect(buildBreadcrumbPath('/media/videos/anime', 0)).toBe('/media');
	});

	it('builds Unix breadcrumb at index 1', () => {
		expect(buildBreadcrumbPath('/media/videos/anime', 1)).toBe('/media/videos');
	});

	it('builds Unix breadcrumb at last index', () => {
		expect(buildBreadcrumbPath('/media/videos/anime', 2)).toBe('/media/videos/anime');
	});

	it('builds Windows breadcrumb at index 0 (drive root)', () => {
		expect(buildBreadcrumbPath('X:\\Videos\\Anime', 0)).toBe('X:\\');
	});

	it('builds Windows breadcrumb at index 1', () => {
		expect(buildBreadcrumbPath('X:\\Videos\\Anime', 1)).toBe('X:\\Videos');
	});

	it('builds Windows breadcrumb at last index', () => {
		expect(buildBreadcrumbPath('X:\\Videos\\Anime', 2)).toBe('X:\\Videos\\Anime');
	});

	it('builds Windows breadcrumb for deep path at index 2', () => {
		expect(buildBreadcrumbPath('C:\\Users\\josep\\Videos', 2)).toBe('C:\\Users\\josep');
	});

	it('builds Windows breadcrumb for deep path at last index', () => {
		expect(buildBreadcrumbPath('C:\\Users\\josep\\Videos', 3)).toBe('C:\\Users\\josep\\Videos');
	});

	it('builds single-component Unix path', () => {
		expect(buildBreadcrumbPath('/media', 0)).toBe('/media');
	});

	it('builds single-component Windows path', () => {
		expect(buildBreadcrumbPath('C:\\Users', 1)).toBe('C:\\Users');
	});
});

describe('isRootPath', () => {
	it('identifies Unix root', () => {
		expect(isRootPath('/')).toBe(true);
	});

	it('identifies Windows drive root with backslash', () => {
		expect(isRootPath('C:\\')).toBe(true);
	});

	it('identifies Windows drive root without backslash', () => {
		expect(isRootPath('X:')).toBe(true);
	});

	it('identifies lowercase drive root', () => {
		expect(isRootPath('d:\\')).toBe(true);
	});

	it('rejects non-root Unix path', () => {
		expect(isRootPath('/media')).toBe(false);
	});

	it('rejects non-root Windows path', () => {
		expect(isRootPath('C:\\Users')).toBe(false);
	});

	it('identifies empty path as root', () => {
		expect(isRootPath('')).toBe(true);
	});

	it('rejects subdirectory under drive root', () => {
		expect(isRootPath('X:\\Videos')).toBe(false);
	});
});
