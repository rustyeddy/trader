// Package report renders backtest results without computing them
// (issue #220, M5-12; ADR-035's "Metrics versus rendering" section;
// ADR-038). It projects backtest.Result into a report-owned view
// model, BacktestReport, once, and then offers Org, text, and JSON
// renderers over that same model — none of them touch backtest.Result
// or backtest.Metrics directly, and none of them perform arithmetic.
//
// report imports backtest for its result/metrics value types, but the
// dependency runs one way only: backtest never imports report
// (enforced by boundary_test.go). report also never imports
// orchestration, marketdata provider internals, broker adapters, or
// pipeline packages — a Renderer's only job is to turn an already-
// computed BacktestReport into bytes on an io.Writer.
package report
