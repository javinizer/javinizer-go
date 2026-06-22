/**
 * Shared types for full-stack Playwright E2E specs.
 *
 * These map 1:1 to the Go backend's JSON contract in
 * internal/api/contracts/batch_types.go — DO NOT invent fields here. If the
 * Go struct grows a new field, update both files together so spec
 * assertions can refer to it without `as any`.
 *
 * Kept in a types-only module so specs + helpers import without dragging in
 * Playwright runtime side-effects (purely ergonomic / IDE-navigation).
 */

/** GET /api/v1/batch/:id?include_data=true response. */
export interface BatchJobResponse {
	id: string;
	status: JobStatus;
	total_files: number;
	completed: number;
	failed: number;
	excluded: Record<string, boolean>;
	progress: number;
	destination: string;
	files?: string[];
	results: Record<string, FileResult>;
	started_at: string;
	completed_at?: string;
	update: boolean;
}

/** Terminal vs in-flight job statuses. Mirrors internal/worker job state. */
export type JobStatus =
	| 'pending'
	| 'running'
	| 'completed'
	| 'failed'
	| 'cancelled'
	| 'organized'
	| 'reverted';

/** Per-file result inside BatchJobResponse. */
export interface FileResult {
	result_id: string;
	file_path: string;
	movie_id: string;
	is_multi_part: boolean;
	part_number: number;
	part_suffix: string;
	status: string;
	error?: string;
	field_sources?: Record<string, string>;
	actress_sources?: Record<string, string>;
	movie?: MovieSummary;
	started_at: string;
	ended_at?: string;
}

/** Narrowed view of models.Movie rendered in the API response. Add fields as
 * specs assert them — keep this intentionally small so the contract surface
 * stays explicit at the seam where the API + frontend meet. */
export interface MovieSummary {
	id: string;
	title?: string;
	poster_url?: string;
	cover_url?: string;
}

/** Full Movie payload from GET /api/v1/movies/:id (under body.movie).
 * Maps to the MovieResponse contract in internal/api/contracts. */
export interface MovieDetail {
	id: string;
	code: string;
	title: string;
	display_title: string;
	maker?: string;
	label?: string;
	director?: string;
	poster_url: string;
	cover_url: string;
	actresses: Array<{ first_name: string; last_name: string }>;
	genres: Array<{ name: string }>;
}

/** GET /api/v1/movies response shape — { movies: MovieDetail[], total: number }. */
export interface MoviesListResponse {
	movies: MovieDetail[];
	total: number;
}

/** POST /api/v1/batch/:jobId/results/:resultId/preview response. */
export interface OrganizePreviewResponse {
	folder_name: string;
	file_name: string;
	subfolder_path?: string;
	full_path: string;
	video_files?: string[];
	nfo_path?: string;
	nfo_paths?: string[];
	poster_path: string;
	fanart_path: string;
	extrafanart_path: string;
	trailer_path: string;
	screenshots?: string[];
	source_path?: string;
	operation_mode?: string;
}

/** Common shape returned by /api/v1/auth/login. */
export interface AuthLoginResponse {
	initialized: boolean;
	authenticated: boolean;
	username?: string;
}

/** GET /api/v1/jobs/:id/operations response — list of BatchFileOperation records. */
export interface OperationItem {
	id: number;
	movie_id: string;
	original_path: string;
	new_path: string;
	operation_type: string;
	revert_status: string;
	reverted_at?: string;
	in_place_renamed: boolean;
	created_at: string;
}

/** GET /api/v1/jobs/:id/operations response envelope. */
export interface OperationListResponse {
	job_id: string;
	job_status: JobStatus | string;
	operations: OperationItem[];
	total: number;
}

/** Submit-scrape options accepted by submitScrape. */
export interface SubmitScrapeOptions {
	files: string[];
	selectedScrapers?: string[];
	destination?: string;
	operationMode?: string;
	/** When true, re-scrape with overwrite-existing semantics (UPSERT). */
	update?: boolean;
}

/** Options for waitForJobCompletion. */
export interface WaitForJobOptions {
	/** Hard timeout in ms. Defaults to 30s. */
	timeoutMs?: number;
	/** If set, throw if the job's terminal status is not this. */
	expectStatus?: JobStatus;
}
