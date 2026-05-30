<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    createChart,
    CandlestickSeries,
    LineSeries,
    createSeriesMarkers,
    CrosshairMode,
    type IChartApi,
    type ISeriesApi,
    type SeriesMarker,
    type Time,
    type CandlestickData,
    type LineData,
  } from 'lightweight-charts';
  import type { CandleBar, BacktestReportTrade, ReplaySignal } from '$lib/api';

  function toChartBars(cs: CandleBar[]): CandlestickData<Time>[] {
    return cs as unknown as CandlestickData<Time>[];
  }

  export let candles: CandleBar[]           = [];
  export let trades:  BacktestReportTrade[] = [];
  export let signals: ReplaySignal[]        = [];

  let container: HTMLDivElement;
  let chart:       IChartApi | null = null;
  let series:      ISeriesApi<'Candlestick'> | null = null;
  let stopSeries:  ISeriesApi<'Line'> | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let markersPlugin: any = null;

  // ── Marker builders ─────────────────────────────────────────────────────────

  function buildTradeMarkers(ts: BacktestReportTrade[]): SeriesMarker<Time>[] {
    const out: SeriesMarker<Time>[] = [];
    for (const t of ts) {
      const isLong = t.side === 'long';
      if (t.open_time) {
        out.push({
          time:     Math.floor(new Date(t.open_time).getTime() / 1000) as Time,
          position: isLong ? 'belowBar' : 'aboveBar',
          color:    isLong ? '#22c55e' : '#ef4444',
          shape:    isLong ? 'arrowUp' : 'arrowDown',
          text:     'Entry',
          size:     1,
        });
      }
      if (t.close_time) {
        const pnlStr = t.pnl >= 0 ? `+${t.pnl.toFixed(2)}` : t.pnl.toFixed(2);
        out.push({
          time:     Math.floor(new Date(t.close_time).getTime() / 1000) as Time,
          position: isLong ? 'aboveBar' : 'belowBar',
          color:    t.pnl >= 0 ? '#22c55e' : '#ef4444',
          shape:    'circle',
          text:     `Exit ${pnlStr}`,
          size:     1,
        });
      }
    }
    return out;
  }

  function buildSignalMarkers(sigs: ReplaySignal[]): SeriesMarker<Time>[] {
    const out: SeriesMarker<Time>[] = [];
    for (const s of sigs) {
      const t = s.time as Time;
      const isLong = s.side !== 'short';
      switch (s.kind) {
        case 'open':
          out.push({
            time:     t,
            position: isLong ? 'belowBar' : 'aboveBar',
            color:    isLong ? '#22c55e' : '#ef4444',
            shape:    isLong ? 'arrowUp' : 'arrowDown',
            text:     `${isLong ? '▲' : '▼'} ${s.stop_pips ? s.stop_pips.toFixed(1) + 'p' : ''}`,
            size:     1,
          });
          break;
        case 'close':
          out.push({
            time:     t,
            position: isLong ? 'aboveBar' : 'belowBar',
            color:    '#94a3b8',
            shape:    'circle',
            text:     '✕',
            size:     1,
          });
          break;
        case 'blocked':
          out.push({
            time:     t,
            position: 'aboveBar',
            color:    '#eab308',
            shape:    'square',
            text:     '⊘',
            size:     1,
          });
          break;
        case 'no_stop':
          out.push({
            time:     t,
            position: 'aboveBar',
            color:    '#f97316',
            shape:    'square',
            text:     '?stop',
            size:     1,
          });
          break;
        // stop_update: handled via the stop trail line — no marker needed
      }
    }
    return out;
  }

  function allMarkers(): SeriesMarker<Time>[] {
    const combined = [
      ...buildTradeMarkers(trades),
      ...buildSignalMarkers(signals),
    ];
    return combined.sort((a, b) => (a.time as number) - (b.time as number));
  }

  // ── Stop trail ───────────────────────────────────────────────────────────────

  // WhitespaceData in LWC v5 is just { time: Time } with no value field.
  type StopPoint = LineData<Time> | { time: Time };

  function buildStopTrail(sigs: ReplaySignal[]): StopPoint[] {
    const pts: StopPoint[] = [];
    let inPos   = false;
    let lastStop = 0;

    for (const s of sigs) {
      const t = s.time as Time;
      if (s.kind === 'open' && s.stop_price) {
        inPos    = true;
        lastStop = s.stop_price;
        pts.push({ time: t, value: lastStop });
      } else if (s.kind === 'stop_update' && inPos && s.stop_price) {
        lastStop = s.stop_price;
        pts.push({ time: t, value: lastStop });
      } else if (s.kind === 'close' && inPos) {
        pts.push({ time: t, value: lastStop });
        // Whitespace break so the trail doesn't connect to the next position.
        pts.push({ time: (s.time + 1) as Time });
        inPos    = false;
        lastStop = 0;
      }
    }
    return pts;
  }

  // ── Reactive updates ─────────────────────────────────────────────────────────

  $: if (markersPlugin) {
    markersPlugin.setMarkers(allMarkers());
  }

  $: if (stopSeries && signals.length > 0) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    stopSeries.setData(buildStopTrail(signals) as any);
  } else if (stopSeries) {
    stopSeries.setData([]);
  }

  // ── Mount / destroy ──────────────────────────────────────────────────────────

  onMount(() => {
    chart = createChart(container, {
      autoSize: true,
      layout: {
        background: { color: '#0f172a' },
        textColor:  '#94a3b8',
      },
      grid: {
        vertLines: { color: '#1e293b' },
        horzLines: { color: '#1e293b' },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: '#334155' },
      timeScale: {
        borderColor:    '#334155',
        timeVisible:    true,
        secondsVisible: false,
      },
      handleScale: {
        // Disable double-click-to-reset on both axes. The default is true, and
        // accidentally double-clicking the time axis calls restoreDefault() which
        // resets bar spacing to 6 px/bar — compressing a year of H1 data to a
        // barely-visible sliver.
        axisDoubleClickReset: { time: false, price: false },
      },
    });

    series = chart.addSeries(CandlestickSeries, {
      upColor:      '#22c55e',
      downColor:    '#ef4444',
      borderVisible: false,
      wickUpColor:   '#22c55e',
      wickDownColor: '#ef4444',
    });
    series.setData(toChartBars(candles));

    stopSeries = chart.addSeries(LineSeries, {
      color:      '#f97316',
      lineWidth:  1,
      lineStyle:  2, // dashed
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    stopSeries.setData(buildStopTrail(signals) as any);

    markersPlugin = createSeriesMarkers(series, allMarkers());

    chart.timeScale().fitContent();
  });

  onDestroy(() => {
    chart?.remove();
    chart        = null;
    series       = null;
    stopSeries   = null;
    markersPlugin = null;
  });
</script>

<!-- absolute + inset-0 fills the relatively-positioned parent exactly -->
<div bind:this={container} style="position: absolute; inset: 0;" />
