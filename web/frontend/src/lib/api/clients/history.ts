import type {
	HistoryListResponse,
	HistoryListParams,
	HistoryStats,
	DeleteHistoryBulkParams,
	DeleteHistoryBulkResponse,
	JobListResponse,
	JobListItem,
	OperationListResponse,
	RevertResultResponse,
	EventListResponse,
	EventListParams,
	EventStatsResponse,
	DeleteEventsParams,
	DeleteEventsResponse,
} from '../types';
import { BaseClient } from './common';

// HistoryClient handles history record queries, stats, and deletion.
export class HistoryClient extends BaseClient {
	async getHistory(params?: HistoryListParams): Promise<HistoryListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		if (params?.operation) queryParams.set('operation', params.operation);
		if (params?.status) queryParams.set('status', params.status);
		if (params?.movie_id) queryParams.set('movie_id', params.movie_id);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<HistoryListResponse>(`/api/v1/history${query}`);
	}

	async getHistoryStats(): Promise<HistoryStats> {
		return this.request<HistoryStats>('/api/v1/history/stats');
	}

	async deleteHistory(id: number): Promise<void> {
		await this.request(`/api/v1/history/${id}`, { method: 'DELETE' });
	}

	async deleteHistoryBulk(params: DeleteHistoryBulkParams): Promise<DeleteHistoryBulkResponse> {
		const queryParams = new URLSearchParams();
		if (params.older_than_days)
			queryParams.set('older_than_days', params.older_than_days.toString());
		if (params.movie_id) queryParams.set('movie_id', params.movie_id);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<DeleteHistoryBulkResponse>(`/api/v1/history${query}`, { method: 'DELETE' });
	}
}

// JobsClient handles the organized-jobs listing endpoint (separate from batch jobs).
export class JobsClient extends BaseClient {
	async listOrganizedJobs(params?: {
		status?: string;
		limit?: number;
		offset?: number;
	}): Promise<JobListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.status) queryParams.set('status', params.status);
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<JobListResponse>(`/api/v1/jobs${query}`);
	}

	async getJob(jobId: string): Promise<JobListItem> {
		return this.request<JobListItem>(`/api/v1/jobs/${jobId}`);
	}

	async getJobOperations(jobId: string): Promise<OperationListResponse> {
		return this.request<OperationListResponse>(`/api/v1/jobs/${jobId}/operations`);
	}

	async revertBatchJob(jobId: string): Promise<RevertResultResponse> {
		return this.request<RevertResultResponse>(`/api/v1/jobs/${jobId}/revert`, {
			method: 'POST',
		});
	}

	async revertJobOperation(jobId: string, movieId: string): Promise<RevertResultResponse> {
		return this.request<RevertResultResponse>(
			`/api/v1/jobs/${jobId}/operations/${movieId}/revert`,
			{
				method: 'POST',
			},
		);
	}
}

// EventsClient handles event log queries, stats, and deletion.
export class EventsClient extends BaseClient {
	async listEvents(params?: EventListParams): Promise<EventListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.type) queryParams.set('type', params.type);
		if (params?.severity) queryParams.set('severity', params.severity);
		if (params?.source) queryParams.set('source', params.source);
		if (params?.start) queryParams.set('start', params.start);
		if (params?.end) queryParams.set('end', params.end);
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<EventListResponse>(`/api/v1/events${query}`);
	}

	async getEventStats(): Promise<EventStatsResponse> {
		return this.request<EventStatsResponse>('/api/v1/events/stats');
	}

	async deleteEvents(params: DeleteEventsParams): Promise<DeleteEventsResponse> {
		const queryParams = new URLSearchParams();
		queryParams.set('older_than_days', params.older_than_days.toString());
		return this.request<DeleteEventsResponse>(`/api/v1/events?${queryParams}`, {
			method: 'DELETE',
		});
	}
}
