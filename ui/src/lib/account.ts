// Account selection store: which OANDA account the UI is currently viewing.
//
// The selected account is threaded into every account-scoped API call and SSE
// stream. It is persisted to localStorage so a reload keeps the same account,
// and falls back to the server-reported default (first account) otherwise.
import { writable, derived, get, type Readable } from 'svelte/store';
import { api, type AccountInfo } from './api';

const STORAGE_KEY = 'trader.selectedAccount';

function initialSelection(): string {
  if (typeof localStorage === 'undefined') return '';
  return localStorage.getItem(STORAGE_KEY) ?? '';
}

/** All accounts the configured token can access. */
export const accounts = writable<AccountInfo[]>([]);

/** The account ID the UI currently operates on; '' until resolved. */
export const selectedAccountId = writable<string>(initialSelection());

// Persist selection across reloads.
selectedAccountId.subscribe((id) => {
  if (typeof localStorage === 'undefined') return;
  if (id) localStorage.setItem(STORAGE_KEY, id);
});

/**
 * Load the account list and ensure a valid account is selected: keep the
 * persisted choice when it is still accessible, otherwise pick the server's
 * default (first account). Safe to call repeatedly.
 */
export async function loadAccounts(): Promise<void> {
  const { accounts: list } = await api.accounts();
  accounts.set(list);

  const current = get(selectedAccountId);
  if (current && list.some((a) => a.id === current)) return;

  const def = list.find((a) => a.is_default) ?? list[0];
  selectedAccountId.set(def ? def.id : '');
}

// Scoped SSE stream URLs that track the selected account. Empty string while
// no account is selected (e.g. backtest-only server) so the streams stay idle.
export const accountStreamUrl: Readable<string> = derived(selectedAccountId, (id) =>
  id ? `/api/v1/accounts/${encodeURIComponent(id)}/stream/account` : '',
);

export const eventsStreamUrl: Readable<string> = derived(selectedAccountId, (id) =>
  id ? `/api/v1/accounts/${encodeURIComponent(id)}/stream/events` : '',
);
