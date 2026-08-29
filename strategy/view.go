package strategy

import "github.com/rustyeddy/trader/account"

// View is the read-only market/portfolio state OnBar may consult. It
// is never a broker handle and never reaches execution/risk types —
// only state a runner has already decided a strategy may see.
//
// View is deliberately minimal for v0 (issue #210's own review):
// Account is backed directly by the canonical account.Snapshot value
// (no parallel type needed). Historical-bar lookup is History, an
// optional capability (issue #214) a runtime's concrete View
// additionally implements rather than a required View method — see
// the package doc comment for why. A View implementation is expected
// to expose only data the owning runner has already made visible as
// of the current simulated or live time, which is what makes it the
// layer that owns *what* historical state is accessible (ADR-035's
// own "no-lookahead is a layered invariant" decision — View is one of
// three cooperating layers, not the only one).
type View interface {
	// Account returns the current, authoritative account snapshot.
	Account() account.Snapshot
}
