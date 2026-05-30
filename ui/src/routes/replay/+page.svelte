<script lang="ts">
  import { api, type ReplayResult, type ReplaySignal } from '$lib/api';
  import CandleChart from '$lib/components/CandleChart.svelte';

  // ── Form state ───────────────────────────────────────────────────────────────

  const INSTRUMENTS = [
    'EURUSD','GBPUSD','USDJPY','USDCHF','USDCAD',
    'AUDUSD','NZDUSD','EURGBP','EURJPY','GBPJPY','AUDJPY',
  ];

  const STRATEGIES = [
    { value: 'donchian-v6',  label: 'Donchian v6' },
    { value: 'donchian-v5',  label: 'Donchian v5' },
    { value: 'donchian-v4',  label: 'Donchian v4' },
    { value: 'donchian-v3',  label: 'Donchian v3' },
    { value: 'donchian-v2',  label: 'Donchian v2' },
    { value: 'donchian',     label: 'Donchian v1' },
    { value: 'ema-cross',    label: 'EMA Cross' },
    { value: 'bb-fade',      label: 'Bollinger Fade' },
  ];

  const EXIT_STRATEGIES = [
    { value: '',            label: 'None (strategy sets stop)' },
    { value: 'chandelier',  label: 'Chandelier ATR' },
  ];

  const REGIME_FILTERS = [
    { value: '',              label: 'None' },
    { value: 'weekly-ema',    label: 'Weekly EMA' },
    { value: 'atr-percentile', label: 'ATR Percentile' },
    { value: 'adx-d1',        label: 'D1 ADX' },
    { value: 'composite',     label: 'Composite' },
  ];

  // Default to last 6 months
  const today   = new Date().toISOString().slice(0, 10);
  const sixBack = new Date(Date.now() - 180 * 86400_000).toISOString().slice(0, 10);

  let instrument   = 'EURUSD';
  let timeframe    = 'H1';
  let from         = sixBack;
  let to           = today;
  let strategyKind = 'donchian-v6';
  let exitKind     = 'chandelier';
  let exitPeriod   = 14;
  let exitMult     = 3.0;
  let regimeKind   = '';
  let warmupBars   = 200;

  // ── Run state ────────────────────────────────────────────────────────────────

  let running = false;
  let error   = '';
  let result: ReplayResult | null = null;

  async function runReplay() {
    running = true;
    error   = '';
    result  = null;
    try {
      result = await api.replay({
        instrument,
        timeframe,
        from,
        to,
        warmup_bars: warmupBars,
        strategy: { kind: strategyKind },
        exit: exitKind
          ? { kind: exitKind, params: { period: exitPeriod, multiplier: exitMult } }
          : { kind: '' },
        regime: { kind: regimeKind },
      });
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      running = false;
    }
  }

  // ── Signal summary ───────────────────────────────────────────────────────────

  function summarise(sigs: ReplaySignal[]) {
    const counts: Record<string, number> = {};
    for (const s of sigs) counts[s.kind] = (counts[s.kind] ?? 0) + 1;
    return counts;
  }

  $: summary = result ? summarise(result.signals) : null;
  $: signals = result?.signals ?? [];
  $: candles = result?.bars    ?? [];
</script>

<div class="flex flex-col gap-4 h-full">

  <!-- ── Controls ─────────────────────────────────────────────────────────── -->
  <div class="flex flex-wrap items-end gap-3">
    <h1 class="text-xl font-semibold text-slate-100 shrink-0 self-center">Replay</h1>

    <!-- Instrument -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Instrument
      <select bind:value={instrument}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent">
        {#each INSTRUMENTS as inst}
          <option value={inst}>{inst}</option>
        {/each}
      </select>
    </label>

    <!-- Timeframe -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Timeframe
      <select bind:value={timeframe}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent">
        <option value="H1">H1</option>
        <option value="D">D1</option>
      </select>
    </label>

    <!-- From / To -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      From
      <input type="date" bind:value={from}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent" />
    </label>
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      To
      <input type="date" bind:value={to}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent" />
    </label>

    <!-- Strategy -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Strategy
      <select bind:value={strategyKind}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent">
        {#each STRATEGIES as s}
          <option value={s.value}>{s.label}</option>
        {/each}
      </select>
    </label>

    <!-- Exit -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Exit
      <select bind:value={exitKind}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent">
        {#each EXIT_STRATEGIES as e}
          <option value={e.value}>{e.label}</option>
        {/each}
      </select>
    </label>

    <!-- Chandelier params (only shown when exit = chandelier) -->
    {#if exitKind === 'chandelier'}
      <label class="flex flex-col gap-0.5 text-xs text-slate-400">
        ATR Period
        <input type="number" bind:value={exitPeriod} min="1" max="50"
          class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
                 w-20 focus:outline-none focus:ring-1 focus:ring-accent" />
      </label>
      <label class="flex flex-col gap-0.5 text-xs text-slate-400">
        Multiplier
        <input type="number" bind:value={exitMult} min="0.5" max="10" step="0.5"
          class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
                 w-20 focus:outline-none focus:ring-1 focus:ring-accent" />
      </label>
    {/if}

    <!-- Regime -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Regime
      <select bind:value={regimeKind}
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               focus:outline-none focus:ring-1 focus:ring-accent">
        {#each REGIME_FILTERS as r}
          <option value={r.value}>{r.label}</option>
        {/each}
      </select>
    </label>

    <!-- Warmup -->
    <label class="flex flex-col gap-0.5 text-xs text-slate-400">
      Warmup bars
      <input type="number" bind:value={warmupBars} min="0" max="5000"
        class="bg-slate-800 border border-slate-600 text-slate-100 text-sm rounded px-2 py-1.5
               w-24 focus:outline-none focus:ring-1 focus:ring-accent" />
    </label>

    <!-- Run button -->
    <button
      on:click={runReplay}
      disabled={running}
      class="px-4 py-2 rounded bg-accent text-white text-sm font-medium self-end
             hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
      {running ? 'Running…' : '▶ Run Replay'}
    </button>
  </div>

  <!-- ── Error ─────────────────────────────────────────────────────────────── -->
  {#if error}
    <div class="text-red-400 text-sm bg-red-950/30 border border-red-900 rounded px-3 py-2">
      {error}
    </div>
  {/if}

  <!-- ── Signal summary ────────────────────────────────────────────────────── -->
  {#if summary}
    <div class="flex gap-4 text-xs text-slate-400 flex-wrap">
      <span>Bars: <span class="text-slate-200">{result?.bars.length}</span></span>
      {#if summary.open}
        <span>Entries: <span class="text-green-400">{summary.open}</span></span>
      {/if}
      {#if summary.close}
        <span>Exits: <span class="text-slate-200">{summary.close}</span></span>
      {/if}
      {#if summary.blocked}
        <span>Blocked: <span class="text-yellow-400">{summary.blocked}</span></span>
      {/if}
      {#if summary.no_stop}
        <span>No-stop drops: <span class="text-orange-400">{summary.no_stop}</span></span>
      {/if}
      {#if summary.stop_update}
        <span>Stop updates: <span class="text-orange-300">{summary.stop_update}</span></span>
      {/if}
      {#if result}
        <span class="text-slate-500">
          {result.strategy} · {result.instrument} · {result.from} → {result.to}
        </span>
      {/if}
    </div>
  {/if}

  <!-- ── Chart ─────────────────────────────────────────────────────────────── -->
  <div class="relative flex-1 min-h-[480px] rounded-lg overflow-hidden bg-[#0f172a]">
    {#if running}
      <div class="absolute inset-0 flex items-center justify-center text-slate-400 text-sm animate-pulse">
        Running replay…
      </div>
    {:else if !result}
      <div class="absolute inset-0 flex items-center justify-center text-slate-500 text-sm">
        Configure a strategy above and click Run Replay
      </div>
    {:else if candles.length === 0}
      <div class="absolute inset-0 flex items-center justify-center text-slate-500 text-sm">
        No candle data found for {instrument} {from}→{to}
      </div>
    {:else}
      <!-- key forces chart remount when result changes so series data resets cleanly -->
      {#key result}
        <CandleChart {candles} {signals} />
      {/key}
    {/if}
  </div>

  <!-- ── Legend ─────────────────────────────────────────────────────────────── -->
  {#if result}
    <div class="flex gap-5 text-xs text-slate-500 flex-wrap">
      <span><span class="text-green-400">▲</span> Long entry</span>
      <span><span class="text-red-400">▼</span> Short entry</span>
      <span><span class="text-slate-400">● ✕</span> Exit</span>
      <span><span class="text-yellow-400">■ ⊘</span> Regime blocked</span>
      <span><span class="text-orange-400">■</span> No-stop dropped</span>
      <span><span class="text-orange-400">— —</span> Stop trail</span>
    </div>
  {/if}
</div>
