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

// acct builds the scoped route prefix for an account's broker operations.
function acct(accountId: string): string {
  const id = accountId.trim();
  if (!id) throw new Error('account ID is required');
  return `/api/v1/accounts/${encodeURIComponent(id)}`;
}

export const api = {
  health: () => request<{ status: string }>('GET', '/health'),

  // Accounts the configured token can access; default flagged for the UI.
  accounts: () => request<AccountsResult>('GET', '/api/v1/accounts'),
  defaultAccount: () => request<AccountInfo>('GET', '/api/v1/accounts/default'),

  account: (accountId: string) =>
    request<AccountSummary>('GET', `${acct(accountId)}/account`),

  trades: (accountId: string) =>
    request<OpenTrade[]>('GET', `${acct(accountId)}/trades`),

  closeTrade: (accountId: string, tradeId: string, units = 0) =>
    request<CloseTradeResult>('DELETE', `${acct(accountId)}/trades/${tradeId}?units=${units}`),

  updateStop: (accountId: string, tradeId: string, stopPrice: number, takePrice: number) =>
    request<{ trade_id: string; status: string }>(
      'PATCH', `${acct(accountId)}/trades/${tradeId}/stop`,
      { stop_price: stopPrice, take_price: takePrice },
    ),

  transactions: (accountId: string, sinceId = 0) =>
    request<TransactionsResult>('GET', `${acct(accountId)}/transactions?since_id=${sinceId}`),

  placeTrade: (accountId: string, req: {
    instrument: string; side: string;
    risk_pct: number; stop_pips: number;
    units?: number; confirm: boolean;
  }) => request<PlaceOrderResult>('POST', `${acct(accountId)}/trades`, req),

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

  backtestCandles: (name: string) =>
    request<CandleResponse>('GET', `/api/v1/backtests/${encodeURIComponent(name)}/candles`),

  replay: (req: ReplayRequest) =>
    request<ReplayResult>('POST', '/api/v1/replay', req),
};

// ── Types (mirror Go / OANDA JSON field names) ────────────────────────────

export interface AccountInfo {
  id: string;
  is_default: boolean;
}

export interface AccountsResult {
  accounts: AccountInfo[];
}

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

export interface PlaceOrderProposal {
  Instrument: string;
  Side: string;
  Units: number;
  EntryPrice: number;
  StopPrice: number;
  RiskAmount: number;
  AccountNAV: number;
}

export interface PlaceOrderFilled {
  OrderID: string;
  TradeID: string;
  Instrument: string;
  Units: number;
  Price: number;
}

export interface PlaceOrderResult {
  Proposal: PlaceOrderProposal;
  Filled: PlaceOrderFilled | null;
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
  stop_price?: number;
  take_profit_price?: number;
}

export interface CandleBar {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
}

export interface CandleResponse {
  instrument: string;
  timeframe: string;
  bars: CandleBar[];
}

export interface BacktestListResult {
  count: number;
  summaries: BacktestSummary[];
}

export interface BacktestResult {
  count: number;
  summaries: BacktestSummary[];
}

// ── Replay ────────────────────────────────────────────────────────────────────

export type SignalKind = 'open' | 'close' | 'blocked' | 'no_stop' | 'stop_update';

export interface ReplaySignal {
  time: number;       // unix seconds (bar open time)
  kind: SignalKind;
  side?: string;      // "long" | "short"
  price?: number;     // entry (open) or exit (close) price
  stop_price?: number;
  stop_pips?: number;
  reason?: string;
}

export interface ReplayResult {
  instrument: string;
  timeframe: string;
  strategy: string;
  from: string;
  to: string;
  warmup_bars: number;
  bars: CandleBar[];
  signals: ReplaySignal[];
}

export interface ReplayRequest {
  instrument: string;
  timeframe: string;
  from: string;
  to: string;
  warmup_bars?: number;
  strategy: { kind: string; params?: Record<string, unknown> };
  exit?:     { kind: string; params?: Record<string, unknown> };
  regime?:   { kind: string; params?: Record<string, unknown> };
}
