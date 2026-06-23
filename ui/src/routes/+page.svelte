<script lang="ts">
  import { writable, get } from 'svelte/store';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { sseStoreReactive, sseLogReactive } from '$lib/sse';
  import { api, type AccountSummary, type OpenTrade, type Transaction, type TransactionsResult } from '$lib/api';
  import { selectedAccountId, accountStreamUrl, eventsStreamUrl } from '$lib/account';
  import EquitySparkline from '$lib/components/EquitySparkline.svelte';

  const qc = useQueryClient();

  // SSE — live account stream for the selected account.
  const { data: account, status: accountStatus } = sseStoreReactive<AccountSummary>(
    accountStreamUrl, 'account',
  );

  // Build a rolling NAV history for the sparkline (last 120 samples ≈ 10 min).
  const navHistory = writable<number[]>([]);
  $: if ($account) {
    navHistory.update(h => [...h, $account!.NAV].slice(-120));
  }

  // SSE — live transaction events for the selected account.
  const events = sseLogReactive<Transaction>(eventsStreamUrl, 'transaction', 50);

  // Polled open trades for the selected account. The queryFn reads the current
  // account at call time; switching accounts invalidates below to refetch.
  const tradesQuery = createQuery({
    queryKey: ['trades'],
    queryFn: () => {
      const id = get(selectedAccountId);
      return id ? api.trades(id) : Promise.resolve([] as OpenTrade[]);
    },
    refetchInterval: 5_000,
    retry: false,
  });

  // Recent closed trades — transactions with realized P/L.
  const txQuery = createQuery({
    queryKey: ['transactions'],
    queryFn: () => {
      const id = get(selectedAccountId);
      return id
        ? api.transactions(id, 0)
        : Promise.resolve({ transactions: [], last_transaction_id: 0 } as TransactionsResult);
    },
    refetchInterval: 30_000,
    retry: false,
  });

  // On account switch, drop the per-account NAV history and refetch the
  // account-scoped queries immediately rather than waiting for the next poll.
  $: $selectedAccountId, onAccountChange();
  function onAccountChange() {
    navHistory.set([]);
    qc.invalidateQueries({ queryKey: ['trades'] });
    qc.invalidateQueries({ queryKey: ['transactions'] });
  }

  $: closedTrades = ($txQuery.data?.transactions ?? [])
    .filter(t => t.Type === 'ORDER_FILL' && t.PL !== 0)
    .slice(0, 10);

  $: dailyPL = closedTrades
    .filter(t => {
      const d = new Date(t.Time);
      const now = new Date();
      return d.toDateString() === now.toDateString();
    })
    .reduce((sum, t) => sum + t.PL, 0);

  // Health check for the status indicator.
  const healthQuery = createQuery({
    queryKey: ['health'],
    queryFn: api.health,
    refetchInterval: 10_000,
  });

  function fmtPrice(n: number) { return n.toFixed(5); }
  function fmtMoney(n: number) {
    return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });
  }
  function plClass(n: number) { return n >= 0 ? 'badge-profit' : 'badge-loss'; }

  $: dotClass =
    $healthQuery.isError            ? 'bg-loss' :
    $accountStatus === 'open'       ? 'bg-profit' :
    $accountStatus === 'connecting' ? 'bg-yellow-400 animate-pulse' :
    $accountStatus === 'error'      ? 'bg-loss' :
                                      'bg-slate-500 animate-pulse';
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <h1 class="text-xl font-semibold text-slate-100">Dashboard</h1>
    <span class="flex items-center gap-2 text-sm">
      <span class="h-2 w-2 rounded-full {dotClass}"></span>
      <span class="text-slate-400">
        {#if $healthQuery.isError}API unreachable
        {:else if $accountStatus === 'open'}Live
        {:else if $accountStatus === 'connecting'}Connecting…
        {:else}No OANDA{/if}
      </span>
    </span>
  </div>

  <!-- Account summary cards -->
  {#if $account}
    {@const a = $account}
    <div class="grid grid-cols-2 md:grid-cols-5 gap-3">
      <div class="card">
        <div class="text-xs text-slate-400 mb-1">Balance</div>
        <div class="text-base font-mono font-semibold">{fmtMoney(a.Balance)}</div>
      </div>
      <div class="card">
        <div class="text-xs text-slate-400 mb-1">NAV</div>
        <div class="text-base font-mono font-semibold">{fmtMoney(a.NAV)}</div>
      </div>
      <div class="card">
        <div class="text-xs text-slate-400 mb-1">Unrealized P/L</div>
        <div class="text-base font-mono font-semibold {plClass(a.UnrealizedPL)}">{fmtMoney(a.UnrealizedPL)}</div>
      </div>
      <div class="card">
        <div class="text-xs text-slate-400 mb-1">Free Margin</div>
        <div class="text-base font-mono font-semibold">{fmtMoney(a.MarginAvail)}</div>
      </div>
      <div class="card">
        <div class="text-xs text-slate-400 mb-1">Daily P/L</div>
        <div class="text-base font-mono font-semibold {plClass(dailyPL)}">{fmtMoney(dailyPL)}</div>
      </div>
    </div>

    <!-- Equity sparkline -->
    <div class="card">
      <div class="text-xs text-slate-400 mb-2">NAV (session)</div>
      <EquitySparkline values={$navHistory} width={700} height={56} />
    </div>
  {:else}
    <div class="card text-slate-500 text-sm">
      {$accountStatus === 'connecting' ? 'Connecting to account stream…'
        : 'Start trader serve --token … to enable live data.'}
    </div>
  {/if}

  <!-- Three-column lower section -->
  <div class="grid grid-cols-1 xl:grid-cols-3 gap-6">

    <!-- Open trades -->
    <div class="xl:col-span-1">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider">Open Trades</h2>
        <a href="/trades" class="text-xs text-accent hover:underline">Manage →</a>
      </div>
      {#if $tradesQuery.isLoading}
        <div class="card text-slate-500 text-sm">Loading…</div>
      {:else if $tradesQuery.isError || !$tradesQuery.data}
        <div class="card text-slate-500 text-sm">No OANDA connection</div>
      {:else if $tradesQuery.data.length === 0}
        <div class="card text-slate-500 text-sm">No open trades.</div>
      {:else}
        <div class="card p-0 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-surface-border text-xs text-slate-400 uppercase">
                <th class="px-3 py-2 text-left">Instr</th>
                <th class="px-3 py-2 text-right">Units</th>
                <th class="px-3 py-2 text-right">P/L</th>
              </tr>
            </thead>
            <tbody>
              {#each $tradesQuery.data as t (t.ID)}
                <tr class="border-b border-surface-border table-row-hover">
                  <td class="px-3 py-2 font-semibold">{t.Instrument}</td>
                  <td class="px-3 py-2 text-right font-mono {t.Units > 0 ? 'text-profit' : 'text-loss'}">
                    {t.Units.toLocaleString()}
                  </td>
                  <td class="px-3 py-2 text-right font-mono {plClass(t.UnrealizedPL)}">
                    {fmtMoney(t.UnrealizedPL)}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <!-- Recent closed trades -->
    <div class="xl:col-span-1">
      <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">Recent Closed</h2>
      {#if closedTrades.length === 0}
        <div class="card text-slate-500 text-sm">No closed trades yet.</div>
      {:else}
        <div class="card p-0 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-surface-border text-xs text-slate-400 uppercase">
                <th class="px-3 py-2 text-left">Instr</th>
                <th class="px-3 py-2 text-right">Price</th>
                <th class="px-3 py-2 text-right">P/L</th>
              </tr>
            </thead>
            <tbody>
              {#each closedTrades as t (t.ID)}
                <tr class="border-b border-surface-border table-row-hover">
                  <td class="px-3 py-2 font-semibold">{t.Instrument || '—'}</td>
                  <td class="px-3 py-2 text-right font-mono">{fmtPrice(t.Price)}</td>
                  <td class="px-3 py-2 text-right font-mono {plClass(t.PL)}">{fmtMoney(t.PL)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <!-- Live event log -->
    <div class="xl:col-span-1">
      <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">
        Live Events {#if $events.length > 0}<span class="text-xs font-normal normal-case text-slate-500">({$events.length})</span>{/if}
      </h2>
      {#if $events.length === 0}
        <div class="card text-slate-500 text-sm">Waiting for broker events…</div>
      {:else}
        <div class="card p-0 overflow-hidden max-h-56 overflow-y-auto">
          <table class="w-full text-xs">
            <tbody>
              {#each $events as ev (ev.ID)}
                <tr class="border-b border-surface-border table-row-hover">
                  <td class="px-3 py-1.5 font-mono text-slate-400 whitespace-nowrap">
                    {new Date(ev.Time).toLocaleTimeString()}
                  </td>
                  <td class="px-3 py-1.5 text-slate-300 truncate max-w-[6rem]">{ev.Type}</td>
                  <td class="px-3 py-1.5 font-mono">{ev.Instrument || '—'}</td>
                  <td class="px-3 py-1.5 text-right font-mono {ev.PL !== 0 ? plClass(ev.PL) : 'text-slate-500'}">
                    {ev.PL !== 0 ? fmtMoney(ev.PL) : ''}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

  </div>
</div>
