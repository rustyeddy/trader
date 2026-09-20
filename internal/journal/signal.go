package journal

// Signal is the payload of a KindSignal record (ADR-044, issue #253,
// EMA-08): a strategy's own research-side trace of the evidence behind
// one bar's decision — why it did or did not act — distinct from the
// authoritative order/fill/account records the rest of this package
// defines.
//
// Values is deliberately a generic, string-keyed evidence bag rather
// than typed fields: journal depends only on id, num, instrument,
// order, risk, broker, and account (this package's own doc comment) —
// it has no notion of "EMA" or any other strategy's own concepts, and
// must not gain one merely to accommodate a single strategy. A
// strategy defines its own key vocabulary (for example
// strategy/emacross uses "fast_ema", "slow_ema", "prev_relation",
// "curr_relation", "cross", "action") and is responsible for
// documenting it; journal only guarantees the record is durable,
// ordered, and correlatable via Record.Metadata like any other Kind.
type Signal struct {
	// Strategy identifies which strategy produced this signal — its
	// Descriptor.Name, conventionally — so a reader consuming a journal
	// spanning strategies (or strategy versions) can group and interpret
	// Values correctly.
	Strategy string
	// Values holds this signal's own evidence, string-keyed and
	// strategy-defined. Every value must already be canonical,
	// deterministic text (a decimal price/rate string, an enum name,
	// and so on) — Signal does not itself interpret or reformat them.
	Values map[string]string
}
