import * as m from '$lib/paraglide/messages';

type Params = Record<string, unknown> | null;

function str(params: Params, key: string): string {
	const v = params?.[key];
	return v == null ? '' : String(v);
}

function num(params: Params, key: string): number {
	const v = params?.[key];
	const n = typeof v === 'number' ? v : Number(v);
	return Number.isFinite(n) ? n : 0;
}

const ERROR_CODE_MAP: Record<string, (p: Params) => string> = {
	AUTH_INVALID_CREDENTIALS: () => m.error_auth_invalid_credentials(),
	AUTH_UNAUTHORIZED: () => m.error_auth_unauthorized(),
	AUTH_USER_EXISTS: () => m.error_auth_user_exists(),
	PATH_NOT_EXIST: (p) => m.error_path_not_found({ path: str(p, 'path') }),
	PATH_INVALID: (p) => m.error_path_invalid({ path: str(p, 'path') }),
	CONFIG_INVALID: (p) => m.error_config_invalid({ field: str(p, 'field') }),
	JOB_NOT_FOUND: (p) => m.error_job_not_found({ job_id: str(p, 'job_id') })
};

const PROGRESS_CODE_MAP: Record<string, (p: Params) => string> = {
	SCRAPE_STARTED: (p) => m.progress_scrape_started({ movie_id: str(p, 'movie_id') }),
	SCRAPE_SUCCEEDED: (p) => m.progress_scrape_succeeded({ movie_id: str(p, 'movie_id') }),
	SCRAPE_FAILED: (p) =>
		m.progress_scrape_failed({ movie_id: str(p, 'movie_id'), error: str(p, 'error') }),
	BATCH_COMPLETED: (p) => m.progress_batch_completed({ count: num(p, 'count'), failed: num(p, 'failed') })
};

export function translateErrorCode(
	code: string,
	params: Params,
	fallback: string
): string {
	const fn = ERROR_CODE_MAP[code];
	if (!fn) return fallback;
	try {
		const result = fn(params);
		return result || fallback;
	} catch {
		return fallback;
	}
}

export function translateProgressMessage(
	code: string,
	args: Params,
	fallback: string
): string {
	const fn = PROGRESS_CODE_MAP[code];
	if (!fn) return fallback;
	try {
		const result = fn(args);
		return result || fallback;
	} catch {
		return fallback;
	}
}
