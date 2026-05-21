<script lang="ts">
  import { createQuery, createMutation } from '@tanstack/svelte-query';
  import { api, type BacktestSummary, type BacktestReportTrade } from '$lib/api';

  // ── List + filters ────────────────────────────────────────────────────────

  let filterInstrument = '';
  let filterStrategy = '';
  let sortKey: keyof BacktestSummary = 'name';
  let sortAsc = true;

  $: listParams = new URLSearchParams({
    ...(filterInstrument ? { instrument: filterInstrument } : {}),
    ...(filterStrategy   ? { strategy:   filterStrategy }   : {}),
  }).toString();

  const listQuery = createQuery({
    queryKey: ['backtests', filterInstrument, filterStrategy],
    queryFn: () => api.backtestList(filterInstrument, filterStrategy),
    staleTime: 30_000,
  });

  $: sorted = [...($listQuery.data?.summaries ?? [])].sort((a, b) => {
    const av = a[sortKey] ?? '';
    const bv = b[sortKey] ?? '';
    const cmp = typeof av === 'number'
      ? (av as number) - (bv as number)
      : String(av).localeCompare(String(bv));
    return sortAsc ? cmp : -cmp;
  });

  function setSort(key: string) {
    const k = key as keyof BacktestSummary;
    if (sortKey === k) { sortAsc = !sortAsc; } else { sortKey = k; sortAsc = false; }
  }

  function sortIcon(key: string) {
    if (sortKey !== (key as keyof BacktestSummary)) return '↕';
    return sortAsc ? '↑' : '↓';
  }

  // ── Detail panel ──────────────────────────────────────────────────────────

  let selectedName = '';

  const detailQuery = createQuery({
    queryKey: ['backtests', selectedName],
    queryFn: () => api.backtestGet(selectedName),
    enabled: !!selectedName,
    staleTime: 60_000,
  });

  $: detail = $detailQuery.data ?? null;
  $: trades = (detail?.trade_details ?? []) as BacktestReportTrade[];
  $: tradesPage = 0;
  $: tradePageSize = 25;
  $: tradePage = trades.slice(tradesPage * tradePageSize, (tradesPage + 1) * tradePageSize);
  $: totalTradePages = Math.ceil(trades.length / tradePageSize);

  function selectRow(name: string) {
    selectedName = selectedName === name ? '' : name;
    tradesPage = 0;
  }

  // ── Compare mode ──────────────────────────────────────────────────────────

  let compareA = '';
  let compareB = '';
  let showCompare = false;

  const compareAQuery = createQuery({
    queryKey: ['backtests', compareA],
    queryFn: () => api.backtestGet(compareA),
    enabled: showCompare && !!compareA,
    staleTime: 60_000,
  });
  const compareBQuery = createQuery({
    queryKey: ['backtests', compareB],
    queryFn: () => api.backtestGet(compareB),
    enabled: showCompare && !!compareB,
    staleTime: 60_000,
  });

  // ── Run panel ─────────────────────────────────────────────────────────────

  let runInput = 'testdata/configs/eurusd-h1-2024-ema-cross.yml';
  let runError = '';
  let runResults: BacktestSummary[] = [];

  const runMutation = createMutation({
    mutationFn: (paths: string[]) => api.runBacktest(paths),
    onSuccess: (data) => { runResults = data.summaries ?? []; runError = ''; },
    onError: (e: Error) => { runError = e.message; },
  });

  function runBacktest() {
    const paths = runInput.split('\n').map(s => s.trim()).filter(Boolean);
    if (paths.length) $runMutation.mutate(paths);
  }

  $: compareBtnClass = showCompare
    ? 'bg-accent/10 text-accent border-accent'
    : 'text-slate-400 hover:text-slate-100';

  // ── Formatting ─────────────────────────────────────────────────────────────

  function pct(n: number) { return n.toFixed(1) + '%'; }
  function money(n: number) {
    return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 0 });
  }
  function price(n: number) { return n.toFixed(5); }
  function plClass(n: number) { return n >= 0 ? 'badge-profit' : 'badge-loss'; }
  function shortDate(s: string) { return s ? s.slice(0, 10) : '—'; }
  function rowClass(name: string) {
    return selectedName === name ? 'bg-accent/10' : 'hover:bg-surface-border';
  }
  function selectVal(e: Event) { return (e.target as HTMLSelectElement).value; }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h1 class="text-xl font-semibold text-slate-100">Backtests</h1>
    <div class="flex gap-2">
      <button
        on:click={() => { showCompare = !showCompare; }}
        class="px-3 py-1.5 rounded text-sm border border-surface-border transition-colors {compareBtnClass}"
      >
        Compare
      </button>
    </div>
  </div>

  <!-- Compare panel -->
  {#if showCompare}
    <div class="card space-y-3">
      <h2 class="text-sm font-medium text-slate-300">Side-by-Side Comparison</h2>
      <div class="grid grid-cols-2 gap-4">
        {#each [{label:'A', val: compareA, set: (v)=>{compareA=v}},
                {label:'B', val: compareB, set: (v)=>{compareB=v}}] as col}
          <div>
            <label class="text-xs text-slate-400">Report {col.label}</label>
            <select
              class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
              value={col.val}
              on:change={(e) => col.set(selectVal(e))}
            >
              <option value="">— select —</option>
              {#each sorted as s}
                <option value={s.name}>{s.name}</option>
              {/each}
            </select>
          </div>
        {/each}
      </div>

      {#if $compareAQuery.data && $compareBQuery.data}
        {@const A = $compareAQuery.data}
        {@const B = $compareBQuery.data}
        {@const rows = [
          ['Strategy',    A.strategy,              B.strategy],
          ['Instrument',  A.instrument,             B.instrument],
          ['Period',      `${shortDate(A.start)} → ${shortDate(A.end)}`, `${shortDate(B.start)} → ${shortDate(B.end)}`],
          ['Trades',      A.trades,                 B.trades],
          ['Win Rate',    pct(A.win_rate),           pct(B.win_rate)],
          ['Return',      pct(A.return_pct),         pct(B.return_pct)],
          ['Net P/L',     money(A.net_pl),           money(B.net_pl)],
          ['Max Drawdown',money(A.max_drawdown),     money(B.max_drawdown)],
          ['RR',          A.rr.toFixed(2),           B.rr.toFixed(2)],
          ['Stop',        A.stop || '—',             B.stop || '—'],
        ]}
        <table class="w-full text-sm mt-3">
          <thead>
            <tr class="border-b border-surface-border text-xs text-slate-400 uppercase">
              <th class="py-1.5 text-left w-32">Metric</th>
              <th class="py-1.5 text-right">{compareA}</th>
              <th class="py-1.5 text-right">{compareB}</th>
            </tr>
          </thead>
          <tbody>
            {#each rows as [label, va, vb]}
              <tr class="border-b border-surface-border">
                <td class="py-1.5 text-slate-400 text-xs">{label}</td>
                <td class="py-1.5 text-right font-mono">{va}</td>
                <td class="py-1.5 text-right font-mono">{vb}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}

  <!-- Filters -->
  <div class="flex gap-3 items-end flex-wrap">
    <div>
      <label class="block text-xs text-slate-400 mb-1">Instrument</label>
      <input bind:value={filterInstrument} placeholder="e.g. EURUSD"
        class="bg-surface border border-surface-border rounded px-3 py-1.5 text-sm
               focus:outline-none focus:border-accent w-36" />
    </div>
    <div>
      <label class="block text-xs text-slate-400 mb-1">Strategy</label>
      <input bind:value={filterStrategy} placeholder="e.g. EMA"
        class="bg-surface border border-surface-border rounded px-3 py-1.5 text-sm
               focus:outline-none focus:border-accent w-40" />
    </div>
    <span class="text-xs text-slate-500 pb-1.5">
      {sorted.length} result{sorted.length !== 1 ? 's' : ''}
    </span>
  </div>

  <!-- List + detail side by side -->
  <div class="flex gap-6 min-h-0">

    <!-- List table -->
    <div class="flex-1 min-w-0">
      {#if $listQuery.isLoading}
        <div class="card text-slate-500 text-sm">Loading…</div>
      {:else if $listQuery.isError}
        <div class="card text-loss text-sm">{$listQuery.error?.message}</div>
      {:else}
        <div class="card p-0 overflow-hidden overflow-x-auto">
          <table class="w-full text-sm whitespace-nowrap">
            <thead>
              <tr class="border-b border-surface-border text-xs text-slate-400 uppercase">
                {#each [
                  ['name',       'Name'],
                  ['instrument', 'Instr'],
                  ['timeframe',  'TF'],
                  ['trades',     'Trades'],
                  ['win_rate',   'Win%'],
                  ['return_pct', 'Return'],
                  ['net_pl',     'Net P/L'],
                  ['max_drawdown','Drawdown'],
                  ['rr',         'RR'],
                ] as [key, label]}
                  <th
                    class="px-3 py-2 text-left cursor-pointer hover:text-slate-100 select-none"
                    on:click={() => setSort(key)}
                  >
                    {label} <span class="text-slate-600">{sortIcon(key)}</span>
                  </th>
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each sorted as s (s.name)}
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <tr
                  class="border-b border-surface-border cursor-pointer transition-colors {rowClass(s.name)}"
                  on:click={() => selectRow(s.name)}
                >
                  <td class="px-3 py-2 font-mono text-xs text-slate-300 max-w-[14rem] truncate">{s.name}</td>
                  <td class="px-3 py-2 font-semibold">{s.instrument}</td>
                  <td class="px-3 py-2 text-slate-400">{s.timeframe}</td>
                  <td class="px-3 py-2 text-right font-mono">{s.trades}</td>
                  <td class="px-3 py-2 text-right font-mono">{pct(s.win_rate)}</td>
                  <td class="px-3 py-2 text-right font-mono {plClass(s.return_pct)}">{pct(s.return_pct)}</td>
                  <td class="px-3 py-2 text-right font-mono {plClass(s.net_pl)}">{money(s.net_pl)}</td>
                  <td class="px-3 py-2 text-right font-mono text-loss">{money(s.max_drawdown)}</td>
                  <td class="px-3 py-2 text-right font-mono">{s.rr.toFixed(2)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <!-- Detail panel -->
    {#if selectedName && detail}
      <div class="w-80 shrink-0 space-y-4 overflow-y-auto max-h-[80vh]">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold text-slate-100 truncate">{detail.name}</h2>
          <button on:click={() => { selectedName = ''; }}
            class="text-slate-500 hover:text-slate-300 shrink-0">✕</button>
        </div>

        <!-- Summary cards -->
        <div class="grid grid-cols-2 gap-2">
          {#each [
            ['Strategy', detail.strategy],
            ['Instrument', detail.instrument],
            ['Period', `${shortDate(detail.start)} → ${shortDate(detail.end)}`],
            ['Stop', detail.stop || '—'],
          ] as [label, val]}
            <div class="card py-2">
              <div class="text-xs text-slate-400">{label}</div>
              <div class="text-xs font-semibold truncate" title={String(val)}>{val}</div>
            </div>
          {/each}
        </div>

        <div class="grid grid-cols-2 gap-2">
          {#each [
            ['Trades',     detail.trades,             ''],
            ['Win Rate',   pct(detail.win_rate),       plClass(detail.win_rate - 50)],
            ['Return',     pct(detail.return_pct),     plClass(detail.return_pct)],
            ['Net P/L',    money(detail.net_pl),       plClass(detail.net_pl)],
            ['Drawdown',   money(detail.max_drawdown), 'text-loss'],
            ['RR',         detail.rr.toFixed(2),       ''],
            ['Avg Winner', money(detail.avg_winner),   'text-profit'],
            ['Avg Loser',  money(detail.avg_loser),    'text-loss'],
          ] as [label, val, cls]}
            <div class="card py-2">
              <div class="text-xs text-slate-400">{label}</div>
              <div class="text-sm font-mono font-semibold {cls}">{val}</div>
            </div>
          {/each}
        </div>

        <!-- Download org -->
        <a
          href="/api/v1/backtests/{detail.name}/org"
          download="{detail.name}.org"
          class="block text-center py-1.5 rounded border border-surface-border text-xs
                 text-slate-400 hover:text-slate-100 hover:border-slate-500 transition-colors"
        >
          Download .org report
        </a>

        <!-- Trade list -->
        {#if trades.length > 0}
          <div>
            <div class="text-xs text-slate-400 mb-2">
              Trades ({trades.length})
              {#if totalTradePages > 1}
                — page {tradesPage + 1}/{totalTradePages}
              {/if}
            </div>
            <div class="card p-0 overflow-hidden">
              <table class="w-full text-xs">
                <thead>
                  <tr class="border-b border-surface-border text-slate-400 uppercase">
                    <th class="px-2 py-1.5 text-left">Side</th>
                    <th class="px-2 py-1.5 text-right">Entry</th>
                    <th class="px-2 py-1.5 text-right">Exit</th>
                    <th class="px-2 py-1.5 text-right">P/L</th>
                  </tr>
                </thead>
                <tbody>
                  {#each tradePage as t (t.id)}
                    <tr class="border-b border-surface-border">
                      <td class="px-2 py-1 {t.side === 'long' ? 'text-profit' : 'text-loss'} uppercase">{t.side}</td>
                      <td class="px-2 py-1 font-mono text-right">{price(t.open_price)}</td>
                      <td class="px-2 py-1 font-mono text-right">{price(t.close_price)}</td>
                      <td class="px-2 py-1 font-mono text-right {plClass(t.pnl)}">{money(t.pnl)}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            {#if totalTradePages > 1}
              <div class="flex justify-between mt-2">
                <button
                  on:click={() => { tradesPage = Math.max(0, tradesPage - 1); }}
                  disabled={tradesPage === 0}
                  class="text-xs text-slate-400 hover:text-slate-100 disabled:opacity-30"
                >← Prev</button>
                <button
                  on:click={() => { tradesPage = Math.min(totalTradePages - 1, tradesPage + 1); }}
                  disabled={tradesPage >= totalTradePages - 1}
                  class="text-xs text-slate-400 hover:text-slate-100 disabled:opacity-30"
                >Next →</button>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {/if}

  </div>

  <!-- Run panel -->
  <details class="card">
    <summary class="cursor-pointer text-sm font-medium text-slate-300 select-none">Run new backtest</summary>
    <div class="mt-3 space-y-3">
      <textarea bind:value={runInput} rows="3" placeholder="One config path per line"
        class="w-full bg-surface border border-surface-border rounded px-3 py-2 text-sm
               font-mono text-slate-200 placeholder-slate-600 resize-none
               focus:outline-none focus:border-accent"
      ></textarea>
      <button on:click={runBacktest} disabled={$runMutation.isLoading}
        class="px-4 py-2 rounded bg-accent text-slate-900 text-sm font-semibold
               hover:bg-accent-dim disabled:opacity-50 transition-colors">
        {$runMutation.isLoading ? 'Running…' : 'Run'}
      </button>
      {#if runError}<p class="text-loss text-sm">{runError}</p>{/if}
      {#if runResults.length > 0}
        <div class="space-y-2">
          {#each runResults as s (s.name)}
            <div class="bg-surface rounded p-3 text-sm">
              <div class="font-semibold">{s.name}</div>
              <div class="text-xs text-slate-400 mt-1">
                {s.trades} trades · {pct(s.win_rate)} win · {pct(s.return_pct)} return · {money(s.net_pl)} P/L
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </details>
</div>
