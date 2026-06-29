import { describe, it, expect } from 'vitest';
import {
	buildManualScrapeRequest,
	classifyInput,
	mergeManualInputs,
	type ManualRow,
	type ManualScrapeOptions
} from './build-manual-scrape-request';
import type { BatchScrapeRequest } from '$lib/api/types';

describe('classifyInput', () => {
	it('classifies empty/whitespace as auto (matcher on basename)', () => {
		expect(classifyInput('')).toBe('auto');
		expect(classifyInput('   ')).toBe('auto');
		expect(classifyInput('\t\n')).toBe('auto');
	});

	it('classifies http(s) URLs as manual-url (URL-compatible scrapers)', () => {
		expect(classifyInput('https://example.com/v/123')).toBe('manual-url');
		expect(classifyInput('http://example.com/v/123')).toBe('manual-url');
		expect(classifyInput('HTTPS://example.com/v/123')).toBe('manual-url');
	});

	it('classifies ID-shaped input as manual-id (bypasses matcher)', () => {
		expect(classifyInput('IPX-123')).toBe('manual-id');
		expect(classifyInput('abc-001')).toBe('manual-id');
	});

	it('treats a bare scheme-less string with a query as manual-id (not a URL fetch)', () => {
		expect(classifyInput('ABC-?123')).toBe('manual-id');
	});
});

describe('mergeManualInputs', () => {
	it('keeps inputs for files still in the batch and drops removed files (D4b)', () => {
		const stored = { '/a.mp4': 'IPX-1', '/b.mp4': 'IPX-2', '/c.mp4': 'IPX-3' };
		const merged = mergeManualInputs(stored, ['/a.mp4', '/b.mp4', '/d.mp4']);
		expect(merged).toEqual({ '/a.mp4': 'IPX-1', '/b.mp4': 'IPX-2' });
	});

	it('never blind-overwrites: new files get no entry (Auto), existing inputs preserved', () => {
		const stored = { '/a.mp4': 'IPX-1' };
		const merged = mergeManualInputs(stored, ['/a.mp4', '/new.mp4']);
		expect(merged).toEqual({ '/a.mp4': 'IPX-1' });
		expect('/new.mp4' in merged).toBe(false);
	});

	it('drops empty/whitespace values so the map only carries real overrides', () => {
		const stored = { '/a.mp4': 'IPX-1', '/b.mp4': '  ', '/c.mp4': '' };
		const merged = mergeManualInputs(stored, ['/a.mp4', '/b.mp4', '/c.mp4']);
		expect(merged).toEqual({ '/a.mp4': 'IPX-1' });
	});
});

describe('buildManualScrapeRequest', () => {
	const opts: ManualScrapeOptions = {
		destination: '/out',
		operation_mode: 'organize',
		selected_scrapers: ['javdb', 'r18dev'],
		force: true,
		preset: 'gap-fill',
		scalar_strategy: 'prefer-scraper',
		array_strategy: 'replace',
		update: false
	};

	it('drops empty/whitespace inputs from manual_inputs (#1)', () => {
		const rows: ManualRow[] = [
			{ filePath: '/a.mp4', input: 'IPX-123' },
			{ filePath: '/b.mp4', input: '' },
			{ filePath: '/c.mp4', input: '   ' },
			{ filePath: '/d.mp4', input: 'https://e.com/v/1' }
		];
		const req = buildManualScrapeRequest(rows, opts);
		expect(req.manual_inputs).toEqual({
			'/a.mp4': 'IPX-123',
			'/d.mp4': 'https://e.com/v/1'
		});
	});

	it('omits manual_inputs entirely when every input is empty (existing callers unaffected)', () => {
		const rows: ManualRow[] = [
			{ filePath: '/a.mp4', input: '' },
			{ filePath: '/b.mp4', input: '  ' }
		];
		const req = buildManualScrapeRequest(rows, opts);
		expect(req.manual_inputs).toBeUndefined();
		expect(JSON.stringify(req)).not.toContain('manual_inputs');
	});

	it('builds files[] and manual_inputs from the same rows in one pass so keys ⊆ files (#2)', () => {
		const rows: ManualRow[] = [
			{ filePath: '/a.mp4', input: 'IPX-123' },
			{ filePath: '/b.mp4', input: '' },
			{ filePath: '/c.mp4', input: 'SSIS-001' }
		];
		const req = buildManualScrapeRequest(rows, opts);
		expect(req.files).toEqual(['/a.mp4', '/b.mp4', '/c.mp4']);
		for (const key of Object.keys(req.manual_inputs ?? {})) {
			expect(req.files).toContain(key);
		}
	});

	it('trims manual input values', () => {
		const rows: ManualRow[] = [{ filePath: '/a.mp4', input: '  IPX-123  ' }];
		const req = buildManualScrapeRequest(rows, opts);
		expect(req.manual_inputs?.['/a.mp4']).toBe('IPX-123');
	});

	it('passes inherited opts through unchanged (#4)', () => {
		const rows: ManualRow[] = [{ filePath: '/a.mp4', input: 'IPX-123' }];
		const req: BatchScrapeRequest = buildManualScrapeRequest(rows, opts);
		expect(req.destination).toBe('/out');
		expect(req.operation_mode).toBe('organize');
		expect(req.selected_scrapers).toEqual(['javdb', 'r18dev']);
		expect(req.force).toBe(true);
		expect(req.preset).toBe('gap-fill');
		expect(req.scalar_strategy).toBe('prefer-scraper');
		expect(req.array_strategy).toBe('replace');
		expect(req.update).toBe(false);
		expect(req.strict).toBe(false);
	});

	it('handles an empty rows list (no files, no manual_inputs)', () => {
		const req = buildManualScrapeRequest([], opts);
		expect(req.files).toEqual([]);
		expect(req.manual_inputs).toBeUndefined();
	});
});
