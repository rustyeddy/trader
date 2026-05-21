<script lang="ts">
  import { createMutation } from '@tanstack/svelte-query';
  import { api, type BacktestSummary } from '$lib/api';

  let configInput = 'testdata/configs/eurusd-h1-2024-ema-cross.yml';
  let results: BacktestSummary[] = [];
  let errorMsg = '';

  const runMutation = createMutation({
    mutationFn: (paths: string[]) => api.runBacktest(paths),
    onSuccess: (data) => {
      results = data.summaries ?? [];
      errorMsg = '';
    },
    onError: (err: Error) => {
      errorMsg = err.message;
    },
  });

  function run() {
    const paths = configInput.split('\n').map(s => s.trim()).filter(Boolean);
    if (!paths.length) return;
    $runMutation.mutate(paths);
  }

  function fmtPct(n: number) { return (n * 100).toFixed(1) + '%'; }
  function fmtMoney(n: number) {
    return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });
  }
  function plClass(n: number) { return n >= 0 ? 'badge-profit' : 'badge-loss'; }
</script>

<div class="space-y-6">
  <h1 class="text-xl font-semibold text-slate-100">Backtests</h1>

  <!-- Run panel -->
  <div class="card space-y-3">
    <h2 class="text-sm font-medium text-slate-300">Run configs</h2>
    <textarea
      bind:value={configInput}
      rows="3"
      placeholder="One config path per line"
      class="w-full bg-surface border border-surface-border rounded px-3 py-2
             text-sm font-mono text-slate-200 placeholder-slate-600 resize-none
             focus:outline-none focus:border-accent"
    ></textarea>
    <button
      on:click={run}
      disabled={$runMutation.isLoading}
      class="px-4 py-2 rounded bg-accent text-slate-900 text-sm font-semibold
             hover:bg-accent-dim disabled:opacity-50 transition-colors"
    >
      {$runMutation.isLoading ? 'Running…' : 'Run Backtest'}
    </button>
    {#if errorMsg}
      <p class="text-loss text-sm">{errorMsg}</p>
    {/if}
  </div>

  <!-- Results -->
  {#if results.length > 0}
    <section>
      <h2 class="text-sm font-medium text-slate-400 uppercase tracking-wider mb-3">
        Results ({results.length})
      </h2>
      <div class="space-y-3">
        {#each results as s (s.name)}
          <div class="card">
            <div class="flex items-start justify-between mb-3">
              <div>
                <div class="font-semibold text-slate-100">{s.name}</div>
                <div class="text-xs text-slate-400">
                  {s.instrument} · {s.timeframe} · {s.startDate} → {s.endDate}
                </div>
              </div>
              <span class="text-lg font-mono font-bold {plClass(s.netPnL)}">
                {fmtMoney(s.netPnL)}
              </span>
            </div>
            <div class="grid grid-cols-4 gap-3 text-sm">
              <div>
                <div class="text-xs text-slate-400">Trades</div>
                <div class="font-mono">{s.totalTrades}</div>
              </div>
              <div>
                <div class="text-xs text-slate-400">Win Rate</div>
                <div class="font-mono">{fmtPct(s.winRate)}</div>
              </div>
              <div>
                <div class="text-xs text-slate-400">Max Drawdown</div>
                <div class="font-mono {plClass(-s.maxDrawdown)}">{fmtPct(s.maxDrawdown)}</div>
              </div>
              <div>
                <div class="text-xs text-slate-400">Sharpe</div>
                <div class="font-mono">{s.sharpeRatio.toFixed(2)}</div>
              </div>
            </div>
          </div>
        {/each}
      </div>
    </section>
  {/if}
</div>
