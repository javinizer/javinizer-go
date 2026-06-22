export type ScraperProxyMode = 'direct' | 'inherit' | 'specific';

export interface ProxyConfig {
	enabled?: boolean;
	profile?: string;
	profiles?: Record<string, ProxyProfile>;
	default_profile?: string;
}

export interface ScraperProxyOverride {
	enabled?: boolean;
	profile?: string;
}

export interface ProxyProfile {
	url: string;
	username?: string;
	password?: string;
}

export interface GlobalProxyConfig {
	enabled: boolean;
	default_profile?: string;
	profiles?: Record<string, ProxyProfile>;
}

/**
 * Determine proxy mode for a scraper based on global and scraper-specific config.
 * This must match backend ResolveScraperProxyMode logic exactly.
 */
export function getScraperProxyMode(
	globalEnabled: boolean,
	override?: ScraperProxyOverride,
): ScraperProxyMode {
	// Circuit breaker: global proxy disabled = all scrapers direct
	if (!globalEnabled) {
		return 'direct';
	}

	// No proxy config = inherit from global (nil override semantics match backend)
	if (!override) {
		return 'inherit';
	}

	// enabled must be explicitly true for inherit/specific modes
	// enabled=false or enabled=undefined means direct (matches backend default)
	if (override.enabled !== true) {
		return 'direct';
	}

	// Enabled=true, check for profile
	if ((override.profile ?? '').trim() !== '') {
		return 'specific';
	}

	return 'inherit';
}

/**
 * Check if proxy config is dirty compared to baseline.
 */
export function isProxyConfigDirty(current: ProxyConfig | undefined, baseline: string): boolean {
	const currentStr = JSON.stringify(current);
	return currentStr !== baseline;
}

/**
 * Check if a test result is valid (exists, success, not expired, config unchanged).
 */
export interface TestResult {
	success: boolean;
	timestamp: number;
	message?: string;
	configSnapshot?: string;
	verificationToken?: string;
	tokenExpiresAt?: number;
}

export function isTestValid(
	result: TestResult | null | undefined,
	currentConfig: unknown,
	validityMs: number = 5 * 60 * 1000,
): boolean {
	if (!result) return false;
	if (!result.success) return false;

	const age = Date.now() - result.timestamp;
	if (age >= validityMs) return false;

	if (result.configSnapshot) {
		const currentStr = JSON.stringify(currentConfig);
		if (currentStr !== result.configSnapshot) return false;
	}

	return true;
}
