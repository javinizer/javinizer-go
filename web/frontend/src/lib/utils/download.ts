import { isDesktopApp } from '$lib/utils/desktop';

export interface SaveFileResult {
	saved: boolean;
	path?: string;
}

interface SaveFileErrorBody {
	error?: string;
}

// saveJsonFile persists a JSON export. In a normal browser this is the usual
// Blob + anchor[download] click. The desktop webviews silently drop those
// downloads (Wails macOS wires no WKDownloadDelegate), so there the payload is
// streamed to POST /desktop/save-file?filename=<name>, which the desktop
// reverse proxy answers with a native save dialog and streams to disk without
// an in-memory copy (exports can reach hundreds of MB).
export async function saveJsonFile(filename: string, data: unknown): Promise<SaveFileResult> {
	const content = JSON.stringify(data, null, 2);

	if (isDesktopApp()) {
		const resp = await fetch(`/desktop/save-file?filename=${encodeURIComponent(filename)}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/octet-stream' },
			body: content,
		});
		if (!resp.ok) {
			let detail = `HTTP ${resp.status}`;
			try {
				const body = (await resp.json()) as SaveFileErrorBody;
				if (body.error) detail = body.error;
			} catch {
				// non-JSON error body: keep the HTTP-status detail
			}
			throw new Error(detail);
		}
		return (await resp.json()) as SaveFileResult;
	}

	const blob = new Blob([content], { type: 'application/json' });
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	document.body.removeChild(a);
	URL.revokeObjectURL(url);
	return { saved: true };
}
