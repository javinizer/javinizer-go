// Test-only, loopback-only guard. Hosted dependency setup runs before this guard.
// Do not load this file into the production application.
const net = require('node:net');
const dns = require('node:dns');
const fs = require('node:fs');
const allowed = (host) =>
	['localhost', 'localhost.', '127.0.0.1', '::1', '[::1]'].includes(String(host).toLowerCase());
function denied() {
	if (process.env.OFFLINE_SYNC_NODE_AUDIT)
		fs.appendFileSync(process.env.OFFLINE_SYNC_NODE_AUDIT, 'OFFLINE_SYNC_NODE_BLOCKED\n');
	throw new Error('Offline sync fixture forbids external network');
}
const connect = net.Socket.prototype.connect;
net.Socket.prototype.connect = function (...args) {
	const normalized = Array.isArray(args[0]) ? args[0] : args;
	const options = normalized[0];
	if (options && typeof options === 'object') {
		if (!options.path && options.host && !allowed(options.host)) denied();
	} else if (typeof normalized[1] === 'string' && !allowed(normalized[1])) denied();
	return connect.apply(this, args);
};
for (const target of [dns, dns.promises]) {
	for (const name of ['lookup', 'resolve', 'resolve4', 'resolve6', 'resolveAny']) {
		const original = target[name];
		target[name] = function (host, ...args) {
			if (!allowed(host)) denied();
			return original.call(this, host, ...args);
		};
	}
}
const fetch = globalThis.fetch;
globalThis.fetch = function (input, ...args) {
	const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
	if (!allowed(url.hostname)) {
		try {
			denied();
		} catch (error) {
			return Promise.reject(error);
		}
	}
	return fetch.call(this, input, ...args);
};
