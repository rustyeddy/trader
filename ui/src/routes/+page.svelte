<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { api, type OpenTrade } from '$lib/api';

  const healthQuery = createQuery({
    queryKey: ['health'],
    queryFn: api.health,
    refetchInterval: 5_000,
  });

  const accountQuery = createQuery({
    queryKey: ['account'],
    queryFn: api.account,
    refetchInterval: 5_000,
    retry: false,
  });

  const tradesQuery = createQuery({
    queryKey: ['trades'],
    queryFn: api.trades,
    refetchInterval: 5_000,
    retry: false,
  });

  function fmtPrice(n: number) {
    return n.toFixed(5);
  }
  function fmtMoney(n: number) {
    return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });
  }
  function plClass(n: number) {
    return n >= 0 ? 'badge-profit' : 'badge-loss';
  }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h1 class="text-xl font-semibold text-slate-100">Dashboard</h1>
    <span class="flex items-center gap-2 text-sm">
      {#if $healthQuery.isSuccess}
        <span class="h-2 w-2 rounded-full bg-profit"></span>
        <span class="text-slate-400">API connected</span>
      {:else if $healthQuery.isError}
        <span class="h-2 w-2 rounded-full bg-loss"></span>
        <span class="text-slate-400">API unreachable</span>
      {:else}
        <span class="h-2 w-2 rounded-full bg-slate-500 animate-pulse"></span>
        <span class="text-slate-500">Connecting…</span>
      {/if}
    </span>
  </div>

  <!-- Account summary -->
  <section>
    <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">Account</h2>
    {#if $accountQuery.isLoading}
      <div class="card text-slate-500 text-sm">Loading account…</div>
    {:else if $accountQuery.isError}
      <div class="card text-slate-500 text-sm">
        OANDA not configured — start <code class="text-slate-300">trader serve --token …</code>
      </div>
    {:else if $accountQuery.data}
      {@const acct = $accountQuery.data}
      <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Balance</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(acct.balance)}</div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">NAV</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(acct.nav)}</div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Unrealized P/L</div>
          <div class="text-lg font-mono font-semibold {plClass(acct.unrealizedPL)}">
            {fmtMoney(acct.unrealizedPL)}
          </div>
        </div>
        <div class="card">
          <div class="text-xs text-slate-400 mb-1">Free Margin</div>
          <div class="text-lg font-mono font-semibold">{fmtMoney(acct.marginAvailable)}</div>
        </div>
      </div>
    {/if}
  </section>

  <!-- Open trades -->
  <section>
    <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">Open Trades</h2>
    {#if $tradesQuery.isLoading}
      <div class="card text-slate-500 text-sm">Loading trades…</div>
    {:else if $tradesQuery.isError}
      <div class="card text-slate-500 text-sm">No OANDA connection</div>
    {:else if $tradesQuery.data && $tradesQuery.data.length === 0}
      <div class="card text-slate-500 text-sm">No open trades.</div>
    {:else if $tradesQuery.data}
      <div class="card p-0 overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-surface-border text-slate-400 text-xs uppercase">
              <th class="px-4 py-2 text-left">ID</th>
              <th class="px-4 py-2 text-left">Instrument</th>
              <th class="px-4 py-2 text-right">Units</th>
              <th class="px-4 py-2 text-right">Entry</th>
              <th class="px-4 py-2 text-right">Stop</th>
              <th class="px-4 py-2 text-right">Unreal P/L</th>
            </tr>
          </thead>
          <tbody>
            {#each $tradesQuery.data as t (t.id)}
              <tr class="border-b border-surface-border table-row-hover">
                <td class="px-4 py-2 font-mono text-slate-400">{t.id}</td>
                <td class="px-4 py-2 font-semibold">{t.instrument}</td>
                <td class="px-4 py-2 text-right font-mono
                           {t.units > 0 ? 'text-profit' : 'text-loss'}">
                  {t.units.toLocaleString()}
                </td>
                <td class="px-4 py-2 text-right font-mono">{fmtPrice(t.entryPrice)}</td>
                <td class="px-4 py-2 text-right font-mono text-slate-400">
                  {t.stopLoss > 0 ? fmtPrice(t.stopLoss) : '—'}
                </td>
                <td class="px-4 py-2 text-right font-mono {plClass(t.unrealizedPL)}">
                  {fmtMoney(t.unrealizedPL)}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
</div>
