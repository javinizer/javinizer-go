/**
 * Filesystem fixture helpers for full-stack E2E specs.
 *
 * The e2emock scraper derives MovieID from the filename via the matcher
 * regex (see cmd/javinizer-e2e config: matching.regex_pattern). File content
 * is irrelevant — the scraper never reads the file. So we write 1-byte
 * placeholder files cheaply, organized so the global-setup fan-outs a canonical set
 * per test run. Individual specs can additionally seed more input files
 * via {@link seedInputFiles} without polluting the shared set.
 */
import { mkdirSync, rmSync } from 'node:fs';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';

export const DEFAULT_INPUT_DIR = process.env.JAVINIZER_E2E_INPUT_DIR ?? '/tmp/javinizer-e2e-input';
export const DEFAULT_OUTPUT_DIR =
	process.env.JAVINIZER_E2E_OUTPUT_DIR ?? '/tmp/javinizer-e2e-output';

/**
 * Canonical fixture set seeded in global-setup so every spec can rely on a
 * stable, predictable input directory. Add new fixtures here when a spec
 * needs an always-available baseline file; per-spec ad-hoc files belong in
 * the spec via {@link seedInputFiles}.
 */
export const CANONICAL_FIXTURES: readonly string[] = [
	'GOOD-001.mp4', // baseline scrape-success single-part
	'FAIL-002.mp4', // baseline verbose scrape-failure
	'FAIL-003.mp4', // secondary scrape-failure for cross-spec isolation
	'MULTI-001-pt1.mp4', // multipart (pt1) — paired with MULTI-001-pt2
	'MULTI-001-pt2.mp4', // multipart (pt2)
] as const;

/**
 * Make both input + output fixture dirs exist on disk, removing any pre-existing
 * canonical fixture files first (so a stale GOOD-001.mp4 from a prior broken
 * run doesn't leak in). Safe to call repeatedly.
 *
 * Called by global-setup once before every Playwright run.
 */
export async function ensureFixtureDirs(): Promise<void> {
	mkdirSync(DEFAULT_INPUT_DIR, { recursive: true });
	mkdirSync(DEFAULT_OUTPUT_DIR, { recursive: true });
	for (const name of CANONICAL_FIXTURES) {
		rmSync(join(DEFAULT_INPUT_DIR, name), { force: true });
	}
	await Promise.all(
		CANONICAL_FIXTURES.map((name) => writeFile(join(DEFAULT_INPUT_DIR, name), 'e2e')),
	);
}

/**
 * Seed an ad-hoc set of input fixture files inside the input dir. Files
 * pre-existing with the same name are deleted first so the spec runs
 * deterministically regardless of prior state.
 *
 * Use this when a specific spec needs files beyond the canonical baseline
 * set (e.g. a spec for a particular edge-case ID prefix). Don't add to
 * CANONICAL_FIXTURES unless every spec would benefit — keeps the baseline
 * minimal + predictable.
 */
export async function seedInputFiles(names: readonly string[]): Promise<void> {
	for (const name of names) {
		rmSync(join(DEFAULT_INPUT_DIR, name), { force: true });
	}
	await Promise.all(names.map((name) => writeFile(join(DEFAULT_INPUT_DIR, name), 'e2e')));
}
