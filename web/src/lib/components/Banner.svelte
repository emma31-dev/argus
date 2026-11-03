<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { IconName } from '$lib/icons';
	import Icon from './Icon.svelte';

	let {
		tone = 'info',
		icon,
		title,
		children,
		action
	}: {
		tone?: 'bad' | 'warn' | 'info';
		icon?: IconName;
		title: string;
		children?: Snippet;
		action?: Snippet;
	} = $props();
	const fallbackIcon: Record<string, IconName> = { bad: 'alert', warn: 'clock', info: 'info' };
</script>

<div class="banner {tone}" role={tone === 'info' ? 'status' : 'alert'}>
	<span class="icon"><Icon name={icon ?? fallbackIcon[tone]} size={16} stroke={2} /></span>
	<div class="body">
		<p class="title">{title}</p>
		{#if children}<div class="text">{@render children()}</div>{/if}
	</div>
	{#if action}<div class="action">{@render action()}</div>{/if}
</div>

<style>
	.banner {
		display: flex;
		align-items: flex-start;
		gap: var(--s-3);
		padding: var(--s-3) var(--s-4);
		border-radius: var(--r);
		border: 1px solid var(--b);
		background: var(--bgc);
	}
	.bad {
		--b: var(--bad-border);
		--bgc: var(--bad-bg);
		--c: var(--bad);
	}
	.warn {
		--b: var(--warn-border);
		--bgc: var(--warn-bg);
		--c: var(--warn);
	}
	.info {
		--b: var(--border);
		--bgc: var(--surface-2);
		--c: var(--text-2);
	}
	.icon {
		color: var(--c);
		padding-top: 2px;
	}
	.body {
		flex: 1;
		min-width: 0;
	}
	.title {
		font-weight: 600;
		color: var(--text);
		font-size: var(--text-sm);
	}
	.text {
		color: var(--text-2);
		font-size: var(--text-sm);
		margin-top: 2px;
	}
	.action {
		flex: none;
		align-self: center;
	}
	@media (max-width: 560px) {
		.banner {
			flex-wrap: wrap;
		}
		.action {
			flex-basis: 100%;
			padding-left: 28px;
		}
	}
</style>
