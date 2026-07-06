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

	public async request<T>(endpoint: string, options?: RequestInit): Promise<T> {
		const url = `${this.baseURL}${endpoint}`;
		const response = await fetch(url, {
			credentials: 'same-origin',
			...options,
			headers: {
				'Content-Type': 'application/json',
				...(BaseClient.getSessionID() ? { 'X-Session-ID': BaseClient.getSessionID()! } : {}),
				...options?.headers,
			},
		});

		if (!response.ok) {
			const error: ErrorResponse = await response.json().catch(() => ({
				error: `HTTP ${response.status}: ${response.statusText}`,
			}));
			throw new Error(error.error || 'API request failed');
		}

		const text = await response.text();
		if (!text || !text.trim()) return undefined as T;
		return JSON.parse(text) as T;
	}
}

// Build API base URL dynamically from browser location.
// In production (Docker/deployed), frontend and backend are same-origin, so we use ''
// In dev mode with Vite proxy, we also use '' (proxy handles forwarding to backend)
// VITE_API_URL can override this for custom setups.
export function getAPIBaseURL(): string {
	if (import.meta.env.VITE_API_URL) {
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
		if (!isDesktopApp()) return url;
		const session = BaseClient.getSessionID();
		return session ? `${url}&session=${encodeURIComponent(session)}` : url;
	}

	async getCurrentWorkingDirectory(): Promise<{ path: string }> {
		return this.request<{ path: string }>('/api/v1/cwd');
	}
}
