<script lang="ts">
	import type { Status } from '#lib/api.ts';
	import { statusMeta } from '#lib/status.ts';
	import Icon from './Icon.svelte';

	let {
		status,
		label,
		size = 'md'
	}: { status: Status | string; label?: string; size?: 'sm' | 'md' | 'lg' } = $props();
	const meta = $derived(statusMeta(status));
</script>

<span class="badge {meta.tone} {size}" title={meta.description}>
	<Icon name={meta.icon} size={size === 'lg' ? 14 : 12} stroke={2.25} />
	<span>{label ?? meta.label}</span>
</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		height: 22px;
		padding: 0 8px 0 6px;
		border-radius: var(--r-pill);
		border: 1px solid;
		font-size: var(--text-xs);
		font-weight: 550;
		line-height: 1;
		white-space: nowrap;
	}
	.sm {
		height: 20px;
		padding: 0 7px 0 5px;
		font-size: var(--text-2xs);
	}
	.lg {
		height: 26px;
		padding: 0 10px 0 8px;
		font-size: var(--text-sm);
	}
	.ok {
		color: var(--ok);
		background: var(--ok-bg);
		border-color: var(--ok-border);
	}
	.warn {
		color: var(--warn);
		background: var(--warn-bg);
		border-color: var(--warn-border);
	}
	.bad {
		color: var(--bad);
		background: var(--bad-bg);
		border-color: var(--bad-border);
	}
	.unk {
		color: var(--unk);
		background: var(--unk-bg);
		border-color: var(--unk-border);
	}
</style>
