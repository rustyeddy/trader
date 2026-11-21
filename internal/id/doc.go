// Package id makes Trader's identifiers. For now there is one, RunID, which names a
// run and the directory its results are kept in.
//
// A RunID's text form is "run_" and a ULID (https://github.com/ulid/spec): a 48-bit
// millisecond timestamp and 80 bits of entropy in 26 Crockford Base32 characters.
// IDs sort by the time they were made and cannot realistically collide. The ULID
// encoding lives in internal/ulid and is never part of this package's API: no
// exported name exposes a raw ULID, a third-party type or the 16-byte form.
//
// # Construction
//
// A RunID comes only from a Generator or from ParseRunID, which validates text it
// did not make, so a RunID in hand is always well formed. The zero value is unset
// (IsZero, a visible String, and an error when marshaled), never the ID that would
// encode sixteen zero bytes; ParseRunID refuses that pattern too.
//
// # Generation
//
// A Generator never reads the time itself: it is given a func() time.Time (in
// production internal/clock.Clock, in a test a fixed time) and an EntropySource
// (Random, crypto/rand, in production; the seeded Deterministic in tests, so the
// same clock and seed give the same IDs). IDs made in one millisecond get strictly
// increasing entropy, so their text sorts in creation order.
//
// # Other identifiers
//
// Orders have ids from the broker (sim-1, t-1), and an account's id is given when
// it is made. Other kinds (order, fill, event, correlation) are not here until
// something needs a generated identity for them; _archive/internal/id has the
// earlier seven-kind design.
//
// This package is private to Trader's runtime (ADR-065).
package id
