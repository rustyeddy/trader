// Package backtest owns deterministic historical simulation
// orchestration (M5, ADR-035). It reads already-published canonical
// market data through marketdata.Manager, merges it into one
// chronologically ordered replay stream per strategy.Descriptor, and —
// in later M5 issues — drives that stream through a simulation clock,
// a strategy, and the same execution/risk pipeline used by live
// trading (pipeline.Pipeline), recording results through journal and
// report.
//
// backtest never imports adapters/broker/sim or any other concrete
// adapter package; only the outermost composition root
// (cmd/trader/backtest) constructs adapters and injects ports.
package backtest
