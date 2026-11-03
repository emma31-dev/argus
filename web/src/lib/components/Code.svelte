<script lang="ts">
	import CopyButton from './CopyButton.svelte';

	let { code, label, lang = '' }: { code: string; label?: string; lang?: string } = $props();
</script>

<figure class="code">
	{#if label}
		<figcaption>
			<span>{label}</span>
			{#if lang}<span class="lang">{lang}</span>{/if}
		</figcaption>
	{/if}
	<div class="wrap">
		<pre><code>{code}</code></pre>
		<span class="cp"><CopyButton value={code} label="Copy code" /></span>
	</div>
</figure>

<style>
	.code {
		border: 1px solid var(--border);
		border-radius: var(--r);
		background: var(--surface-2);
		overflow: hidden;
		min-width: 0;
	}
	figcaption {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 7px 12px;
		border-bottom: 1px solid var(--border);
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
		background: var(--surface);
	}
	.lang {
		font-family: var(--font-mono);
		color: var(--text-3);
		font-size: var(--text-2xs);
	}
	.wrap {
		position: relative;
	}
	pre {
		margin: 0;
		padding: 12px 40px 12px 14px;
		overflow-x: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 12.5px;
		line-height: 1.65;
		color: var(--text);
		tab-size: 2;
	}
	code {
		font-size: inherit;
	}
	.cp {
		position: absolute;
		top: 6px;
		right: 6px;
	}
</style>
