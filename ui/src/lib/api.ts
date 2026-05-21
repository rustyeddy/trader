// Typed REST client for the trader daemon API.
// BASE_URL is empty in production (same-origin); set VITE_API_URL for
// cross-origin dev if not using the Vite proxy.

const BASE = import.meta.env.VITE_API_URL ?? '';

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`${method} ${path} → ${res.status}: ${text}`);
  }
  return res.json() as Promise<T>;
}

export const api = {
  health: () => request<{ status: string }>('GET', '/health'),
  account: () => request<AccountSummary>('GET', '/api/v1/account'),
  trades: () => request<OpenTrade[]>('GET', '/api/v1/trades'),
  transactions: (sinceId = 0) =>
    request<TransactionsResult>('GET', `/api/v1/transactions?since_id=${sinceId}`),
  runBacktest: (configPaths: string[]) =>
    request<BacktestResult>('POST', '/api/v1/backtests/run', { config_paths: configPaths }),
};

// ── Types (mirror Go types) ───────────────────────────────────────────────

export interface AccountSummary {
  balance: number;
  nav: number;
  unrealizedPL: number;
  marginUsed: number;
  marginAvailable: number;
  currency: string;
}

export interface OpenTrade {
  id: string;
  instrument: string;
  units: number;
  entryPrice: number;
  stopLoss: number;
  unrealizedPL: number;
  currentPrice: number;
}

export interface Transaction {
  id: string;
  type: string;
  instrument: string;
  units: number;
  price: number;
  pl: number;
  time: string;
}

export interface TransactionsResult {
  transactions: Transaction[];
  last_transaction_id: number;
}

export interface BacktestSummary {
  name: string;
  instrument: string;
  timeframe: string;
  startDate: string;
  endDate: string;
  startingBalance: number;
  endingBalance: number;
  totalTrades: number;
  winRate: number;
  netPnL: number;
  maxDrawdown: number;
  sharpeRatio: number;
}

export interface BacktestResult {
  count: number;
  summaries: BacktestSummary[];
}
