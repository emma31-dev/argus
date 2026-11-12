import { resolve } from '$app/paths';

export const REPO_URL = 'https://github.com/emma31-dev/argus';
export const API_DOC_URL = `${REPO_URL}/blob/main/docs/api.md`;
export const INTENTS_CONTRACT = 'intents.near';

const EXPLORER = 'https://nearblocks.io';
export const explorer = {
	block: (height: number | string) => `${EXPLORER}/blocks/${height}`,
	account: (id: string) => `${EXPLORER}/address/${encodeURIComponent(id)}`,
	tx: (hash: string) => `${EXPLORER}/txns/${encodeURIComponent(hash)}`
};

/** Internal routes. Ids travel in the query string so the site stays fully static. */
export const href = {
	home: () => resolve('/'),
	token: (id: string) => resolve(`token/?id=${encodeURIComponent(id)}`),
	incidents: () => resolve(`incidents/`),
	incident: (id: number | string) => resolve(`incident/?id=${encodeURIComponent(String(id))}`),
	how: (hash = '') => `${resolve('/how-it-works')}/${hash}`,
	replay: () => resolve(`replay/`),
	api: (hash = '') => `${resolve('/api')}/${hash}`
};
