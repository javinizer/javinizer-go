import { test as base, expect, type Page } from '@playwright/test';
import { spawn, execFile, type ChildProcess } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, chmod, writeFile, readFile, rm, copyFile, rename } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
const run = promisify(execFile);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..');
const binary = process.env.E2E_SYNC_BINARY ?? join(repoRoot, 'bin/javinizer-sync-e2e');
const port = Number(process.env.E2E_SYNC_BACKEND_PORT ?? 18081);
const frontendPort = Number(process.env.E2E_SYNC_VITE_PORT ?? 5181);
export type SyncOptions = {
	mode?: 'ready' | 'disabled' | 'missing' | 'stale' | 'future';
	pauseAfter?: number;
};
export type FixtureActor = {
	id: number;
	dmm_id: number;
	name: string;
	before_name: string;
	after_name: string;
	source_id: string;
	source_key: string;
};
export type Fixture = {
	actors: Record<
		'managed' | 'managed2' | 'editable' | 'unchanged' | 'manual' | 'guarded' | 'unicode',
		FixtureActor
	>;
	digests: Record<string, string>;
	source_count: number;
	movie_id: string;
};
export type Row = Record<string, string | number | boolean | null>;
export type Snapshot = Record<
	'actors' | 'jobs' | 'tasks' | 'bindings' | 'guards' | 'credits' | 'translations' | 'aliases',
	Row[]
>;
/** Host-only controls: no control routes or mocked successful application responses. */
export class SyncHarness {
	fixture!: Fixture;
	private child?: ChildProcess;
	private exited = true;
	private exitCode: number | null = null;
	private error?: Error;
	private output = '';
	private options: SyncOptions;
	private readyManifest?: Buffer;
	constructor(
		readonly root: string,
		options: SyncOptions,
	) {
		this.options = { mode: 'ready', pauseAfter: -1, ...options };
	}
	get logs() {
		return this.output;
	}
	private env(): NodeJS.ProcessEnv {
		return {
			...process.env,
			JAVINIZER_SYNC_E2E_ROOT: this.root,
			JAVINIZER_SYNC_E2E_PORT: String(port),
			JAVINIZER_SYNC_E2E_ORIGIN: `http://localhost:${frontendPort}`,
			JAVINIZER_SYNC_E2E_MODE: this.options.mode,
			JAVINIZER_SYNC_E2E_PAUSE_AFTER: String(this.options.pauseAfter),
		};
	}
	async start() {
		if (this.child && !this.exited) throw new Error('Backend is already running');
		await Promise.all(
			['worker.paused', 'worker.release'].map((p) => rm(join(this.root, p), { force: true })),
		);
		this.exited = false;
		this.exitCode = null;
		this.error = undefined;
		const start = this.output.length;
		this.child = spawn(binary, [], {
			cwd: repoRoot,
			env: this.env(),
			stdio: ['ignore', 'pipe', 'pipe'],
		});
		const capture = (b: Buffer) => {
			this.output += b.toString();
		};
		this.child.stdout?.on('data', capture);
		this.child.stderr?.on('data', capture);
		this.child.once('error', (e) => {
			this.error = e;
			this.exited = true;
		});
		this.child.once('exit', (code) => {
			this.exitCode = code;
			this.exited = true;
		});
		try {
			await expect
				.poll(
					async () => {
						if (this.error) throw this.error;
						if (this.exited) throw new Error(`Fixture exited ${this.exitCode}: ${this.output}`);
						if (!this.output.slice(start).includes('javinizer-sync-e2e: ready')) return false;
						try {
							return (
								await fetch(`http://127.0.0.1:${port}/health`, {
									signal: AbortSignal.timeout(1000),
								})
							).ok;
						} catch {
							return false;
						}
					},
					{ timeout: 30_000, message: 'real offline fixture must become ready' },
				)
				.toBe(true);
			this.fixture = JSON.parse(await readFile(join(this.root, 'fixture.json'), 'utf8'));
			this.readyManifest ??= await readFile(join(this.root, 'artifacts/ready/current.json'));
		} catch (e) {
			throw new Error(
				`Could not start fixture. Build cmd/javinizer-sync-e2e first. ${String(e)}\n${this.output}`,
			);
		}
	}
	async stop() {
		if (!this.child || this.exited) return;
		this.child.kill('SIGTERM');
		try {
			await expect
				.poll(() => this.exited, { timeout: 10_000, message: 'runtime shutdown must join worker' })
				.toBe(true);
		} catch (e) {
			this.child.kill('SIGKILL');
			await expect.poll(() => this.exited, { timeout: 5000 }).toBe(true);
			throw e;
		}
		if (this.exitCode !== 0)
			throw new Error(`Fixture shutdown failed ${this.exitCode}: ${this.output}`);
	}
	async restart(options: SyncOptions = {}) {
		await this.stop();
		this.options = { ...this.options, ...options };
		await this.start();
	}
	async waitForPause(): Promise<{ job_id: string; applied: number }> {
		let marker: { job_id: string; applied: number } | undefined;
		await expect
			.poll(
				async () => {
					try {
						marker = JSON.parse(await readFile(join(this.root, 'worker.paused'), 'utf8'));
						return true;
					} catch {
						return false;
					}
				},
				{ timeout: 15_000 },
			)
			.toBe(true);
		return marker!;
	}
	async releaseWorker() {
		await writeFile(join(this.root, 'worker.release'), 'release\n', { mode: 0o600 });
	}
	async inspect(): Promise<Snapshot> {
		const { stdout } = await run(binary, ['inspect'], {
			cwd: repoRoot,
			env: this.env(),
			timeout: 15_000,
			maxBuffer: 1024 * 1024,
		});
		return JSON.parse(stdout);
	}
	/** Replace only disposable host fixture files; no application control API. */
	async replaceEvidence(variant: 'ready' | 'stale' | 'corrupt') {
		const target = join(this.root, 'artifacts/ready');
		if (variant === 'corrupt') {
			await writeFile(join(target, 'current.json'), 'broken fixture manifest', { mode: 0o600 });
			return;
		}
		const manifest =
			variant === 'ready'
				? this.readyManifest!
				: await readFile(join(this.root, 'artifacts', variant, 'current.json'));
		if (variant !== 'ready')
			await copyFile(
				join(this.root, 'artifacts', variant, `${this.fixture.digests[variant]}.ndjson`),
				join(target, `${this.fixture.digests[variant]}.ndjson`),
			);
		await writeFile(join(target, 'next-fixture.json'), manifest, { mode: 0o600 });
		await rename(join(target, 'next-fixture.json'), join(target, 'current.json'));
	}
	async login(page: Page) {
		const response = await page.request.post('/api/v1/auth/login', {
			data: { username: 'admin', password: 'adminpassword123', remember_me: true },
		});
		expect(response.ok(), await response.text()).toBe(true);
	}
}
export const test = base.extend<{ sync: SyncHarness; syncOptions: SyncOptions }>({
	syncOptions: [{}, { option: true }],
	sync: async ({ syncOptions }, use, testInfo) => {
		const root = await mkdtemp(join(tmpdir(), 'javinizer-sync-browser-'));
		await chmod(root, 0o700);
		await writeFile(join(root, '.javinizer-sync-e2e'), 'javinizer-sync-e2e disposable fixture\n', {
			mode: 0o600,
		});
		const sync = new SyncHarness(root, syncOptions);
		try {
			await sync.start();
			await use(sync);
		} finally {
			try {
				await sync.stop();
			} finally {
				if (testInfo.status !== testInfo.expectedStatus)
					await testInfo.attach('sync-backend.log', { body: sync.logs, contentType: 'text/plain' });
				await rm(root, { recursive: true, force: true });
			}
		}
	},
});
// Browser integration is fixture-only, including third-party image/provenance URLs.
// Keep network consumers disabled even when Chromium runs outside a socket sandbox.
test.beforeEach(async ({ context }) => {
	await context.route('**/*', async (route) => {
		const host = new URL(route.request().url()).hostname;
		if (['localhost', '127.0.0.1', '[::1]'].includes(host)) await route.continue();
		else await route.abort('blockedbyclient');
	});
});
export { expect };
