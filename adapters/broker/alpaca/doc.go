// Package alpaca implements Trader's first equity-brokerage broker.Broker/
// broker.Account adapter (issue #301, EQ-08), against Alpaca's paper
// Trading API. It is a real, network-facing adapter — not a simulator —
// so unlike adapters/broker/sim, Alpaca itself is authoritative for
// account balances, positions, and order/fill state; this package only
// translates between Alpaca's wire vocabulary and Trader's canonical
// broker/order/account types (ADR-007).
//
// # A separate package from marketdata/internal/provider/alpaca
//
// This package shares no code with marketdata/internal/provider/alpaca,
// the Alpaca Market Data provider added by issue #297 (EQ-04). That
// package lives under marketdata/internal/, so Go's internal/ visibility
// rule confines it to the marketdata/ subtree — this package could not
// import it even if the wire shapes overlapped. They do not: Alpaca's
// Trading API (orders, positions, account) and Market Data API (bars)
// are different products with different base URLs and response shapes.
// This package's HTTP client mirrors that package's conventions in shape
// only — CredentialProvider, an HTTPDoer seam, retry/backoff, and
// sentinel-error status classification — duplicated deliberately, the
// same "provider-independent pacing/backoff needs" reasoning that
// package's own client.go already documents for its copy of oanda's
// pattern.
//
// # Scope: Phase 1 minimal
//
// Per issue #301's own "start with the smallest order-type set required
// for the end-to-end Phase 1 smoke test" instruction, this package
// implements order.Market only. Submit reports broker.ErrUnsupported for
// any other order.Type; extending to Limit/Stop/StopLimit is a small,
// additive change to translate.go once a real caller needs one — nothing
// in this codebase's execution layer produces a non-Market proposal
// today.
//
// Fractional-share quantities are not handled by this package at all:
// every Phase 1 equity Listing's Spec already fixes a whole-share
// quantity increment (ADR-047), which order.NewFill/order.NewOrder
// already enforce before a Request ever reaches this adapter's Submit.
// Enabling fractional trading is a future Listing Spec change, not an
// adapter change.
//
// # Events: REST polling, not streaming
//
// Alpaca offers a real-time order-update websocket, but nothing in this
// codebase has a streaming client yet — LIVE-01 (#311) is the natural
// home for a first one. Building a websocket client (reconnect/backoff,
// auth handshake, message framing) is materially more work than this
// issue's own "smallest adapter behavior" scope calls for. Account.Events
// therefore returns a REST-polling EventReader (see event_reader.go and
// ADR-051) that periodically calls ListOrders and synthesizes
// broker.Event values locally — this adapter, like sim, owns Sequence
// assignment for its own stream, since Alpaca reports none.
//
// One direct consequence: a fill event synthesized from a polling
// observation uses the order's cumulative filled_avg_price for the
// newly observed quantity increment, not a real per-execution price.
// This is exact when an order fills via a single execution — the normal
// case for a small paper-trading market order, and what EQ-09's
// end-to-end smoke test exercises — and only an approximation for an
// order that fills across multiple partial executions between two polls.
// order.NewFill's own tick-size validation may reject such an
// approximated price on rare, non-tick-aligned averages; Next() surfaces
// that as an error rather than silently dropping the fill, matching this
// package's "never silently discard a critical event" discipline.
// Deriving true per-execution fills (Alpaca's activities/fills endpoint)
// is a documented, deferred improvement, not solved here.
//
// # Order identity and adapter correlation
//
// Request.OrderID becomes Alpaca's client_order_id at submission —
// Alpaca treats client_order_id as an idempotency key on its own side,
// so Trader's existing OrderID-based submission idempotency (ADR-017)
// carries through directly with no adapter-side dedup table needed for
// Submit itself. Alpaca's own order id (a UUID this adapter never
// constructs or interprets, only stores) is preserved solely for
// adapter-internal correlation — resolving a Trader order.CancelRequest/
// order.ReplaceRequest to the Alpaca order Cancel/Replace must address —
// and is never exposed to a caller except via order.Order.BrokerOrderID,
// per the broker-neutral order vocabulary (ADR-017).
package alpaca
