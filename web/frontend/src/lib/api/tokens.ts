import { apiClient } from '$lib/api/client';
import type { TokenListResponse, CreateTokenResponse } from '$lib/types/token';

export async function listTokens(): Promise<TokenListResponse> {
	return apiClient.request<TokenListResponse>('/api/v1/tokens');
}

export async function createToken(name?: string): Promise<CreateTokenResponse> {
	return apiClient.request<CreateTokenResponse>('/api/v1/tokens', {
		method: 'POST',
		body: JSON.stringify({ name: name ?? '' }),
	});
}

export async function revokeToken(id: string): Promise<void> {
	await apiClient.request(`/api/v1/tokens/${id}`, {
		method: 'DELETE',
	});
}

export async function regenerateToken(id: string): Promise<CreateTokenResponse> {
	return apiClient.request<CreateTokenResponse>(`/api/v1/tokens/${id}/regenerate`, {
		method: 'POST',
	});
}
