// Package account is the "trader account" command group (issue #514,
// ADR-084): sync, list and summarize broker account activity and the trades
// reconstructed from it.
//
// The handlers are thin transports. They parse flags, call
// internal/service/accountactivity or internal/service/accountreport, and
// pass the structured result to a Formatter. They contain no provider
// logic (provider names live only in cmd/internal/accountactivitycfg), no
// reconstruction and no execution-quality arithmetic.
//
// Output is a table by default and stable JSON with --format json. In both,
// an unknown figure is the literal word "unknown" (JSON: {"state":"unknown"})
// and a known zero is a real zero such as 0.00, so absence is never readable
// as zero. Execution quality appears only when --benchmark asks for it: not
// requested and requested-but-unmeasurable are different states.
package account
