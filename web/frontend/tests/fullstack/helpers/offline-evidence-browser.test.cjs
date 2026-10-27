const { test } = require('node:test');
const assert = require('node:assert/strict');
const { mkdtempSync, readFileSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join } = require('node:path');
const {
	installOfflineEvidenceBrowserGuard,
	isLoopbackURL,
} = require('./offline-evidence-browser.cjs');
test('browser guard admits loopback and blocks external HTTP and WebSocket before connection', async () => {
	const root = mkdtempSync(join(tmpdir(), 'evidence-guard-'));
	const before = process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT;
	process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT = join(root, 'audit');
	try {
		let http, ws;
		let allowed = 0,
			denied = 0;
		await installOfflineEvidenceBrowserGuard({
			route: async (_, handler) => {
				http = handler;
			},
			routeWebSocket: async (_, handler) => {
				ws = handler;
			},
		});
		for (const url of ['http://localhost:5179', 'https://127.0.0.1/', 'ws://[::1]:18080/'])
			assert.equal(isLoopbackURL(url), true);
		for (const url of [
			'https://example.invalid',
			'https://localhost.evil.invalid',
			'https://127.0.0.1.evil.invalid',
			'file:///tmp/example',
			'invalid',
		])
			assert.equal(isLoopbackURL(url), false);
		for (const url of ['http://localhost:5179/', 'https://example.invalid/'])
			await http({
				request: () => ({ url: () => url }),
				continue: async () => allowed++,
				abort: async () => denied++,
			});
		for (const url of ['ws://127.0.0.1:18080/', 'wss://example.invalid/'])
			ws({ url: () => url, connectToServer: () => allowed++, close: () => denied++ });
		assert.equal(allowed, 2);
		assert.equal(denied, 2);
		assert.equal(
			readFileSync(process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT, 'utf8'),
			'OFFLINE_EVIDENCE_BROWSER_BLOCKED\n'.repeat(2),
		);
	} finally {
		if (before === undefined) delete process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT;
		else process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT = before;
		rmSync(root, { recursive: true, force: true });
	}
});
