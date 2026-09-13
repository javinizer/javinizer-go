import type {
	ActressListParams,
	ActressListResponse,
	ActressUpsertRequest,
	Actress,
	ActressMergePreviewRequest,
	ActressMergePreviewResponse,
	ActressMergeRequest,
	ActressMergeResponse,
	ActressesImportRequest,
	ImportResponse,
	ActressAliasGroup,
	CandidateListResponse,
	CreditCollision,
	CollisionResolveRequest,
	CollisionResolution,
} from '../types';
import { BaseClient } from './common';

// ActressClient handles actress CRUD, merge, search, import, and export.
export class ActressClient extends BaseClient {
	async listActresses(params?: ActressListParams): Promise<ActressListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		if (params?.q) queryParams.set('q', params.q);
		if (params?.sort_by) queryParams.set('sort_by', params.sort_by);
		if (params?.sort_order) queryParams.set('sort_order', params.sort_order);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<ActressListResponse>(`/api/v1/actresses${query}`);
	}

	async getActress(id: number): Promise<Actress> {
		return this.request<Actress>(`/api/v1/actresses/${id}`);
	}

	async createActress(request: ActressUpsertRequest): Promise<Actress> {
		return this.request<Actress>('/api/v1/actresses', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async updateActress(id: number, request: ActressUpsertRequest): Promise<Actress> {
		return this.request<Actress>(`/api/v1/actresses/${id}`, {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}

	async deleteActress(id: number): Promise<void> {
		await this.request(`/api/v1/actresses/${id}`, { method: 'DELETE' });
	}

	async previewActressMerge(
		request: ActressMergePreviewRequest,
	): Promise<ActressMergePreviewResponse> {
		return this.request<ActressMergePreviewResponse>('/api/v1/actresses/merge/preview', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async mergeActresses(request: ActressMergeRequest): Promise<ActressMergeResponse> {
		return this.request<ActressMergeResponse>('/api/v1/actresses/merge', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async exportActresses(): Promise<Actress[]> {
		return this.request<Actress[]>('/api/v1/actresses/export', { method: 'GET' });
	}

	async importActresses(request: ActressesImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/actresses/import', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async listCandidates(limit = 50, offset = 0): Promise<CandidateListResponse> {
		const query = new URLSearchParams({ limit: limit.toString(), offset: offset.toString() });
		return this.request<CandidateListResponse>(`/api/v1/actresses/candidates?${query}`);
	}

	async promoteCandidate(
		id: number,
		request: Partial<ActressUpsertRequest> = {},
	): Promise<Actress> {
		return this.request<Actress>(`/api/v1/actresses/candidates/${id}/promote`, {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async listCollisions(movieId: string): Promise<{ collisions: CreditCollision[] }> {
		const query = new URLSearchParams({ movie_id: movieId });
		return this.request<{ collisions: CreditCollision[] }>(
			`/api/v1/actresses/collisions?${query}`,
		);
	}

	async resolveCollision(id: number, request: CollisionResolveRequest): Promise<{ resolved: boolean; remaining_open: number }> {
		return this.request<{ resolved: boolean; remaining_open: number }>(
			`/api/v1/actresses/collisions/${id}/resolve`,
			{
				method: 'POST',
				body: JSON.stringify(request),
			},
		);
	}

	async updateCreditOverride(
		creditId: number,
		overrideName: string,
		userOverride: boolean,
	): Promise<void> {
		await this.request(`/api/v1/actresses/credits/${creditId}/override`, {
			method: 'POST',
			body: JSON.stringify({ override_name: overrideName, user_override: userOverride }),
		});
	}

	async suppressCredit(creditId: number, suppressed: boolean): Promise<void> {
		await this.request(`/api/v1/actresses/credits/${creditId}/suppress`, {
			method: 'POST',
			body: JSON.stringify({ suppressed }),
		});
	}

	async getAliasGroup(name: string): Promise<ActressAliasGroup> {
		const query = new URLSearchParams({ name });
		return this.request<ActressAliasGroup>(`/api/v1/actresses/alias-group?${query}`);
	}
}
