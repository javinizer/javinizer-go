import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const source = readFileSync('src/routes/review/[jobId]/+page.svelte', 'utf8');

describe('review collision panel content key', () => {
	it('uses the MovieView code field', () => {
		expect(source).toContain('{#if s.currentMovie?.code}');
		expect(source).toContain('movieContentId={s.currentMovie.code}');
		expect(source).toContain('onResolved={() => s.refreshAfterCollision()}');
		expect(source).not.toContain('s.currentMovie?.content_id');
	});
});
