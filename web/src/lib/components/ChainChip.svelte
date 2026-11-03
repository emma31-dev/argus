<script lang="ts">
	import { chainLabel } from '$lib/chain';

	/** Muted chain label shown next to every symbol: symbols repeat across chains. */
	let { id, chain, size = 'sm' }: { id: string; chain?: string; size?: 'sm' | 'md' } = $props();
	const label = $derived(chainLabel(id, chain));
</script>

<span class="chip {size}" title="Asset from {label}">{label}</span>

<style>
	.chip {
		display: inline-flex;
		align-items: center;
		height: 18px;
		padding: 0 6px;
		border-radius: var(--r-xs);
		background: var(--surface-3);
		color: var(--text-2);
		font-size: 10.5px;
		font-weight: 550;
		letter-spacing: 0.01em;
		white-space: nowrap;
		vertical-align: middle;
	}
	.md {
		height: 22px;
		padding: 0 8px;
		font-size: var(--text-xs);
	}
</style>
