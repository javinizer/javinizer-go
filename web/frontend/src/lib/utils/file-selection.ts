import type { FileInfo } from '$lib/api/types';

/**
 * Drop missing direct-child file selections after listing a directory, leaving
 * selections from other directories and nested scans untouched. Keep the existing
 * textual path comparison: trim trailing separators, but do not canonicalize
 * roots, separator styles, or case.
 */
export function pruneStaleFileSelections(
	selectedFiles: readonly string[],
	currentDirectory: string,
	items: readonly Pick<FileInfo, 'path' | 'is_dir'>[],
): string[] {
	const presentPaths = new Set(items.filter((item) => !item.is_dir).map((item) => item.path));
	const normalizeDirectory = (directory: string): string =>
		directory.replace(/[\\/]+$/, '') || directory;
	return selectedFiles.filter((path) => {
		const parent = path.substring(0, Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')));
		return (
			normalizeDirectory(parent) !== normalizeDirectory(currentDirectory) || presentPaths.has(path)
		);
	});
}
