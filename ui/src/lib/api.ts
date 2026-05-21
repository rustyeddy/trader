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

export interface BacktestSummary {
  Name: string;
  Instrument: string;
  Timeframe: string;
  StartingBalance: number;
  EndingBalance: number;
  TotalTrades: number;
  WinRate: number;
  NetPnL: number;
  MaxDrawdown: number;
  SharpeRatio: number;
}

export interface BacktestResult {
  count: number;
  summaries: BacktestSummary[];
}
