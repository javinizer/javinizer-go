export function portalToBody(node: HTMLElement) {
	if (typeof document === 'undefined') {
		return {};
	}

	document.body.appendChild(node);

	return {
		destroy() {
			if (node.parentNode) {
				node.parentNode.removeChild(node);
			}
		},
	};
}
