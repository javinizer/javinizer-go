import type {
	HealthResponse,
	ErrorResponse,
	AuthCredentialsRequest,
	AuthStatusResponse,
	VersionStatusResponse,
} from '../types';
import { browser } from '$app/environment';

const sessionStorageKey = 'javinizer_session';

function isDesktopApp(): boolean {
	if (!browser) return false;
	if (location.protocol === 'wails:') return true;
	return location.hostname === 'wails.localhost';
}

function readStoredSession(): string | null {
	if (!browser) return null;
	try {
		return localStorage.getItem(sessionStorageKey);
	} catch {
		return null;
	}
}

function writeStoredSession(id: string): void {
	if (!browser) return;
	try {
		localStorage.setItem(sessionStorageKey, id);
	} catch {
		// localStorage may be unavailable in private mode or sandboxed frames
	}
}

function clearStoredSession(): void {
	if (!browser) return;
	try {
		localStorage.removeItem(sessionStorageKey);
	} catch {
		// ignore
	}
}

// ApiError carries the structured ErrorResponse fields (code/params) so callers
// can translate known error codes via translateErrorCode before showing the
// message. It extends Error so existing `instanceof Error` checks keep working.
export class ApiError extends Error {
	code?: string;
	params?: Record<string, unknown> | null;
	status?: number;

	constructor(
		message: string,
		code?: string,
		params?: Record<string, unknown> | null,
		status?: number,
	) {
		super(message);
		this.name = 'ApiError';
		this.code = code;
		this.params = params;
		this.status = status;
	}
}

export const REQUEST_TIMEOUT_CODE = 'REQUEST_TIMEOUT';

export interface RequestOptions extends RequestInit {
	timeoutMs?: number;
}

interface RequestAbortControl {
	signal?: AbortSignal;
	timedOut: () => boolean;
	cleanup: () => void;
}

function createRequestAbortControl(options?: RequestOptions): RequestAbortControl {
	const timeoutMs = options?.timeoutMs;
	const callerSignal = options?.signal;
	if (!timeoutMs || !Number.isFinite(timeoutMs) || timeoutMs <= 0) {
		return { signal: callerSignal ?? undefined, timedOut: () => false, cleanup: () => undefined };
	}

	const controller = new AbortController();
	let timedOut = false;
	let timer: ReturnType<typeof setTimeout> | undefined;

	const abortFromCaller = () => controller.abort(callerSignal?.reason);
	if (callerSignal?.aborted) {
		abortFromCaller();
	} else {
		callerSignal?.addEventListener('abort', abortFromCaller, { once: true });
	}

	timer = setTimeout(() => {
		timedOut = true;
		controller.abort();
	}, timeoutMs);

	return {
		signal: controller.signal,
		timedOut: () => timedOut,
		cleanup: () => {
			if (timer !== undefined) clearTimeout(timer);
			callerSignal?.removeEventListener('abort', abortFromCaller);
		},
	};
}

function isRequestAbortError(error: unknown): boolean {
	return (
		(error instanceof Error && error.name === 'AbortError') ||
		(typeof DOMException !== 'undefined' &&
			error instanceof DOMException &&
			error.name === 'AbortError')
	);
}

function timeoutError(): ApiError {
	return new ApiError('Request timed out', REQUEST_TIMEOUT_CODE);
}

// Base client provides the shared request method that all sub-clients use.
export class BaseClient {
	protected baseURL: string;

	private static sessionID: string | null = null;

	static setSessionID(id: string | null) {
		if (id) {
			BaseClient.sessionID = id;
			writeStoredSession(id);
		} else {
			BaseClient.sessionID = null;
			clearStoredSession();
		}
	}

	static getSessionID(): string | null {
		if (BaseClient.sessionID) return BaseClient.sessionID;
		const stored = readStoredSession();
		if (stored) {
			BaseClient.sessionID = stored;
			return stored;
		}
		return null;
	}

	constructor(baseURL: string) {
		this.baseURL = baseURL;
	}

	public async request<T>(endpoint: string, options?: RequestOptions): Promise<T> {
		const url = `${this.baseURL}${endpoint}`;
		const fetchOptions = options ? { ...options } : {};
		delete fetchOptions.timeoutMs;
		const abortControl = createRequestAbortControl(options);

		try {
			const response = await fetch(url, {
				credentials: 'same-origin',
				...fetchOptions,
				...(abortControl.signal ? { signal: abortControl.signal } : {}),
				headers: {
					'Content-Type': 'application/json',
					...(BaseClient.getSessionID() ? { 'X-Session-ID': BaseClient.getSessionID()! } : {}),
					...fetchOptions.headers,
				},
			});

			if (!response.ok) {
				let error: ErrorResponse;
				try {
					error = await response.json();
				} catch (cause) {
					if (abortControl.timedOut() || isRequestAbortError(cause)) throw cause;
					error = {
						error: `HTTP ${response.status}: ${response.statusText}`,
					};
				}
				throw new ApiError(
					error.error || 'API request failed',
					error.code,
					error.params,
					response.status,
				);
			}

			const text = await response.text();
			if (!text || !text.trim()) return undefined as T;
			return JSON.parse(text) as T;
		} catch (error) {
			if (abortControl.timedOut()) throw timeoutError();
			throw error;
		} finally {
			abortControl.cleanup();
		}
	}
}

// Build API base URL dynamically from browser location.
// In production (Docker/deployed, CLI `web` subcommand) frontend and backend
// are same-origin, so we use '' (relative URLs). In dev mode with the Vite
// proxy, VITE_API_URL can point the browser at a different host/port.
//
// In the desktop app (Wails webview) the SPA loads from wails://wails.localhost
// and must use same-origin relative URLs so requests route through the embedded
// reverse proxy to the API server on its random localhost port. A dev .env may
// bake VITE_API_URL=http://localhost:8765 into the bundle; that would make the
// SPA fetch cross-origin (and to the wrong port — the desktop binary's config
// binds a random high port, not 8765), which WKWebView blocks with
// "Load failed". Force same-origin in the desktop context regardless of the
// baked-in env value.
//
// VITE_API_URL is honored ONLY in dev (import.meta.env.DEV). A production build
// with VITE_API_URL baked in would otherwise pin the API client to a fixed
// host:port that won't match the server's actual bind address — e.g. running
// the desktop binary's `web` subcommand (which reads the portable config's
// random port) against a bundle baked with :8765 produces "Failed to fetch".
export function getAPIBaseURL(): string {
	if (isDesktopApp()) return '';
	if (import.meta.env.DEV && import.meta.env.VITE_API_URL) {
		return import.meta.env.VITE_API_URL;
	}
	return '';
}

// AuthClient handles authentication endpoints.
export class AuthClient extends BaseClient {
	async getAuthStatus(): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/status');
	}

	async setupAuth(credentials: AuthCredentialsRequest): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/setup', {
			method: 'POST',
			body: JSON.stringify(credentials),
		});
	}

	async loginAuth(credentials: AuthCredentialsRequest): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/login', {
			method: 'POST',
			body: JSON.stringify(credentials),
		});
	}

	async logoutAuth(): Promise<{ message: string }> {
		return this.request<{ message: string }>('/api/v1/auth/logout', {
			method: 'POST',
		});
	}
}

// SystemClient handles health, version, and utility endpoints.
export class SystemClient extends BaseClient {
	async health(): Promise<HealthResponse> {
		return this.request<HealthResponse>('/health');
	}

	async getVersionStatus(): Promise<VersionStatusResponse> {
		return this.request<VersionStatusResponse>('/api/v1/version');
	}

	async checkVersion(): Promise<VersionStatusResponse> {
		return this.request<VersionStatusResponse>('/api/v1/version/check', { method: 'POST' });
	}

	getPreviewImageURL(imageURL: string): string {
		const url = `${this.baseURL}/api/v1/temp/image?url=${encodeURIComponent(imageURL)}`;
		return this.withSessionParam(url);
	}

	// withSessionParam appends ?session= to /api/v1/ URLs for the desktop app,
	// where WKWebView does not persist the session cookie against the direct
	// backend origin and <img>/WebSocket can't set the X-Session-ID header. In
	// the browser (dev proxy or same-origin prod), the session cookie
	// authenticates these requests natively, so this is a no-op there.
	withSessionParam(url: string): string {
		if (!isDesktopApp()) return url;
		if (!url.includes('/api/v1/')) return url;
		const session = BaseClient.getSessionID();
		if (!session) return url;
		const sep = url.includes('?') ? '&' : '?';
		return `${url}${sep}session=${encodeURIComponent(session)}`;
	}

	async getCurrentWorkingDirectory(): Promise<{ path: string }> {
		return this.request<{ path: string }>('/api/v1/cwd');
	}
}
