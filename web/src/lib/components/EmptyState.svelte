<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { IconName } from '$lib/icons';
	import Icon from './Icon.svelte';

	let {
		icon = 'info',
		title,
		children,
		actions,
		compact = false
	}: { icon?: IconName; title: string; children?: Snippet; actions?: Snippet; compact?: boolean } = $props();
</script>

<div class="empty" class:compact>
	<span class="glyph"><Icon name={icon} size={18} /></span>
	<p class="title">{title}</p>
	{#if children}<div class="text">{@render children()}</div>{/if}
	{#if actions}<div class="actions">{@render actions()}</div>{/if}
</div>

<style>
	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		padding: var(--s-16) var(--s-6);
	}
	.compact {
		padding: var(--s-10) var(--s-4);
	}
	.glyph {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		border-radius: var(--r);
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text-2);
		box-shadow: var(--shadow-sm);
		margin-bottom: var(--s-4);
	}
	.title {
		font-weight: 600;
		font-size: var(--text-md);
	}
	.text {
		margin-top: var(--s-2);
		max-width: 46ch;
		color: var(--text-3);
		font-size: var(--text-sm);
		line-height: var(--leading-loose);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--s-2);
		margin-top: var(--s-5);
	}
</style>
