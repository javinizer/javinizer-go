const { test } = require('node:test');
const assert = require('node:assert/strict');
const net = require('node:net');
const dns = require('node:dns');
const http = require('node:http');

test('offline sync Node guard rejects external I/O before DNS and allows fixture loopback', async () => {
	assert.throws(
		() => net.connect({ host: 'example.invalid', port: 443 }),
		/forbids external network/,
	);
	assert.throws(() => dns.lookup('example.invalid', () => {}), /forbids external network/);
	await assert.rejects(fetch('https://example.invalid/'), /forbids external network/);
	const server = http.createServer((_, response) => response.end('local fixture'));
	await new Promise((resolve, reject) => {
		server.once('error', reject);
		server.listen(0, '127.0.0.1', resolve);
	});
	try {
		const response = await fetch(`http://127.0.0.1:${server.address().port}/`);
		assert.equal(await response.text(), 'local fixture');
	} finally {
		server.closeAllConnections();
		await new Promise((resolve) => server.close(resolve));
	}
});
