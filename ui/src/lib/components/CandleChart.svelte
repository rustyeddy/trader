<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    createChart,
    CandlestickSeries,
    createSeriesMarkers,
    CrosshairMode,
    type IChartApi,
    type ISeriesApi,
    type SeriesMarker,
    type Time,
    type CandlestickData,
  } from 'lightweight-charts';
  import type { CandleBar, BacktestReportTrade } from '$lib/api';

  function toChartBars(cs: CandleBar[]): CandlestickData<Time>[] {
    return cs as unknown as CandlestickData<Time>[];
  }

  export let candles: CandleBar[] = [];
  export let trades: BacktestReportTrade[] = [];

  let container: HTMLDivElement;
  let chart: IChartApi | null = null;
  let series: ISeriesApi<'Candlestick'> | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let markersPlugin: any = null;

  function buildMarkers(ts: BacktestReportTrade[]): SeriesMarker<Time>[] {
    const markers: SeriesMarker<Time>[] = [];
    for (const t of ts) {
      const isLong = t.side === 'long';

      if (t.open_time) {
        markers.push({
          time: Math.floor(new Date(t.open_time).getTime() / 1000) as Time,
          position: isLong ? 'belowBar' : 'aboveBar',
          color: isLong ? '#22c55e' : '#ef4444',
          shape: isLong ? 'arrowUp' : 'arrowDown',
          text: 'Entry',
          size: 1,
        });
      }

      if (t.close_time) {
        const pnlStr = t.pnl >= 0 ? `+${t.pnl.toFixed(2)}` : t.pnl.toFixed(2);
        markers.push({
          time: Math.floor(new Date(t.close_time).getTime() / 1000) as Time,
          position: isLong ? 'aboveBar' : 'belowBar',
          color: t.pnl >= 0 ? '#22c55e' : '#ef4444',
          shape: 'circle',
          text: `Exit ${pnlStr}`,
          size: 1,
        });
      }
    }
    return markers.sort((a, b) => (a.time as number) - (b.time as number));
  }

  // Update markers reactively when trades prop changes (may arrive after candles).
  $: if (markersPlugin) {
    markersPlugin.setMarkers(buildMarkers(trades));
  }

  onMount(() => {
    chart = createChart(container, {
      // autoSize fills the container using the library's own ResizeObserver.
      // The container uses absolute positioning so its dimensions are always
      // equal to the parent's rendered size — not a flex h-full percentage.
      autoSize: true,
      layout: {
        background: { color: '#0f172a' },
        textColor: '#94a3b8',
      },
      grid: {
        vertLines: { color: '#1e293b' },
        horzLines: { color: '#1e293b' },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: '#334155' },
      timeScale: {
        borderColor: '#334155',
        timeVisible: true,
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
      upColor: '#22c55e',
      downColor: '#ef4444',
      borderVisible: false,
      wickUpColor: '#22c55e',
      wickDownColor: '#ef4444',
    });

    series.setData(toChartBars(candles));
    markersPlugin = createSeriesMarkers(series, buildMarkers(trades));
    // Note: price lines (stop/TP) intentionally omitted — createPriceLine() is
    // included in Y-axis auto-scale; 100+ trade stop/TP levels collapse the range.
    chart.timeScale().fitContent();
  });

  onDestroy(() => {
    chart?.remove();
    chart = null;
    series = null;
    markersPlugin = null;
  });
</script>

<!-- absolute + inset-0 fills the relatively-positioned parent exactly, bypassing
     any h-full percentage-chain issues in the flex layout above. -->
<div bind:this={container} style="position: absolute; inset: 0;" />
