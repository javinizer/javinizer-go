export function splitPath(currentPath: string): string[] {
	if (!currentPath) return [];
	const driveMatch = currentPath.match(/^([A-Za-z]:)[\\/]/);
	const stripped = driveMatch ? currentPath.slice(driveMatch[0].length) : currentPath;
	const parts = stripped.split(/[/\\]/).filter((p) => p);
	if (driveMatch) {
		return [driveMatch[1], ...parts];
	}
	return parts;
}

export function buildPathUp(currentPath: string, serverParentPath?: string): string {
	if (serverParentPath) {
		return serverParentPath;
	}
	const isWindows = /^[A-Za-z]:/.test(currentPath);
	const parts = splitPath(currentPath);
	parts.pop();
	if (parts.length === 0) return isWindows ? currentPath : '/';
	const sep = isWindows ? '\\' : '/';
	if (isWindows) {
		const drive = parts[0];
		const rest = parts.slice(1);
		return rest.length > 0 ? drive + sep + rest.join(sep) : drive + sep;
	}
	return sep + parts.join(sep);
}

export function buildBreadcrumbPath(currentPath: string, index: number): string {
	const parts = splitPath(currentPath);
	const selectedParts = parts.slice(0, index + 1);
	const isWindows = /^[A-Za-z]:/.test(currentPath);
	const sep = isWindows ? '\\' : '/';
	if (isWindows) {
		const drive = selectedParts[0];
		const rest = selectedParts.slice(1);
		return rest.length > 0 ? drive + sep + rest.join(sep) : drive + sep;
	}
	return sep + selectedParts.join(sep);
}

export function isRootPath(currentPath: string): boolean {
	if (!currentPath) return true;
	if (currentPath === '/') return true;
	if (/^[A-Za-z]:\\?$/.test(currentPath)) return true;
	return false;
}
