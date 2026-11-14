<script lang="ts">
	import CopyButton from './CopyButton.svelte';
	import Icon from './Icon.svelte';

	let { retry }: { retry: () => void | Promise<void> } = $props();

	const commands = `git clone https://github.com/emma31-dev/argus.git
cd argus
cp .env.example .env
docker compose up --build -d`;

	const prompt = `Run the Argus NEAR Intents solvency monitor locally with Docker Compose.

1. git clone https://github.com/emma31-dev/argus.git
2. cd argus
3. cp .env.example .env
4. docker compose up --build -d
5. Wait for the first snapshot round, then open http://localhost:3000

Argus is read-only: it needs no wallet, keys or funded account. On the free RPC endpoints at 1 request per second the first round takes about 10 minutes. Check http://localhost:8080/healthz and http://localhost:8080/v1/status while you wait. Do not change the code or add secrets. Tell me when the dashboard shows token rows.`;

	let busy = $state(false);

	async function onRetry() {
		busy = true;
		try {
			await retry();
		} finally {
			busy = false;
		}
	}
</script>

<section class="card setup" id="setup" aria-labelledby="setup-h">
	<div class="head">
		<span class="glyph"><Icon name="offline" size={18} stroke={2} /></span>
		<div class="head-text">
			<h2 id="setup-h">The monitor is not running</h2>
			<p>
				This is the static build. It reads live numbers from the Go API's <code>/v1</code> routes, and
				nothing answered on this host. Argus never shows placeholder numbers.
			</p>
		</div>
		<button class="btn" type="button" onclick={onRetry} disabled={busy}>
			<Icon name="refresh" size={14} />{busy ? 'Retrying…' : 'Retry'}
		</button>
	</div>

	<div class="cols">
		<div class="col">
			<div class="col-head">
				<h3>Run the full stack</h3>
				<CopyButton value={commands} showLabel label="Copy commands" />
			</div>
			<ol class="steps">
				<li>
					<span>Clone the repository.</span>
					<code>git clone https://github.com/emma31-dev/argus.git</code>
					<code>cd argus</code>
				</li>
				<li>
					<span>Copy the example env. The defaults work and need no keys.</span>
					<code>cp .env.example .env</code>
				</li>
				<li>
					<span>Start Postgres, Redis, the monitor and this dashboard.</span>
					<code>docker compose up --build -d</code>
				</li>
				<li>
					<span>
						Open <a href="http://localhost:3000" target="_blank" rel="noopener noreferrer"
							>localhost:3000</a
						>. The first round takes about 10 minutes on free RPC at 1 request per second.
					</span>
				</li>
			</ol>
			<p class="hint">
				Needs Docker Engine with Compose v2.24 or newer, in WSL, Docker Desktop or any Linux host. On
				PowerShell, copy the env file with <code>Copy-Item .env.example .env</code>.
			</p>
		</div>

		<div class="col agent">
			<div class="col-head">
				<h3>Or hand it to your agent</h3>
				<CopyButton value={prompt} showLabel label="Copy prompt" />
			</div>
			<p class="agent-text">Paste this into Claude Code, Cursor, Codex or any agent with shell access.</p>
			<pre class="prompt">{prompt}</pre>
		</div>
	</div>

	<p class="foot">
		Deploying this build separately? Set <code>PUBLIC_API_BASE</code> to the monitor's public URL when you
		build, and allow this origin in <code>ARGUS_CORS_ORIGINS</code>.
	</p>
</section>

<style>
	.setup {
		padding: var(--s-6);
		scroll-margin-top: calc(var(--nav-h) + var(--s-4));
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: var(--s-3);
	}
	.glyph {
		display: grid;
		place-items: center;
		flex: none;
		width: 36px;
		height: 36px;
		border-radius: var(--r);
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text-2);
	}
	.head-text {
		flex: 1;
		min-width: 0;
	}
	.head-text h2 {
		font-size: var(--text-lg);
	}
	.head-text p {
		margin-top: var(--s-1);
		max-width: 72ch;
		color: var(--text-2);
		font-size: var(--text-sm);
		line-height: var(--leading-loose);
	}
	.head-text code,
	.hint code,
	.foot code,
	.steps code {
		font-family: var(--font-mono);
		font-size: 0.9em;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: var(--r-xs);
		padding: 1px 5px;
	}
	.head > .btn {
		flex: none;
		margin-top: 2px;
	}

	.cols {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--s-6);
		margin-top: var(--s-6);
		padding-top: var(--s-5);
		border-top: 1px solid var(--border);
	}
	.col {
		min-width: 0;
	}
	.col-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--s-3);
		margin-bottom: var(--s-3);
	}
	.col-head h3 {
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.steps {
		display: grid;
		gap: var(--s-3);
		margin: 0;
		padding: 0;
		list-style: none;
		counter-reset: step;
	}
	.steps li {
		display: grid;
		gap: 6px;
		position: relative;
		padding-left: 28px;
		counter-increment: step;
		font-size: var(--text-sm);
		color: var(--text-2);
	}
	.steps li::before {
		content: counter(step);
		position: absolute;
		left: 0;
		top: 0;
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text-3);
		font-family: var(--font-mono);
		font-size: var(--text-2xs);
	}
	.steps li > span {
		line-height: var(--leading);
	}
	.steps code {
		display: block;
		padding: 5px 8px;
		overflow-x: auto;
		white-space: nowrap;
	}
	.steps a {
		color: var(--accent);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.hint {
		margin-top: var(--s-4);
		color: var(--text-3);
		font-size: var(--text-xs);
		line-height: var(--leading-loose);
	}

	.agent-text {
		color: var(--text-3);
		font-size: var(--text-xs);
		line-height: var(--leading-loose);
	}
	.prompt {
		margin: var(--s-2) 0 0;
		padding: var(--s-3) var(--s-4);
		max-height: 330px;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--r);
		background: var(--surface-2);
		font-family: var(--font-mono);
		font-size: 12px;
		line-height: 1.6;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		color: var(--text-2);
	}
	.foot {
		margin-top: var(--s-5);
		padding-top: var(--s-4);
		border-top: 1px solid var(--border);
		color: var(--text-3);
		font-size: var(--text-xs);
		line-height: var(--leading-loose);
	}

	@media (max-width: 900px) {
		.cols {
			grid-template-columns: 1fr;
		}
	}
	@media (max-width: 560px) {
		.setup {
			padding: var(--s-4);
		}
		.head {
			flex-wrap: wrap;
		}
		.head > .btn {
			margin-left: 48px;
		}
		.col-head {
			align-items: flex-start;
			flex-wrap: wrap;
		}
	}
</style>
