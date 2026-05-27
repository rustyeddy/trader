<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { api, type BacktestSummary } from '$lib/api';
  import CandleChart from '$lib/components/CandleChart.svelte';

  let selectedName = '';

  const listQuery = createQuery({
    queryKey: ['backtests'],
    queryFn: () => api.backtestList(),
    staleTime: 30_000,
  });

  $: summaries = ($listQuery.data?.summaries ?? []) as BacktestSummary[];

  // $: re-creates each query when selectedName changes so enabled/queryKey
  // are re-evaluated — createQuery captures options once at call time.
  $: detailQuery = createQuery({
    queryKey: ['backtests', selectedName],
    queryFn: () => api.backtestGet(selectedName),
    enabled: !!selectedName,
    staleTime: 60_000,
  });

  $: candleQuery = createQuery({
    queryKey: ['backtests-candles', selectedName],
    queryFn: () => api.backtestCandles(selectedName),
    enabled: !!selectedName,
    staleTime: 300_000,
  });

  $: detail   = $detailQuery?.data ?? null;
  $: candles  = $candleQuery?.data?.bars ?? [];
  $: trades   = detail?.trade_details ?? [];
  $: loading  = ($candleQuery?.isLoading ?? false) || ($detailQuery?.isLoading ?? false);
</script>

<div class="flex flex-col gap-4 h-full">
  <!-- Selector row -->
  <div class="flex items-center gap-3">
    <h1 class="text-xl font-semibold text-slate-100 shrink-0">Charts</h1>

    {#if $listQuery.isLoading}
      <span class="text-slate-400 text-sm">Loading…</span>
    {:else if $listQuery.isError}
      <span class="text-red-400 text-sm">Failed to load backtests</span>
    {:else}
      <select
        bind:value={selectedName}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-3 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent min-w-[28rem]"
      >
        <option value="">— select a backtest —</option>
        {#each summaries as s}
          <option value={s.name}>
            {s.instrument} · {s.strategy} · {s.start?.slice(0, 10)} → {s.end?.slice(0, 10)}
            ({s.trades} trades, {s.win_rate?.toFixed(1)}% WR)
          </option>
        {/each}
      </select>
    {/if}

    {#if selectedName && loading}
      <span class="text-slate-400 text-sm animate-pulse">Loading data…</span>
    {/if}
  </div>

  <!-- Stats bar -->
  {#if detail && selectedName}
    <div class="flex gap-4 text-xs text-slate-400 flex-wrap">
      <span>Balance: <span class="text-slate-200">${detail.end_balance?.toFixed(2)}</span></span>
      <span>Net P/L: <span class="{detail.net_pl >= 0 ? 'text-green-400' : 'text-red-400'}">${detail.net_pl?.toFixed(2)}</span></span>
      <span>Return: <span class="{detail.return_pct >= 0 ? 'text-green-400' : 'text-red-400'}">{detail.return_pct?.toFixed(2)}%</span></span>
      <span>Trades: <span class="text-slate-200">{detail.trades}</span></span>
      <span>Win rate: <span class="text-slate-200">{detail.win_rate?.toFixed(1)}%</span></span>
      <span>R:R: <span class="text-slate-200">{detail.rr?.toFixed(2)}</span></span>
      <span>Max DD: <span class="text-red-400">${detail.max_drawdown?.toFixed(2)}</span></span>
    </div>
  {/if}

  <!-- Chart area -->
  <div class="relative flex-1 min-h-[480px] rounded-lg overflow-hidden bg-[#0f172a]">
    {#if !selectedName}
      <div class="absolute inset-0 flex items-center justify-center text-slate-500 text-sm">
        Select a backtest above to view its chart
      </div>
    {:else if loading}
      <div class="absolute inset-0 flex items-center justify-center text-slate-400 text-sm animate-pulse">
        Loading chart data…
      </div>
    {:else if $candleQuery?.isError}
      <div class="absolute inset-0 flex items-center justify-center text-red-400 text-sm">
        Failed to load candle data
      </div>
    {:else if candles.length === 0}
      <div class="absolute inset-0 flex items-center justify-center text-slate-500 text-sm">
        No candle data found for this backtest period
      </div>
    {:else}
      {#key selectedName}
        <CandleChart {candles} {trades} />
      {/key}
    {/if}
  </div>
</div>
