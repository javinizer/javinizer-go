/**
 * Browser-context WebSocket progress helpers for full-stack E2E specs.
 *
 * Opens a REAL WebSocket to /ws/progress FROM THE BROWSER PAGE (the same path
 * the app's own websocket store uses), bridging received ProgressMessage
 * frames to the Node side via page.exposeFunction. The browser context
 * inherits the global-setup session cookie (storageState), so the WS upgrade
 * handshake carries `javinizer_session` automatically — the same auth path the
 * app uses. NO Node-side WS client, NO manual Cookie/Origin headers: this
 * exercises the real browser → Vite /ws proxy → Go backend path, so a
 * regression in that path (e.g. the changeOrigin same-origin 403) surfaces here.
 *
 * Requires the Vite /ws proxy to use changeOrigin:false (see vite.config.ts /
 * vite.fullstack.config.ts) so the backend's isSameOrigin check (Origin vs
 * request Host, port-sensitive) passes for a browser-context upgrade.
 */
import type { Page } from '@playwright/test';

/** Shape of a WS ProgressMessage frame (mirrors internal/websocket types). */
export interface ProgressFrame {
	job_id: string;
	file_index: number;
	file_path: string;
	status: string;
	progress: number;
	message: string;
	error?: string;
}

/** Terminal organize-completion WS statuses emitted by OnPhaseComplete. */
export const ORGANIZE_TERMINAL_STATUSES = ['organization_completed', 'update_completed'] as const;

/** Handle to an open progress WebSocket + its captured frames. */
export interface ProgressWebSocket {
	/** Frames received for the target job (filtered by job_id; the hub
	 * broadcasts to all connected clients). Ordered by arrival. */
	frames: ProgressFrame[];
	/** Resolves when an organization_completed/update_completed frame for the
	 * job arrives. REJECTS if the socket closes mid-test before a terminal
	 * frame (fast-fail instead of a silent timeout). Callers MUST handle the
	 * rejection (e.g. Promise.race with a timeout/poll) to avoid an unhandled
	 * rejection. */
	terminal: Promise<void>;
	/** Close the browser-side WS. Safe to call once; ignores errors if the
	 * page has navigated/closed. */
	close(): Promise<void>;
}

/**
 * Open a real browser-context WebSocket to /ws/progress and bridge frames to
 * the Node-side `frames` array. Navigates the page to the app root first so
 * location.origin resolves to the Vite dev server (which proxies /ws to the Go
 * backend) and the session cookie applies to the handshake.
 *
 * Returns once the socket is OPEN, so the caller can submit work only after
 * the client is subscribed (no missed frames). On open failure the browser
 * socket is closed before re-throwing (no orphaned connection).
 */
export async function openProgressWebSocket(page: Page, jobId: string): Promise<ProgressWebSocket> {
	// Navigate to the app root so the page origin is the Vite dev server and
	// the session cookie (scoped to that origin) is sent on the WS handshake.
	await page.goto('/');
	await page.waitForLoadState('domcontentloaded');

	const frames: ProgressFrame[] = [];

	// Unique binding names per call so two opens on the same page never collide.
	const tag = Math.random().toString(36).slice(2);
	const pushFn = `e2ePushFrame_${tag}`;
	const openFn = `e2eWsOpened_${tag}`;
	const openFailFn = `e2eWsOpenFailed_${tag}`;
	const termFailFn = `e2eWsTermFailed_${tag}`;

	let resolveOpen!: () => void;
	let rejectOpen!: (e: Error) => void;
	let resolveTerminal!: () => void;
	let rejectTerminal!: (e: Error) => void;
	const opened = new Promise<void>((resolve, reject) => {
		resolveOpen = resolve;
		rejectOpen = reject;
	});
	const terminal = new Promise<void>((resolve, reject) => {
		resolveTerminal = resolve;
		rejectTerminal = reject;
	});

	await page.exposeFunction(pushFn, (frame: ProgressFrame) => {
		if (frame.job_id !== jobId) return; // hub broadcasts to all clients; keep only ours
		frames.push(frame);
		if ((ORGANIZE_TERMINAL_STATUSES as readonly string[]).includes(frame.status)) {
			resolveTerminal();
		}
	});
	await page.exposeFunction(openFn, () => resolveOpen());
	await page.exposeFunction(openFailFn, (msg: string) => rejectOpen(new Error(msg)));
	await page.exposeFunction(termFailFn, (msg: string) => rejectTerminal(new Error(msg)));

	await page.evaluate(
		(p: { push: string; open: string; openFail: string; termFail: string }) => {
			const url = location.origin.replace(/^http/, 'ws') + '/ws/progress';
			const ws = new WebSocket(url);
			(window as unknown as { __e2eWs?: WebSocket }).__e2eWs = ws;
			let didOpen = false;
			const w = window as unknown as Record<string, ((...a: unknown[]) => void) | undefined>;
			ws.onopen = () => {
				didOpen = true;
				w[p.open]?.();
			};
			ws.onerror = () => {
				if (!didOpen) w[p.openFail]?.('websocket onerror before open');
			};
			ws.onclose = (ev) => {
				if (!didOpen) {
					w[p.openFail]?.(
						`websocket closed before open (code=${ev.code} reason=${JSON.stringify(ev.reason)} clean=${ev.wasClean})`,
					);
				} else {
					// Mid-test close (e.g. backend torn down): fast-fail the
					// terminal promise instead of letting the caller hang on a
					// 20s safety-net timeout. A later close after the terminal
					// frame already arrived is a no-op (terminal is settled).
					w[p.termFail]?.(
						`websocket closed mid-test (code=${ev.code} reason=${JSON.stringify(ev.reason)} clean=${ev.wasClean})`,
					);
				}
			};
			ws.onmessage = (ev) => {
				try {
					const frame = JSON.parse(ev.data as string) as ProgressFrame;
					w[p.push]?.(frame);
				} catch {
					// ignore non-JSON frames (keepalive payloads)
				}
			};
			// Safety: if the socket never opens, fail the open promise fast.
			setTimeout(() => {
				if (!didOpen) w[p.openFail]?.('ws open timeout (5s)');
			}, 5_000);
		},
		{ push: pushFn, open: openFn, openFail: openFailFn, termFail: termFailFn },
	);

	// Wait for OPEN before returning (caller submits work only after subscribe).
	// On failure, close the orphaned browser socket before re-throwing — the
	// caller's `close()` cannot run because this function throws before returning.
	try {
		await opened;
	} catch (err) {
		await closeProgressWebSocket(page);
		throw err;
	}

	return {
		frames,
		terminal,
		close: () => closeProgressWebSocket(page),
	};
}

/** Close the browser-side WS opened by openProgressWebSocket (best-effort). */
async function closeProgressWebSocket(page: Page): Promise<void> {
	await page
		.evaluate(() => {
			const ws = (window as unknown as { __e2eWs?: WebSocket }).__e2eWs;
			// Close unconditionally: ws.close() is a no-op if already CLOSED, and
			// aborts an in-flight CONNECTING handshake. Guarding on readyState===OPEN
			// would orphan a CONNECTING socket if the 5s open-timeout fired while
			// the handshake was still pending (the Cycle-7 socket-leak finding).
			if (ws) ws.close();
		})
		.catch(() => {
			/* page may have navigated/closed; ignore */
		});
}
