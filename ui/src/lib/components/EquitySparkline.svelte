<script lang="ts">
  export let values: number[] = [];
  export let width = 300;
  export let height = 60;
  export let strokeWidth = 1.5;

  // Pad with a single zero when there's nothing yet so the chart isn't blank.
  $: pts = values.length > 1 ? values : [0, 0];

  $: min = Math.min(...pts);
  $: max = Math.max(...pts);
  $: range = max - min || 1;

  // Map value → SVG coordinates.
  $: points = pts.map((v, i) => {
    const x = (i / (pts.length - 1)) * width;
    const y = height - ((v - min) / range) * (height - 4) - 2;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });

  $: polyline = points.join(' ');

  // Colour based on overall direction.
  $: trend = pts[pts.length - 1] - pts[0];
  $: stroke = trend >= 0 ? '#4ade80' : '#f87171'; // profit / loss

  // Area fill path (close back down to baseline).
  $: area = `M${points[0]} L${points.join(' L')} L${width},${height} L0,${height} Z`;
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" class="overflow-visible">
  <!-- Gradient area fill -->
  <defs>
    <linearGradient id="equity-fill" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color={stroke} stop-opacity="0.25" />
      <stop offset="100%" stop-color={stroke} stop-opacity="0.02" />
    </linearGradient>
  </defs>
  <path d={area} fill="url(#equity-fill)" />
  <polyline
    points={polyline}
    fill="none"
    stroke={stroke}
    stroke-width={strokeWidth}
    stroke-linejoin="round"
    stroke-linecap="round"
  />
</svg>
