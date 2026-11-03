<script lang="ts">
	import { formatInteger } from '$lib/format';
	import { explorer } from '$lib/links';

	let { height, prefix = '' }: { height: number | null | undefined; prefix?: string } = $props();
</script>

{#if height === null || height === undefined || height === 0}
	<span class="mono muted">—</span>
{:else}
	<a
		class="block mono num"
		href={explorer.block(height)}
		target="_blank"
		rel="noopener noreferrer"
		title="Block {height} on nearblocks.io"
		onclick={(e) => e.stopPropagation()}
		>{prefix}{formatInteger(height)}<svg viewBox="0 0 12 12" aria-hidden="true"
			><path d="M4.5 3h4.5v4.5M9 3 3.5 8.5" /></svg
		></a
	>
{/if}

<style>
	.block {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		color: inherit;
		text-decoration: underline;
		text-decoration-color: var(--border-strong);
		text-underline-offset: 3px;
		white-space: nowrap;
	}
	.block:hover {
		text-decoration-color: currentColor;
		color: var(--text);
	}
	svg {
		width: 10px;
		height: 10px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.4;
		opacity: 0.55;
	}
</style>
