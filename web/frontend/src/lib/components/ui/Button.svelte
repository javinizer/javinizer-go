<script lang="ts">
	import { cn } from '$lib/utils';

	interface Props {
		variant?: 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link';
		size?: 'default' | 'sm' | 'lg' | 'icon';
		disabled?: boolean;
		type?: 'button' | 'submit' | 'reset';
		class?: string;
		onclick?: () => void;
		children?: import('svelte').Snippet;
		title?: string;
		'aria-label'?: string;
		'aria-pressed'?: boolean;
	}

	let {
		variant = 'default',
		size = 'default',
		disabled = false,
		type = 'button',
		class: className,
		onclick,
		children,
		title,
		'aria-label': ariaLabel,
		'aria-pressed': ariaPressed
	}: Props = $props();

	const variants = {
		default: 'bg-primary text-primary-foreground hover:bg-primary/90',
		destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
		outline: 'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
		secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
		ghost: 'hover:bg-accent hover:text-accent-foreground',
		link: 'text-primary underline-offset-4 hover:underline'
	};

	const sizes = {
		default: 'h-10 px-4 py-2',
		sm: 'h-9 rounded-md px-3',
		lg: 'h-11 rounded-md px-8',
		icon: 'h-10 w-10'
	};

</script>

<button
	{type}
	{disabled}
	{title}
	aria-label={ariaLabel}
	{...(ariaPressed !== undefined ? { 'aria-pressed': ariaPressed } : {})}
	onclick={onclick}
	class={cn(
		'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50',
		variants[variant],
		sizes[size],
		className
	)}
>
	{@render children?.()}
</button>
