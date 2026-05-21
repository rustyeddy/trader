<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
  import { api, type OpenTrade, type PlaceOrderProposal } from '$lib/api';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

  const qc = useQueryClient();

  const tradesQuery = createQuery({
    queryKey: ['trades'],
    queryFn: api.trades,
    refetchInterval: 5_000,
    retry: false,
  });

  // ── Selected trade + side panel ───────────────────────────────────────────

  let selected: OpenTrade | null = null;
  let stopInput = '';
  let takeInput = '';

  function select(t: OpenTrade) {
    selected = t;
    stopInput = t.StopLoss > 0 ? t.StopLoss.toFixed(5) : '';
    takeInput = t.TakeProfit > 0 ? t.TakeProfit.toFixed(5) : '';
  }

  function deselect() { selected = null; }

  // ── Update stop/take ───────────────────────────────────────────────────────

  let updateMsg = '';

  const updateMutation = createMutation({
    mutationFn: ({ id, stop, take }: { id: string; stop: number; take: number }) =>
      api.updateStop(id, stop, take),
    onSuccess: () => {
      updateMsg = 'Updated.';
      qc.invalidateQueries({ queryKey: ['trades'] });
      setTimeout(() => { updateMsg = ''; }, 3000);
    },
    onError: (e: Error) => { updateMsg = e.message; },
  });

  function applyStop() {
    if (!selected) return;
    $updateMutation.mutate({
      id: selected.ID,
      stop: parseFloat(stopInput) || 0,
      take: parseFloat(takeInput) || 0,
    });
  }

  // ── Close trade ────────────────────────────────────────────────────────────

  let closeUnits = '';
  let showCloseConfirm = false;
  let closeMsg = '';

  const closeMutation = createMutation({
    mutationFn: ({ id, units }: { id: string; units: number }) =>
      api.closeTrade(id, units),
    onSuccess: () => {
      closeMsg = 'Trade closed.';
      selected = null;
      qc.invalidateQueries({ queryKey: ['trades'] });
    },
    onError: (e: Error) => { closeMsg = e.message; },
  });

  function confirmClose() {
    if (!selected) return;
    $closeMutation.mutate({
      id: selected.ID,
      units: parseInt(closeUnits) || 0,
    });
  }

  // ── Place order ────────────────────────────────────────────────────────────

  let orderInstrument = 'EUR_USD';
  let orderSide = 'long';
  let orderStopPips = 20;
  let orderRiskPct = 0.1;
  let orderPreview: PlaceOrderProposal | null = null;
  let orderMsg = '';

  const previewMutation = createMutation({
    mutationFn: () => api.placeTrade({
      instrument: orderInstrument, side: orderSide,
      stop_pips: orderStopPips, risk_pct: orderRiskPct, confirm: false,
    }),
    onSuccess: (data) => { orderPreview = data.Proposal; orderMsg = ''; },
    onError: (e: Error) => { orderMsg = e.message; },
  });

  const placeMutation = createMutation({
    mutationFn: () => api.placeTrade({
      instrument: orderInstrument, side: orderSide,
      stop_pips: orderStopPips, risk_pct: orderRiskPct, confirm: true,
    }),
    onSuccess: () => {
      orderMsg = 'Order placed!';
      orderPreview = null;
      qc.invalidateQueries({ queryKey: ['trades'] });
      setTimeout(() => { orderMsg = ''; }, 4000);
    },
    onError: (e: Error) => { orderMsg = e.message; orderPreview = null; },
  });

  function cancelPreview() { orderPreview = null; orderMsg = ''; }

  function sideTabClass(tab: string) {
    return orderSide === tab
      ? 'bg-accent text-slate-900 font-semibold'
      : 'text-slate-400 hover:text-slate-200';
  }
  function orderMsgClass() {
    return orderMsg === 'Order placed!' ? 'text-profit' : 'text-loss';
  }

  // ── Formatting ─────────────────────────────────────────────────────────────

  function fmtPrice(n: number) { return n.toFixed(5); }
  function fmtMoney(n: number) {
    return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });
  }
  function plClass(n: number) { return n >= 0 ? 'badge-profit' : 'badge-loss'; }
  function side(units: number) { return units > 0 ? 'LONG' : 'SHORT'; }
  function sideClass(units: number) { return units > 0 ? 'text-profit' : 'text-loss'; }
</script>

<ConfirmDialog
  bind:open={showCloseConfirm}
  title="Close trade"
  message="{closeUnits ? `Close ${closeUnits} units of` : 'Fully close'} trade {selected?.ID} ({selected?.Instrument})?"
  confirmLabel="Close trade"
  danger
  on:confirm={confirmClose}
/>

<div class="flex gap-6 h-full">

  <!-- Trade table -->
  <div class="flex-1 min-w-0">
    <h1 class="text-xl font-semibold text-slate-100 mb-4">Open Trades</h1>

    {#if $tradesQuery.isLoading}
      <div class="card text-slate-500 text-sm">Loading…</div>
    {:else if $tradesQuery.isError}
      <div class="card text-slate-500 text-sm">No OANDA connection.</div>
    {:else if !$tradesQuery.data || $tradesQuery.data.length === 0}
      <div class="card text-slate-500 text-sm">No open trades.</div>
    {:else}
      <div class="card p-0 overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-surface-border text-xs text-slate-400 uppercase">
              <th class="px-4 py-2 text-left">ID</th>
              <th class="px-4 py-2 text-left">Instrument</th>
              <th class="px-4 py-2 text-left">Side</th>
              <th class="px-4 py-2 text-right">Units</th>
              <th class="px-4 py-2 text-right">Entry</th>
              <th class="px-4 py-2 text-right">Stop</th>
              <th class="px-4 py-2 text-right">Take</th>
              <th class="px-4 py-2 text-right">Unreal P/L</th>
            </tr>
          </thead>
          <tbody>
            {#each $tradesQuery.data as t (t.ID)}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <tr
                class="border-b border-surface-border cursor-pointer transition-colors
                       {selected?.ID === t.ID
                         ? 'bg-accent/10'
                         : 'hover:bg-surface-border'}"
                on:click={() => select(t)}
              >
                <td class="px-4 py-2.5 font-mono text-slate-400 text-xs">{t.ID}</td>
                <td class="px-4 py-2.5 font-semibold">{t.Instrument}</td>
                <td class="px-4 py-2.5 font-semibold text-xs {sideClass(t.Units)}">{side(t.Units)}</td>
                <td class="px-4 py-2.5 text-right font-mono">{Math.abs(t.Units).toLocaleString()}</td>
                <td class="px-4 py-2.5 text-right font-mono">{fmtPrice(t.EntryPrice)}</td>
                <td class="px-4 py-2.5 text-right font-mono text-slate-400">
                  {t.StopLoss > 0 ? fmtPrice(t.StopLoss) : '—'}
                </td>
                <td class="px-4 py-2.5 text-right font-mono text-slate-400">
                  {t.TakeProfit > 0 ? fmtPrice(t.TakeProfit) : '—'}
                </td>
                <td class="px-4 py-2.5 text-right font-mono {plClass(t.UnrealizedPL)}">
                  {fmtMoney(t.UnrealizedPL)}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>

  <!-- Right panel: trade detail when selected, new order form otherwise -->
  <div class="w-72 shrink-0 space-y-4">
  {#if selected}
    {@const t = selected}
    <div>
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold text-slate-100">Trade {t.ID}</h2>
        <button on:click={deselect} class="text-slate-500 hover:text-slate-300 text-lg leading-none">✕</button>
      </div>

      <!-- Summary -->
      <div class="card space-y-2 text-sm">
        <div class="flex justify-between">
          <span class="text-slate-400">Instrument</span>
          <span class="font-semibold">{t.Instrument}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-slate-400">Side</span>
          <span class="font-semibold {sideClass(t.Units)}">{side(t.Units)}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-slate-400">Units</span>
          <span class="font-mono">{Math.abs(t.Units).toLocaleString()}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-slate-400">Entry</span>
          <span class="font-mono">{fmtPrice(t.EntryPrice)}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-slate-400">Unreal P/L</span>
          <span class="font-mono {plClass(t.UnrealizedPL)}">{fmtMoney(t.UnrealizedPL)}</span>
        </div>
      </div>

      <!-- Update stop / take -->
      <div class="card space-y-3">
        <h3 class="text-sm font-medium text-slate-300">Update Stop / Take</h3>
        <label class="block">
          <span class="text-xs text-slate-400">Stop price</span>
          <input
            bind:value={stopInput}
            type="number"
            step="0.00001"
            placeholder="0 = cancel"
            class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                   text-sm font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <label class="block">
          <span class="text-xs text-slate-400">Take profit</span>
          <input
            bind:value={takeInput}
            type="number"
            step="0.00001"
            placeholder="0 = cancel"
            class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                   text-sm font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <button
          on:click={applyStop}
          disabled={$updateMutation.isLoading}
          class="w-full py-1.5 rounded bg-accent text-slate-900 text-sm font-semibold
                 hover:bg-accent-dim disabled:opacity-50 transition-colors"
        >
          {$updateMutation.isLoading ? 'Updating…' : 'Apply'}
        </button>
        {#if updateMsg}
          <p class="text-xs {updateMsg === 'Updated.' ? 'text-profit' : 'text-loss'}">{updateMsg}</p>
        {/if}
      </div>

      <!-- Close trade -->
      <div class="card space-y-3">
        <h3 class="text-sm font-medium text-slate-300">Close Trade</h3>
        <label class="block">
          <span class="text-xs text-slate-400">Units (empty = full close)</span>
          <input
            bind:value={closeUnits}
            type="number"
            min="1"
            placeholder="all"
            class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                   text-sm font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <button
          on:click={() => { showCloseConfirm = true; }}
          disabled={$closeMutation.isLoading}
          class="w-full py-1.5 rounded bg-loss/80 hover:bg-loss text-white text-sm font-semibold
                 disabled:opacity-50 transition-colors"
        >
          {$closeMutation.isLoading ? 'Closing…' : 'Close Trade'}
        </button>
        {#if closeMsg}
          <p class="text-xs text-loss">{closeMsg}</p>
        {/if}
      </div>
    </div>

  {:else}

    <!-- New order form -->
    <h2 class="font-semibold text-slate-100">New Order</h2>

    <div class="card space-y-3">
      <label class="block">
        <span class="text-xs text-slate-400">Instrument</span>
        <input
          bind:value={orderInstrument}
          type="text"
          placeholder="EUR_USD"
          class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                 text-sm font-mono uppercase focus:outline-none focus:border-accent"
        />
      </label>

      <div>
        <span class="text-xs text-slate-400">Side</span>
        <div class="mt-1 flex rounded overflow-hidden border border-surface-border">
          <button
            on:click={() => { orderSide = 'long'; orderPreview = null; }}
            class="flex-1 py-1.5 text-sm transition-colors {sideTabClass('long')}"
          >Long</button>
          <button
            on:click={() => { orderSide = 'short'; orderPreview = null; }}
            class="flex-1 py-1.5 text-sm transition-colors {sideTabClass('short')}"
          >Short</button>
        </div>
      </div>

      <label class="block">
        <span class="text-xs text-slate-400">Stop (pips)</span>
        <input
          bind:value={orderStopPips}
          type="number"
          min="1"
          step="1"
          on:change={() => { orderPreview = null; }}
          class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                 text-sm font-mono focus:outline-none focus:border-accent"
        />
      </label>

      <label class="block">
        <span class="text-xs text-slate-400">Risk %</span>
        <input
          bind:value={orderRiskPct}
          type="number"
          min="0.01"
          step="0.01"
          on:change={() => { orderPreview = null; }}
          class="mt-1 w-full bg-surface border border-surface-border rounded px-3 py-1.5
                 text-sm font-mono focus:outline-none focus:border-accent"
        />
      </label>

      {#if !orderPreview}
        <button
          on:click={() => $previewMutation.mutate()}
          disabled={$previewMutation.isLoading}
          class="w-full py-1.5 rounded bg-surface-border hover:bg-slate-600 text-slate-200
                 text-sm font-semibold disabled:opacity-50 transition-colors"
        >
          {$previewMutation.isLoading ? 'Loading…' : 'Preview Order'}
        </button>
      {:else}
        <div class="border border-surface-border rounded p-3 space-y-1.5 text-sm">
          <div class="flex justify-between">
            <span class="text-slate-400">Units</span>
            <span class="font-mono">{Math.abs(orderPreview.Units).toLocaleString()}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-slate-400">Entry</span>
            <span class="font-mono">{orderPreview.EntryPrice.toFixed(5)}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-slate-400">Stop</span>
            <span class="font-mono">{orderPreview.StopPrice.toFixed(5)}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-slate-400">Risk $</span>
            <span class="font-mono">{fmtMoney(orderPreview.RiskAmount)}</span>
          </div>
          <div class="flex justify-between text-xs text-slate-500">
            <span>Account NAV</span>
            <span class="font-mono">{fmtMoney(orderPreview.AccountNAV)}</span>
          </div>
        </div>
        <div class="flex gap-2">
          <button
            on:click={cancelPreview}
            class="flex-1 py-1.5 rounded bg-surface-border text-slate-300 text-sm
                   hover:bg-slate-600 transition-colors"
          >Cancel</button>
          <button
            on:click={() => $placeMutation.mutate()}
            disabled={$placeMutation.isLoading}
            class="flex-1 py-1.5 rounded bg-accent text-slate-900 text-sm font-semibold
                   disabled:opacity-50 transition-colors"
          >
            {$placeMutation.isLoading ? 'Placing…' : 'Place Order'}
          </button>
        </div>
      {/if}

      {#if orderMsg}
        <p class="text-xs {orderMsgClass()}">{orderMsg}</p>
      {/if}
    </div>

  {/if}
  </div>

</div>
