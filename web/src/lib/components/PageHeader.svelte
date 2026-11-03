<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		eyebrow,
		title,
		children,
		aside
	}: { eyebrow?: string; title: string; children?: Snippet; aside?: Snippet } = $props();
</script>

<header class="ph">
	<div class="main">
		{#if eyebrow}<p class="eyebrow">{eyebrow}</p>{/if}
		<h1>{title}</h1>
		{#if children}<div class="lede">{@render children()}</div>{/if}
	</div>
	{#if aside}<div class="aside">{@render aside()}</div>{/if}
</header>

<style>
	.ph {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: var(--s-6);
		padding-top: var(--s-12);
		padding-bottom: var(--s-8);
		flex-wrap: wrap;
	}
	.main {
		min-width: 0;
		flex: 1 1 520px;
	}
	h1 {
		margin-top: var(--s-2);
		font-size: var(--text-3xl);
		font-weight: 640;
		letter-spacing: -0.032em;
	}
	.lede {
		margin-top: var(--s-3);
		color: var(--text-2);
		font-size: var(--text-md);
		line-height: var(--leading-loose);
		max-width: 68ch;
	}
	.aside {
		flex: none;
		display: flex;
		gap: var(--s-2);
		flex-wrap: wrap;
	}
	@media (max-width: 560px) {
		.ph {
			padding-top: var(--s-8);
			padding-bottom: var(--s-6);
		}
		h1 {
			font-size: var(--text-2xl);
		}
	}
</style>
