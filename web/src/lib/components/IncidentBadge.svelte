<script lang="ts">
	import Icon from './Icon.svelte';

	let { open }: { open: boolean } = $props();
</script>

<span class="ib" class:open>
	<Icon name={open ? 'alert' : 'check'} size={12} stroke={2.25} />
	{open ? 'Open' : 'Resolved'}
</span>

<style>
	.ib {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		height: 20px;
		padding: 0 7px 0 5px;
		border-radius: var(--r-pill);
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text-2);
		font-size: var(--text-2xs);
		font-weight: 550;
		white-space: nowrap;
	}
	.open {
		color: var(--bad);
		background: var(--bad-bg);
		border-color: var(--bad-border);
	}
</style>
