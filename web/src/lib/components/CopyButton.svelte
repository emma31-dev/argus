<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		value,
		label = 'Copy',
		showLabel = false
	}: { value: string; label?: string; showLabel?: boolean } = $props();
	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy(e: MouseEvent) {
		e.stopPropagation();
		e.preventDefault();
		let ok = false;
		try {
			await navigator.clipboard.writeText(value);
			ok = true;
		} catch {
			try {
				const ta = document.createElement('textarea');
				ta.value = value;
				ta.setAttribute('readonly', '');
				ta.style.position = 'fixed';
				ta.style.opacity = '0';
				document.body.appendChild(ta);
				ta.select();
				ok = document.execCommand('copy');
				ta.remove();
			} catch {
				ok = false;
			}
		}
		if (ok) {
			copied = true;
			clearTimeout(timer);
			timer = setTimeout(() => (copied = false), 1400);
		}
	}
</script>

<button
	type="button"
	class="copy"
	class:labelled={showLabel}
	class:copied
	onclick={copy}
	aria-label={copied ? 'Copied' : label}
	title={copied ? 'Copied' : label}
>
	<Icon name={copied ? 'check' : 'copy'} size={13} stroke={2} />
	{#if showLabel}<span>{copied ? 'Copied' : label}</span>{/if}
	<span class="sr-only" aria-live="polite">{copied ? 'Copied to clipboard' : ''}</span>
</button>

<style>
	.copy {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		flex: none;
		width: 24px;
		height: 24px;
		border-radius: var(--r-xs);
		color: var(--text-3);
		transition:
			color var(--dur) var(--ease),
			background var(--dur) var(--ease);
	}
	.labelled {
		width: auto;
		padding: 0 8px;
		font-size: var(--text-xs);
		font-weight: 500;
	}
	.copy:hover {
		color: var(--text);
		background: var(--surface-3);
	}
	.copied {
		color: var(--ok);
	}
</style>
