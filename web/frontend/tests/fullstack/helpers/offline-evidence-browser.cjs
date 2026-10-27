// Test-only guard: deny before the browser can contact any external HTTP/WS host.
// Keep audit markers secret-free; failed requests must fail the hosted proof gate.
const { appendFileSync } = require('node:fs');
/** @param {string} raw */
function isLoopbackURL(raw) {
	try {
		const url = new URL(raw);
		return (
			['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) &&
			['localhost', 'localhost.', '127.0.0.1', '[::1]'].includes(url.hostname.toLowerCase())
		);
	} catch {
		return false;
	}
}
function recordBlocked() {
	if (process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT)
		appendFileSync(
			process.env.OFFLINE_EVIDENCE_BROWSER_AUDIT,
			'OFFLINE_EVIDENCE_BROWSER_BLOCKED\n',
		);
}
/** @param {import('@playwright/test').BrowserContext} context */
async function installOfflineEvidenceBrowserGuard(context) {
	await context.route('**/*', async (route) => {
		if (isLoopbackURL(route.request().url())) await route.continue();
		else {
			recordBlocked();
			await route.abort('blockedbyclient');
		}
	});
	await context.routeWebSocket('**/*', (socket) => {
		if (isLoopbackURL(socket.url())) socket.connectToServer();
		else {
			recordBlocked();
			socket.close({ code: 1008, reason: 'Offline fixture only' });
		}
	});
}
module.exports = { installOfflineEvidenceBrowserGuard, isLoopbackURL };
