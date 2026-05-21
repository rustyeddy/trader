<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { sseStore, sseLog } from '$lib/sse';
  import { api, type AccountSummary, type Transaction } from '$lib/api';

  // Account summary — live via SSE, falls back gracefully when OANDA absent.
  const { data: account, status: accountStatus } = sseStore<AccountSummary>(
    '/api/v1/stream/account',
    'account',
  );

  // Transaction events — append-only log, newest first.
  const events = sseLog<Transaction>('/api/v1/stream/events', 'transaction', 50);

  // Open trades — poll every 5 s (no streaming trade positions from OANDA REST).
  const tradesQuery = createQuery({
    queryKey: ['trades'],
    queryFn: api.trades,
    refetchInterval: 5_000,
    retry: false,
  });

  // Health — tiny pulse for the status indicator.
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

  // Derive connection dot colour from SSE status + health.
  $: dotClass =
    $healthQuery.isError              ? 'bg-loss' :
    $accountStatus === 'open'         ? 'bg-profit' :
    $accountStatus === 'connecting'   ? 'bg-yellow-400 animate-pulse' :
    $accountStatus === 'error'        ? 'bg-loss' :
                                        'bg-slate-500 animate-pulse';
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <h1 class="text-xl font-semibold text-slate-100">Dashboard</h1>
    <span class="flex items-center gap-2 text-sm">
      <span class="h-2 w-2 rounded-full {dotClass}"></span>
      <span class="text-slate-400">
        {#if $healthQuery.isError}
          API unreachable
        {:else if $accountStatus === 'open'}
          Live
        {:else if $accountStatus === 'connecting'}
          Connecting…
        {:else}
          No OANDA
        {/if}
      </span>
    </span>
  </div>

  <!-- Account summary (SSE) -->
  <section>
    <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">Account</h2>
    {#if $account}
      {@const a = $account}
      <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Balance</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(a.balance)}</div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">NAV</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(a.nav)}</div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Unrealized P/L</div>
          <div class="text-lg font-mono font-semibold {plClass(a.unrealizedPL)}">
            {fmtMoney(a.unrealizedPL)}
          </div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Free Margin</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(a.marginAvailable)}</div>
        </div>
      </div>
    {:else}
      <div class="card text-slate-500 text-sm">
        {#if $accountStatus === 'connecting'}
          Connecting to account stream…
        {:else}
          OANDA not configured — start <code class="text-slate-300">trader serve --token …</code>
        {/if}
      </div>
    {/if}
  </section>

  <!-- Two-column: open trades + event log -->
  <div class="grid grid-cols-1 xl:grid-cols-2 gap-6">

    <!-- Open trades (polled) -->
    <section>
      <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">Open Trades</h2>
      {#if $tradesQuery.isLoading}
        <div class="card text-slate-500 text-sm">Loading…</div>
      {:else if $tradesQuery.isError}
        <div class="card text-slate-500 text-sm">No OANDA connection</div>
      {:else if !$tradesQuery.data || $tradesQuery.data.length === 0}
        <div class="card text-slate-500 text-sm">No open trades.</div>
      {:else}
        <div class="card p-0 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-surface-border text-slate-400 text-xs uppercase">
                <th class="px-3 py-2 text-left">Instr</th>
                <th class="px-3 py-2 text-right">Units</th>
                <th class="px-3 py-2 text-right">Entry</th>
                <th class="px-3 py-2 text-right">Unreal P/L</th>
              </tr>
            </thead>
            <tbody>
              {#each $tradesQuery.data as t (t.id)}
                <tr class="border-b border-surface-border table-row-hover">
                  <td class="px-3 py-2 font-semibold">{t.instrument}</td>
                  <td class="px-3 py-2 text-right font-mono
                             {t.units > 0 ? 'text-profit' : 'text-loss'}">
                    {t.units.toLocaleString()}
                  </td>
                  <td class="px-3 py-2 text-right font-mono">{fmtPrice(t.entryPrice)}</td>
                  <td class="px-3 py-2 text-right font-mono {plClass(t.unrealizedPL)}">
                    {fmtMoney(t.unrealizedPL)}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <!-- Live event log (SSE) -->
    <section>
      <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">
        Live Events
        {#if $events.length > 0}
          <span class="ml-2 text-xs font-normal normal-case text-slate-500">
            ({$events.length})
          </span>
        {/if}
      </h2>
      {#if $events.length === 0}
        <div class="card text-slate-500 text-sm">
          Waiting for broker events…
        </div>
      {:else}
        <div class="card p-0 overflow-hidden max-h-64 overflow-y-auto">
          <table class="w-full text-xs">
            <tbody>
              {#each $events as ev}
                <tr class="border-b border-surface-border table-row-hover">
                  <td class="px-3 py-1.5 font-mono text-slate-400 whitespace-nowrap">
                    {new Date(ev.time).toLocaleTimeString()}
                  </td>
                  <td class="px-3 py-1.5 text-slate-300">{ev.type}</td>
                  <td class="px-3 py-1.5 font-mono">{ev.instrument || '—'}</td>
                  <td class="px-3 py-1.5 font-mono text-right
                             {ev.pl !== 0 ? plClass(ev.pl) : 'text-slate-500'}">
                    {ev.pl !== 0 ? fmtMoney(ev.pl) : ''}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

  </div>
</div>
