import { SvelteMap } from 'svelte/reactivity';

interface DialogButton {
	label: string;
	variant?: 'default' | 'destructive' | 'outline';
	value: string;
}

interface DialogConfig {
	title: string;
	message: string;
	buttons: DialogButton[];
	variant?: 'default' | 'danger';
}

interface ActiveDialog extends DialogConfig {
	id: string;
	resolve: (value: string) => void;
}

const dialogs = new SvelteMap<string, ActiveDialog>();
let nextId = 0;

function addDialog(config: DialogConfig): Promise<string> {
	const id = `dialog-${++nextId}`;
	return new Promise((resolve) => {
		dialogs.set(id, { ...config, id, resolve });
	});
}

function dismissDialog(id: string, value: string) {
	const dialog = dialogs.get(id);
	if (dialog) {
		dialog.resolve(value);
		dialogs.delete(id);
	}
}

export function confirmDialog(
	title: string,
	message: string,
	options?: { confirmLabel?: string; cancelLabel?: string; variant?: 'danger' }
): Promise<boolean> {
	return addDialog({
		title,
		message,
		variant: options?.variant,
		buttons: [
			{ label: options?.cancelLabel ?? 'Cancel', variant: 'outline', value: 'cancel' },
			{
				label: options?.confirmLabel ?? 'Confirm',
				variant: options?.variant === 'danger' ? 'destructive' : 'default',
				value: 'confirm'
			}
		]
	}).then((v) => v === 'confirm');
}

export function alertDialog(title: string, message: string): Promise<void> {
	return addDialog({
		title,
		message,
		buttons: [{ label: 'OK', variant: 'default', value: 'ok' }]
	}).then(() => {});
}

export { dialogs, dismissDialog };
