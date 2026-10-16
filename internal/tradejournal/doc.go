// Package tradejournal renders completed trades as a trading journal (issue
// #515, ADR-085). It is a pure renderer over tradehistory.Trade and, if the
// caller measured it, executionquality.Result: no I/O, no clock, no provider
// types, and no relation to internal/journal, which records what a Trader run
// did. Imported broker history is never written there.
//
// What it writes is objective: identifiers, times, quantities, prices, P/L and
// the cost components, each exactly as known. An unknown figure is the word
// "unknown", never zero; a known zero is 0.00. A figure that was derived says
// so. The Review section of every entry is a scaffold of empty fields for the
// trader to fill in; nothing in it is generated.
//
// Output is deterministic: no timestamps of its own, no map iteration, and the
// same input renders the same bytes. Free text from outside (identifiers,
// benchmark sources, reasons) is flattened to one line so it cannot start a
// heading or close a property drawer.
package tradejournal
