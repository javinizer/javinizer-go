import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../client';
import { BaseClient } from './common';

afterEach(() => vi.restoreAllMocks());
it('forwards return-time timeout through the facade without changing ordinary Browse', async () => {
	const request = vi.spyOn(BaseClient.prototype, 'request').mockResolvedValue({ current_path: '/source', items: [] });
	await apiClient.browse({ path: '/source', scope: 'operation' }, { timeoutMs: 5000 });
	expect(request).toHaveBeenLastCalledWith('/api/v1/browse', {
		method: 'POST', body: JSON.stringify({ path: '/source', scope: 'operation' }), timeoutMs: 5000,
	});
	await apiClient.browse({ path: '/other', scope: 'operation' });
	expect(request).toHaveBeenLastCalledWith('/api/v1/browse', {
		method: 'POST', body: JSON.stringify({ path: '/other', scope: 'operation' }),
	});
});
