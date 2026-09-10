// Package chart renders deterministic static PNG/SVG research charts
// from already-computed inputs — market data bars, price-level
// series (an indicator like SMA, or a resting stop level), and
// labeled markers (entry, exit, re-entry, post-exit trough) — for
// visually inspecting backtest behavior (issue #355).
//
// chart is a research/reporting concern, the same tier report and
// journal already occupy (docs/arch/package-boundaries.org): it
// consumes plain values a caller has already assembled from
// marketdata.Bar, order.Trade, and journal.Record — never a
// strategy, backtest, execution, risk, pipeline, broker, or service
// package. No strategy package may import chart either (the
// architecture document's own "strategies never receive or call a
// broker" invariant has a direct visual-tooling analogue: a strategy
// must remain unaware of how — or whether — its own decisions are
// ever rendered). boundary_test.go enforces both directions
// mechanically.
//
// Every renderer in this package is a pure function of its input:
// identical OverviewInput/EpisodeInput/EquityInput values produce
// byte-identical output, for the same reason backtest determinism
// matters (docs/arch/trader-framework-architecture.org's own
// "Determinism Is a Feature" section) — a chart is part of a
// research run's own reproducible record, not a one-off visualization.
// This holds because the underlying renderer (gonum.org/v1/plot)
// embeds its own fonts (codeberg.org/go-fonts/liberation) rather than
// depending on whatever fonts happen to be installed on the host, and
// because this package never reads the system clock, environment, or
// any other hidden input — every value a chart displays arrives
// through its own Input struct.
//
// Only two chart-native types are exported at all (Format and Size);
// every other exported type (LevelPoint, Marker, MarkerKind,
// OverviewInput, EpisodeInput, EquityInput) is a plain Trader-owned
// value built from marketdata/num/order-time types, never a
// gonum.org/v1/plot type — the architecture document's own "do not
// expose vendor SDK types from Trader's core APIs" rule applies here
// exactly as it does to a broker or data-provider adapter.
//
// This is deliberately the smallest renderer that can produce the
// three chart types issue #355 requires (overview, one per-trade
// episode, and an equity curve) — not a generic multi-panel charting
// framework. Candlestick/OHLC rendering, interactive zoom/hover,
// indicator toggling, and an HTML dashboard are explicit, named
// follow-ups in that issue, not gaps in this package's own design.
package chart
