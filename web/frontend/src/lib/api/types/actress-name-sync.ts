export const ACTRESS_NAME_SYNC_MAX_SELECTION = 500;
export const ACTRESS_NAME_SYNC_MAX_PAGE_SIZE = 100;
export const ACTRESS_NAME_SYNC_MAX_JOB_PAGE_SIZE = 32;
export interface ActressNameSyncStatus {
	enabled: boolean;
	available: boolean;
	code?: string;
	worker_problem_code?: string;
	reload_problem_code?: string;
	artifact_digest?: string;
	record_count: number;
	recovered: boolean;
	max_age_seconds: number;
	restart_required_for_config_changes: boolean;
}
export interface ActressNameSyncSource {
	source_key: string;
	source_id: string;
	japanese_name: string;
	aliases: string[];
	source_url: string;
	observed_at: string;
}
export interface ActressNameSyncChoice {
	actress_id: number;
	source_key: string;
}
export interface ActressNameSyncProposal {
	actress_id: number;
	source: ActressNameSyncSource;
	before_name: string;
	after_name: string;
}
export interface ActressNameSyncReview {
	review_id: string;
	expires_at: string;
	artifact_digest: string;
	max_age_seconds: number;
	proposals: ActressNameSyncProposal[];
}
export interface ActressNameSyncJob {
	id: string;
	snapshot_digest: string;
	created_at: string;
	cancelled: boolean;
	status: 'pending' | 'running' | 'blocked' | 'completed' | 'cancelled';
	counts: Record<string, number>;
}
export interface ActressNameSyncTask {
	id: number;
	actress_id: number;
	source_key: string;
	before_name: string;
	after_name: string;
	status:
		| 'pending'
		| 'leased'
		| 'applying'
		| 'applied'
		| 'unchanged'
		| 'skipped'
		| 'invalidated'
		| 'cancelled';
	reason?: string;
	problem_code?: string;
	retry_after_ms?: number;
	problem_attempts?: number;
	finished_at?: string;
}
export interface ActressNameSyncPageParams {
	offset?: number;
	limit?: number;
}
export interface ActressNameSyncSourcesParams extends ActressNameSyncPageParams {
	q?: string;
	artifact_digest?: string;
}
export interface ActressNameSyncJobsParams {
	cursor?: string;
	limit?: number;
}
export interface ActressNameSyncSourcePage {
	items: ActressNameSyncSource[];
	next_offset: number | null;
	artifact_digest: string;
}
export interface ActressNameSyncTaskPage {
	items: ActressNameSyncTask[];
	next_offset: number | null;
}
export interface ActressNameSyncJobPage {
	items: ActressNameSyncJob[];
	next_cursor?: string;
}
