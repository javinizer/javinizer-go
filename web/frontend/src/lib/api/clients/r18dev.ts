import type { DumpStatus, DumpSearchResult } from '../types';
import { BaseClient } from './common';

export class R18DevClient extends BaseClient {
	async getDumpStatus(): Promise<DumpStatus> {
		return this.request<DumpStatus>('/api/v1/r18dev/dump/status');
	}

	async downloadDump(updateOnly = false): Promise<void> {
		await this.request<void>(`/api/v1/r18dev/dump/${updateOnly ? 'update' : 'download'}`, {
			method: 'POST',
		});
	}

	async searchDump(query: string): Promise<DumpSearchResult> {
		const params = new URLSearchParams({ q: query });
		return this.request<DumpSearchResult>(`/api/v1/r18dev/dump/search?${params.toString()}`);
	}

	async clearDump(): Promise<void> {
		await this.request<void>('/api/v1/r18dev/dump', { method: 'DELETE' });
	}
}
