package journal

import "github.com/rustyeddy/trader/internal/id"

// NoAction is the payload of a KindNoAction record (ADR-067): a
// strategy intent that needed no order because the account already
// satisfied it — an exit or protective stop with no open position, or
// a target exposure already held. The most common cause is an entry
// that was refused (for example for insufficient initial margin,
// ADR-066) followed by the strategy's later exit.
//
// It records why a journaled Intent produced no Proposal, so the
// journal never shows an intent that silently led nowhere.
type NoAction struct {
	// IntentID identifies the journaled intent that needed no action.
	IntentID id.IntentID
	// Reason explains why, in the pipeline's own words.
	Reason string
}
