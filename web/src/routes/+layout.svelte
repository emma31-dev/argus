<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/geist-mono';
	import '$lib/styles/tokens.css';
	import '$lib/styles/base.css';

	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { base } from '$app/paths';
	import Logo from '$lib/components/Logo.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import GithubMark from '$lib/components/GithubMark.svelte';
	import StatusDot from '$lib/components/StatusDot.svelte';
	import { theme } from '$lib/theme.svelte';
	import { monitor } from '$lib/monitor.svelte';
	import { poll } from '$lib/live.svelte';
	import { formatInteger } from '$lib/format';
	import { href, REPO_URL } from '$lib/links';
	import { statusMeta } from '$lib/status';

	let { children } = $props();

	const nav = [
		{ href: href.home(), label: 'Overview', match: (p: string) => p === '/' || p.startsWith('/token') },
		{ href: href.incidents(), label: 'Incidents', match: (p: string) => p.startsWith('/incident') },
		{ href: href.replay(), label: 'Replay', match: (p: string) => p.startsWith('/replay') },
		{ href: href.how(), label: 'How it works', match: (p: string) => p.startsWith('/how-it-works') },
		{ href: href.api(), label: 'API', match: (p: string) => p.startsWith('/api') }
	];

	const path = $derived(page.url.pathname.slice(base.length) || '/');
	const st = $derived(monitor.status.data);

	onMount(() => {
		theme.init();
		return poll(() => monitor.status.load(), () => monitor.intervalMs);
	});
</script>

<a class="skip" href="#main">Skip to content</a>

{#if st?.version?.includes('mock')}
	<div class="synthetic" role="note">
		Synthetic design data from the local mock API (dev/mock-api.mjs). These are not real measurements.
	</div>
{/if}

<header class="top">
	<div class="container bar">
		<a href={href.home()} class="brand" aria-label="Argus home"><Logo /></a>
		<nav aria-label="Primary" class="nav">
			{#each nav as item (item.href)}
				<a href={item.href} class:active={item.match(path)} aria-current={item.match(path) ? 'page' : undefined}
					>{item.label}</a
				>
			{/each}
		</nav>
		<div class="right">
			{#if st && !monitor.offline}
				<a href={href.home()} class="live" title="Overall status: {statusMeta(st.status).label}">
					<StatusDot status={monitor.stale ? 'amber' : st.status} pulse={!monitor.stale} size={7} />
					<span class="live-label">{monitor.stale ? 'Stale' : 'Live'}</span>
					<span class="live-block mono num">#{formatInteger(st.last_block)}</span>
				</a>
			{:else if monitor.status.settled && monitor.offline}
				<span class="live off"><StatusDot status="unknown" size={7} /><span class="live-label">Offline</span></span>
			{/if}
			<a class="icon-btn" href={REPO_URL} target="_blank" rel="noopener noreferrer" aria-label="Source on GitHub" title="Source on GitHub"
				><GithubMark size={16} /></a
			>
			<button
				class="icon-btn"
				type="button"
				onclick={() => theme.toggle()}
				aria-label={theme.resolved === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
				title={theme.resolved === 'dark' ? 'Light theme' : 'Dark theme'}
			>
				<Icon name={theme.resolved === 'dark' ? 'sun' : 'moon'} size={16} />
			</button>
		</div>
	</div>
	<nav aria-label="Primary" class="container nav-mobile">
		{#each nav as item (item.href)}
			<a href={item.href} class:active={item.match(path)} aria-current={item.match(path) ? 'page' : undefined}
				>{item.label}</a
			>
		{/each}
	</nav>
</header>

<main id="main">
	{@render children()}
</main>

<footer class="foot">
	<div class="container foot-in">
		<div class="foot-brand">
			<Logo size={18} />
			<p>Independent monitor. Not affiliated with NEAR Intents.</p>
		</div>
		<div class="foot-links">
			<a href={href.how()}>Methodology</a>
			<a href={href.api()}>API</a>
			<a href={REPO_URL} target="_blank" rel="noopener noreferrer">Source</a>
			{#if st?.version}<span class="mono muted">v{st.version}</span>{/if}
		</div>
	</div>
</footer>

<style>
	.skip {
		position: absolute;
		left: -9999px;
		top: 8px;
		z-index: 100;
		padding: 8px 12px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--r-sm);
	}
	.skip:focus {
		left: 8px;
	}

	.synthetic {
		padding: 6px var(--gutter);
		text-align: center;
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-inverse);
		background: repeating-linear-gradient(-45deg, var(--text) 0 10px, var(--text-2) 10px 20px);
	}
	.top {
		position: sticky;
		top: 0;
		z-index: 50;
		background: var(--overlay);
		backdrop-filter: saturate(1.6) blur(12px);
		-webkit-backdrop-filter: saturate(1.6) blur(12px);
		border-bottom: 1px solid var(--border);
	}
	.bar {
		display: flex;
		align-items: center;
		gap: var(--s-8);
		height: var(--nav-h);
	}
	.brand {
		flex: none;
		border-radius: var(--r-sm);
	}
	.nav {
		display: flex;
		gap: 2px;
		flex: 1;
		min-width: 0;
	}
	.nav a,
	.nav-mobile a {
		display: inline-flex;
		align-items: center;
		height: 30px;
		padding: 0 10px;
		border-radius: var(--r-sm);
		color: var(--text-2);
		font-size: var(--text-sm);
		font-weight: 500;
		white-space: nowrap;
		transition:
			color var(--dur) var(--ease),
			background var(--dur) var(--ease);
	}
	.nav a:hover {
		color: var(--text);
		background: var(--surface-3);
	}
	.nav a.active {
		color: var(--text);
		background: var(--surface-3);
	}
	.right {
		display: flex;
		align-items: center;
		gap: var(--s-1);
		margin-left: auto;
	}
	.live {
		display: inline-flex;
		align-items: center;
		gap: 7px;
		height: 28px;
		padding: 0 10px;
		margin-right: var(--s-2);
		border: 1px solid var(--border);
		border-radius: var(--r-pill);
		background: var(--surface);
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
	}
	.live-block {
		color: var(--text-3);
		font-size: 11.5px;
	}
	.icon-btn {
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border-radius: var(--r-sm);
		color: var(--text-2);
		transition:
			color var(--dur) var(--ease),
			background var(--dur) var(--ease);
	}
	.icon-btn:hover {
		color: var(--text);
		background: var(--surface-3);
	}
	.nav-mobile {
		display: none;
	}

	@media (max-width: 860px) {
		.nav {
			display: none;
		}
		.nav-mobile {
			display: flex;
			gap: 2px;
			overflow-x: auto;
			scrollbar-width: none;
			padding-bottom: 8px;
			margin-top: -4px;
		}
		.nav-mobile::-webkit-scrollbar {
			display: none;
		}
		.nav-mobile a {
			height: 28px;
			padding: 0 10px;
		}
		.nav-mobile a:first-child {
			margin-left: -10px;
		}
		.nav-mobile a.active {
			color: var(--text);
			background: var(--surface-3);
		}
		.bar {
			height: 52px;
		}
	}
	@media (max-width: 420px) {
		.live-block {
			display: none;
		}
	}

	main {
		min-height: calc(100vh - var(--nav-h) - 120px);
	}

	.foot {
		margin-top: var(--s-24);
		border-top: 1px solid var(--border);
	}
	.foot-in {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--s-4);
		flex-wrap: wrap;
		padding-block: var(--s-8);
		font-size: var(--text-sm);
		color: var(--text-3);
	}
	.foot-brand {
		display: flex;
		align-items: center;
		gap: var(--s-4);
		flex-wrap: wrap;
		color: var(--text-2);
	}
	.foot-brand p {
		color: var(--text-3);
	}
	.foot-links {
		display: flex;
		align-items: center;
		gap: var(--s-5);
	}
	.foot-links a {
		color: var(--text-2);
	}
	.foot-links a:hover {
		color: var(--text);
	}
</style>
