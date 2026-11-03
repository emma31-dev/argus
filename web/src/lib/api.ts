/**
 * Typed client for the Argus read-only HTTP API (docs/api.md).
 * All amounts are base-10 integer strings in smallest units; see format.ts.
 */

export type Status = 'green' | 'amber' | 'red' | 'unknown';
export const STATUSES: Status[] = ['red', 'amber', 'unknown', 'green'];

export interface Provider {
	name: string;
	url: string;
	healthy: boolean;
	last_error: string;
}

export interface StatusResponse {
	status: Status;
	last_block: number;
	last_block_time: string;
	last_checked_at: string;
	interval_seconds: number;
	debounce_snapshots: number;
	tokens_total: number;
	tokens_by_status: Record<Status, number>;
	open_incidents: number;
	providers: Provider[];
	version: string;
}

export interface Snapshot {
	block_height: number;
	block_time: string;
	owed: string;
	held: string;
	gap: string;
	provider?: string;
}

export interface Token {
	id: string;
	standard: string;
	contract: string;
	inner_id: string;
	symbol: string;
	decimals: number;
	/** Human chain label ("NEAR", "Ethereum", "BNB Chain"...). Symbols repeat across chains. */
	chain?: string;
	status: Status;
	negative_streak: number;
	latest: Snapshot | null;
	/** True when the most recent read of this token failed; status is kept from the last good read. */
	stale?: boolean;
	last_error?: string;
	/** Optional (P1), display only. */
	usd_price?: string;
}

export interface TokenDetail extends Token {
	open_incident: Incident | null;
}

export interface TokensResponse {
	block_height: number;
	tokens: Token[];
}

export interface HistoryResponse {
	token: { id: string; symbol: string; decimals: number };
	points: Snapshot[];
}

export interface Incident {
	id: number;
	token_id: string;
	symbol: string;
	decimals: number;
	start_block: number;
	red_block: number;
	end_block?: number | null;
	opened_at: string;
	closed_at?: string | null;
	duration_seconds?: number | null;
	max_deficit: string;
	max_deficit_block: number;
	summary?: string;
}

export interface IncidentEvent {
	incident_id: number;
	block_height: number;
	receipt_id: string;
	tx_hash?: string;
	standard: string;
	event: string;
	token_id?: string;
	amount?: string;
	raw: string;
}

export interface IncidentDetail extends Incident {
	events: IncidentEvent[];
	snapshots: Snapshot[];
}

export interface IncidentsResponse {
	incidents: Incident[];
}

export interface ReplayPoint extends Snapshot {
	label: string;
	status: Status;
}

export interface ReplayToken {
	id: string;
	symbol: string;
	decimals: number;
	points: ReplayPoint[];
	first_negative_block: number;
	detected_red_block: number;
	max_deficit: string;
}

export interface ReplayResponse {
	title: string;
	generated_at: string;
	rpc_provider: string;
	notes: string;
	tokens: ReplayToken[];
}

export class ApiError extends Error {
	/** HTTP status, or 0 when the API could not be reached at all. */
	readonly status: number;
	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
	get offline() {
		return this.status === 0 || this.status >= 502;
	}
	get notFound() {
		return this.status === 404;
	}
}

/** Base URL of the API, '' for same origin. Set PUBLIC_API_BASE at build time. */
export const API_BASE: string = typeof __ARGUS_API_BASE__ === 'string' ? __ARGUS_API_BASE__ : '';

export function apiUrl(path: string, params?: Record<string, string | number | undefined | null>): string {
	let url = `${API_BASE}${path}`;
	if (params) {
		const q = new URLSearchParams();
		for (const [k, v] of Object.entries(params)) {
			if (v !== undefined && v !== null && v !== '') q.set(k, String(v));
		}
		const qs = q.toString();
		if (qs) url += `?${qs}`;
	}
	return url;
}

const TIMEOUT_MS = 15_000;

async function getJson<T>(
	path: string,
	params?: Record<string, string | number | undefined | null>,
	signal?: AbortSignal
): Promise<T> {
	const ctrl = new AbortController();
	const timer = setTimeout(() => ctrl.abort(new DOMException('timeout', 'TimeoutError')), TIMEOUT_MS);
	const onAbort = () => ctrl.abort(signal?.reason);
	signal?.addEventListener('abort', onAbort, { once: true });
	let res: Response;
	try {
		res = await fetch(apiUrl(path, params), {
			signal: ctrl.signal,
			headers: { accept: 'application/json' }
		});
	} catch (e) {
		if (signal?.aborted) throw e;
		throw new ApiError(0, 'The Argus API could not be reached.');
	} finally {
		clearTimeout(timer);
		signal?.removeEventListener('abort', onAbort);
	}
	const type = res.headers.get('content-type') ?? '';
	if (!res.ok) {
		// The Argus API always answers errors with JSON. Anything else came from
		// a proxy, CDN or static host in front of it: treat the API as unreachable.
		if (!type.includes('json')) throw new ApiError(0, `The Argus API is unavailable (${res.status}).`);
		let message = `${res.status} ${res.statusText}`.trim();
		try {
			const body = await res.json();
			if (body && typeof body.error === 'string') message = body.error;
		} catch {
			/* malformed error body */
		}
		throw new ApiError(res.status, message);
	}
	if (!type.includes('json')) {
		// A static host answering /v1/* with the SPA shell means no API is behind it.
		throw new ApiError(0, 'The Argus API is not available at this address.');
	}
	return (await res.json()) as T;
}

const enc = encodeURIComponent;

export const api = {
	status: (signal?: AbortSignal) => getJson<StatusResponse>('/v1/status', undefined, signal),
	tokens: (signal?: AbortSignal) => getJson<TokensResponse>('/v1/tokens', undefined, signal),
	token: (id: string, signal?: AbortSignal) => getJson<TokenDetail>(`/v1/tokens/${enc(id)}`, undefined, signal),
	history: (
		id: string,
		range: { from?: string | number; to?: string | number; limit?: number } = {},
		signal?: AbortSignal
	) => getJson<HistoryResponse>(`/v1/tokens/${enc(id)}/history`, range, signal),
	incidents: (status: 'open' | 'closed' | 'all' = 'all', limit?: number, signal?: AbortSignal) =>
		getJson<IncidentsResponse>('/v1/incidents', { status, limit }, signal),
	incident: (id: number | string, signal?: AbortSignal) =>
		getJson<IncidentDetail>(`/v1/incidents/${enc(String(id))}`, undefined, signal),
	replay: (signal?: AbortSignal) => getJson<ReplayResponse>('/v1/replay', undefined, signal)
};

export function exportCsvUrl(token?: string, range: { from?: string | number; to?: string | number } = {}) {
	return apiUrl('/v1/export.csv', { token, ...range });
}

/** Rank for client-side sorting, mirroring the server's deficit-risk order. */
export const STATUS_RANK: Record<Status, number> = { red: 0, amber: 1, unknown: 2, green: 3 };
