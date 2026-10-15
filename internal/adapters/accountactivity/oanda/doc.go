// Package oanda is the OANDA v20 source for the account-activity ledger
// (issue #510, ADR-081): it fetches an account's transaction history and
// normalizes it into accountactivity.Activity values. It implements
// accountactivity.Source; storage and the fetch-store-checkpoint loop are
// the Store and internal/service/accountactivity.
//
// # Fetching
//
// OANDA transaction IDs are per-account sequential integers, so the source
// pages by ID range with the idrange endpoint. The cursor is the last
// transaction ID of the last page returned. An unbounded run reads from
// ID 1 (or after the cursor) up to the account's lastTransactionID, taken
// from the account summary at the start of each call. A bounded run
// (Bounds) first resolves the time window to an ID window with the
// transactions list endpoint, which bootstraps existing history without
// importing everything. Both resume from the cursor alone, so a replay after
// any failure refetches the same pages.
//
// # Normalizing
//
// Money, quantities and prices arrive as decimal strings and are parsed
// straight into num types; no provider value passes through binary floating
// point. Money is denominated in the account's home currency, read from the
// account summary. Fields OANDA omits stay unknown rather than defaulting to
// zero; fields it reports as zero are reported zeros.
//
//   - ORDER_FILL becomes a fill, with realized P/L, half-spread cost
//     (broker-reported, with OANDA's own sign), commission, financing,
//     guaranteed-execution fee, the order and batch IDs, fullVWAP, the
//     fullPrice liquidity snapshot, and a TradeEffect per trade opened,
//     reduced or closed carrying that trade's units, exact price, realized
//     P/L, financing, spread cost and fee. Commission and the fees are
//     positive costs in OANDA's data and are negated to Trader's
//     account-centric sign. The top-level price is OANDA-deprecated and kept
//     only as a non-authoritative figure; the per-trade prices are the exact
//     ones, and fullVWAP is a reference, not the actual price.
//   - DAILY_FINANCING becomes financing.
//   - TRANSFER_FUNDS becomes a deposit or withdrawal by the sign of the amount.
//   - DIVIDEND_ADJUSTMENT becomes a dividend.
//   - Every other transaction, and a fill in an instrument Trader cannot yet
//     identify (anything but a pair of fiat currencies), becomes KindOther: its
//     ID, time, type, order ID, batch ID and the provider-native instrument
//     name are kept and no economics are claimed. Nothing is dropped.
//
// A recognized transaction that fails to parse is an error, not a skip: the
// page fails, the checkpoint does not move, and the problem is visible.
//
// # Credentials and network
//
// The bearer token comes from a CredentialProvider the composition root
// supplies from the environment or a token file (ADR-078); this package never
// reads configuration and never includes a token in an error. All HTTP goes
// through an HTTPDoer, so tests run against fixtures with no network.
package oanda
