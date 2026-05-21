// Typed REST client for the trader daemon API.
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

  closeTrade: (tradeId: string, units = 0) =>
    request<CloseTradeResult>('DELETE', `/api/v1/trades/${tradeId}?units=${units}`),

  updateStop: (tradeId: string, stopPrice: number, takePrice: number) =>
    request<{ trade_id: string; status: string }>(
      'PATCH', `/api/v1/trades/${tradeId}/stop`,
      { stop_price: stopPrice, take_price: takePrice },
    ),

  transactions: (sinceId = 0) =>
    request<TransactionsResult>('GET', `/api/v1/transactions?since_id=${sinceId}`),

  runBacktest: (configPaths: string[]) =>
    request<BacktestResult>('POST', '/api/v1/backtests/run', { config_paths: configPaths }),

  backtestList: (instrument = '', strategy = '') => {
    const params = new URLSearchParams();
    if (instrument) params.set('instrument', instrument);
    if (strategy)   params.set('strategy', strategy);
    const qs = params.toString();
    return request<BacktestListResult>('GET', `/api/v1/backtests${qs ? '?' + qs : ''}`);
  },

  backtestGet: (name: string) =>
    request<BacktestSummary>('GET', `/api/v1/backtests/${encodeURIComponent(name)}`),
};

// ── Types (mirror Go / OANDA JSON field names) ────────────────────────────

export interface AccountSummary {
  // Fields from oanda.AccountSummary (Go uses PascalCase → JSON default)
  ID: string;
  Currency: string;
  Balance: number;
  NAV: number;
  UnrealizedPL: number;
  MarginUsed: number;
  MarginAvail: number;
}

export interface OpenTrade {
  ID: string;
  Instrument: string;
  EntryPrice: number;
  Units: number;         // positive = long, negative = short
  UnrealizedPL: number;
  StopLoss: number;      // 0 if none
  TakeProfit: number;    // 0 if none
}

export interface CloseTradeResult {
  order_id: string;
  trade_id: string;
  units: number;
  price: number;
}

export interface ClosedTrade {
  tradeID: string;
  units: number;
  price: number;
  realizedPL: number;
}

export interface Transaction {
  ID: string;
  Type: string;
  Instrument: string;
  Units: number;
  Price: number;
  PL: number;
  AccountBalance: number;
  Time: string;
  TradesClosed: ClosedTrade[] | null;
}

export interface TransactionsResult {
  transactions: Transaction[];
  last_transaction_id: number;
}

// BacktestSummary matches trader.BacktestReportSummary JSON tags (snake_case).
export interface BacktestSummary {
  name: string;
  kind: string;
  strategy: string;
  instrument: string;
  timeframe: string;
  dataset: string;
  start: string;
  end: string;
  trades: number;
  wins: number;
  losses: number;
  start_balance: number;
  end_balance: number;
  net_pl: number;
  return_pct: number;
  win_rate: number;
  risk_pct: number;
  stop: string;
  regime: string;
  rr: number;
  max_drawdown: number;
  avg_winner: number;
  avg_loser: number;
  trade_details: BacktestReportTrade[] | null;
}

export interface BacktestReportTrade {
  id: string;
  instrument: string;
  side: string;
  units: number;
  open_price: number;
  close_price: number;
  open_time: string;
  close_time: string;
  pnl: number;
}

export interface BacktestListResult {
  count: number;
  summaries: BacktestSummary[];
}

export interface BacktestResult {
  count: number;
  summaries: BacktestSummary[];
}
