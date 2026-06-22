import { describe, it, expect, vi, beforeEach } from 'vitest';
import { listTokens, createToken, revokeToken, regenerateToken } from './tokens';
import { apiClient } from './client';

vi.mock('./client', () => ({
	apiClient: {
		request: vi.fn(),
	},
}));

const mockRequest = vi.mocked(apiClient.request);

beforeEach(() => {
	vi.clearAllMocks();
});

describe('tokens API client', () => {
	describe('listTokens', () => {
		it('calls GET /api/v1/tokens and returns typed response', async () => {
			const mockResponse = {
				tokens: [
					{
						id: 'abc123',
						token_prefix: 'abcd1234',
						name: 'test',
						created_at: '2026-01-01T00:00:00Z',
						last_used_at: null,
						revoked_at: null,
					},
				],
				count: 1,
			};
			mockRequest.mockResolvedValue(mockResponse);

			const result = await listTokens();

			expect(mockRequest).toHaveBeenCalledWith('/api/v1/tokens');
			expect(result).toEqual(mockResponse);
			expect(result.tokens).toHaveLength(1);
			expect(result.tokens[0].name).toBe('test');
		});

		it('handles error response', async () => {
			mockRequest.mockRejectedValue(new Error('Network error'));

			await expect(listTokens()).rejects.toThrow('Network error');
		});
	});

	describe('createToken', () => {
		it('calls POST /api/v1/tokens with name', async () => {
			const mockResponse = {
				token: 'jv_abcd1234567890',
				id: 'new-id',
				name: 'my-token',
				created_at: '2026-01-01T00:00:00Z',
			};
			mockRequest.mockResolvedValue(mockResponse);

			const result = await createToken('my-token');

			expect(mockRequest).toHaveBeenCalledWith('/api/v1/tokens', {
				method: 'POST',
				body: JSON.stringify({ name: 'my-token' }),
			});
			expect(result).toEqual(mockResponse);
		});

		it('sends empty name when no name provided', async () => {
			const mockResponse = {
				token: 'jv_abcd1234567890',
				id: 'new-id',
				name: '',
				created_at: '2026-01-01T00:00:00Z',
			};
			mockRequest.mockResolvedValue(mockResponse);

			await createToken();

			expect(mockRequest).toHaveBeenCalledWith('/api/v1/tokens', {
				method: 'POST',
				body: JSON.stringify({ name: '' }),
			});
		});

		it('handles error response', async () => {
			mockRequest.mockRejectedValue(new Error('Server error'));

			await expect(createToken('test')).rejects.toThrow('Server error');
		});
	});

	describe('revokeToken', () => {
		it('calls DELETE /api/v1/tokens/:id', async () => {
			mockRequest.mockResolvedValue(undefined);

			await revokeToken('token-id-123');

			expect(mockRequest).toHaveBeenCalledWith('/api/v1/tokens/token-id-123', {
				method: 'DELETE',
			});
		});
	});

	describe('regenerateToken', () => {
		it('calls POST /api/v1/tokens/:id/regenerate', async () => {
			const mockResponse = {
				token: 'jv_newtoken123456',
				id: 'existing-id',
				name: 'regenerated',
				created_at: '2026-01-01T00:00:00Z',
			};
			mockRequest.mockResolvedValue(mockResponse);

			const result = await regenerateToken('existing-id');

			expect(mockRequest).toHaveBeenCalledWith('/api/v1/tokens/existing-id/regenerate', {
				method: 'POST',
			});
			expect(result).toEqual(mockResponse);
			expect(result.token).toContain('jv_');
		});
	});
});
