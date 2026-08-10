// Package instrument implements Trader's economic instrument and listing
// value models, as decided by issue #25 (M1-07) and ADR-003.
//
// # Instrument versus Listing
//
// Instrument is what an economic thing is, independent of any provider,
// venue, or symbol spelling. Listing is how one specific provider exposes
// an Instrument for trading: its provider-native symbol, tick size,
// quantity increment, contract multiplier, settlement currency, and
// whether it is currently tradable there.
//
//	Instrument = what economic thing is this?
//	Listing    = how does a provider/venue expose or trade it?
//
// EUR/USD is one Instrument. An OANDA adapter parsing "EUR_USD" and another
// provider's adapter parsing "EURUSD" both resolve, after normalizing to
// num.Currency values, to the identical Instrument — see ID below. Each
// provider's own listing of it — its symbol text, tick size, and whether it
// is currently tradable there — is a separate Listing referencing that one
// Instrument by ID.
//
// A Listing distinguishes its provider (a broker or data vendor, such as
// "OANDA" or "IBKR") from its venue (an exchange or execution venue, such
// as "NASDAQ" or "CME"); they are not the same thing, and one provider can
// expose listings on several venues. Venue may be empty for markets with
// no meaningful centralized venue, such as spot FX. NewListing also
// rejects a Listing with Tradable set true for an Instrument that is
// non-orderable by Kind — KindContinuousSeries or KindIndex — since a
// Listing given only an ID could not otherwise catch that contradiction.
//
// # ID is canonical and deterministic, not generated
//
// instrument.ID is a different identity scheme from the id package's
// ID[K]: id.ID[K] is deliberately random and regenerated per event (a new
// RunID per run, a new OrderID per order); an instrument has a natural,
// canonical identity instead. Two otherwise-identical instances of EUR/USD
// must never be distinct — a fresh identity every run would be exactly
// wrong. This is why id's own package doc comment named this package,
// rather than a generated id.InstrumentID, as where instrument identity
// would come from.
//
// Every ID is built deterministically from an instrument's economic
// attributes by a per-kind constructor (CurrencyPairID, EquityID, ETFID,
// FutureID, ContinuousSeriesID, IndexID), never parsed from free-form
// provider text. A provider adapter converts whatever spelling it receives
// into typed inputs — num.Currency values, a normalized exchange and
// ticker — before calling one of these constructors; by the time this
// package sees the input, there is no spelling left to be inconsistent
// about.
//
// Each constructor validates its own input and returns the zero ID, never
// a malformed non-zero one, when given an empty or invalid component: this
// keeps IsZero a reliable check even for direct callers of the ID
// constructors, not only for the Instrument constructors that call them
// after their own validation. Identifier components are further
// restricted to letters, digits, '.', and '-' — enough for real exchange
// codes, tickers, and futures roots ("BRK.B", "RDS-A", "6E") — specifically
// excluding ':' and '/', the characters this package uses to join
// components together; without that restriction, EquityID("A:B", "C") and
// EquityID("A", "B:C") could collide on the same canonical string.
//
// # Six initial kinds, and why futures split in two
//
// KindCurrencyPair, KindEquity, KindETF, KindFuture, KindContinuousSeries,
// and KindIndex are this package's initial kinds (see Kind).
//
// A futures Instrument is one specific expiring contract, not the contract
// family: the December 2026 and March 2027 E-mini S&P 500 contracts are
// two different Instruments, because they are the actual distinct
// orderable economic objects — expiration is part of a future's canonical
// identity, the same way base/quote is for a currency pair. A continuous,
// back-adjusted research series derived from a futures family is a
// different kind of thing entirely: synthetic, non-orderable, and without
// a single expiration. Rather than express that as a flag on a Listing of
// what looks like a tradable contract, it is its own kind,
// KindContinuousSeries, with its own ID constructor, ContinuousSeriesID.
// This makes "tradable versus research-only" and "a specific contract
// versus its continuous series" both decidable from Kind alone, not from
// validating a combination of Listing flags.
//
// Only a future's year and month are significant to its identity, matching
// standard contract-month conventions (a real contract's exact last
// trading day is a settlement detail, not part of what makes it "the
// December 2026 contract"). NewFuture normalizes whatever time.Time it is
// given down to the first instant of that month in UTC before building the
// ID and storing Instrument.Expiration, so the two can never disagree —
// two calls with expirations on different days of the same month always
// produce the same Instrument.
//
// # Equity and ETF identity is a convention, not a permanent identifier
//
// EquityID and ETFID use exchange+ticker as Trader's M1 canonical identity
// convention. This is not a claim that exchange+ticker is a permanent,
// globally stable security identifier: corporate actions, ticker reuse,
// and venue changes can all break that assumption over time. It is
// sufficient for M1's scope; a more durable identity scheme, if one proves
// necessary, is a later, additive change.
//
// # Synthetic and multi-leg instruments are deferred
//
// Pairs trades, futures calendar spreads, options spreads, and weighted
// baskets are explicitly out of scope, per the architecture document's
// "Deferred: Synthetic and Multi-Leg Instruments" section. Nothing in this
// package names a leg, a synthetic composition, or a multi-leg execution
// concept; see arch_test.go, which enforces that mechanically.
package instrument
